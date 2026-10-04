package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/protocol/app"
	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	wire "github.com/YMGPwcca/linkmyphone/protocol/notifications"
	"github.com/YMGPwcca/linkmyphone/protocol/platform"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
)

type memoryDesktop struct {
	mu          sync.Mutex
	next        uint32
	visible     map[uint32]DesktopNotification
	events      chan NativeEvent
	failOnce    bool
	renderCount int
	closeCount  int
}

func newMemoryDesktop() *memoryDesktop {
	return &memoryDesktop{visible: make(map[uint32]DesktopNotification), events: make(chan NativeEvent, 16)}
}
func (n *memoryDesktop) Notify(_ context.Context, item DesktopNotification) (uint32, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.failOnce {
		n.failOnce = false
		return 0, errors.New("synthetic render failure")
	}
	id := item.ReplaceID
	if id == 0 {
		n.next++
		id = n.next
	}
	n.visible[id] = item
	n.renderCount++
	return id, nil
}
func (n *memoryDesktop) CloseNotification(_ context.Context, id uint32) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.visible, id)
	return nil
}
func (n *memoryDesktop) Events() <-chan NativeEvent { return n.events }
func (n *memoryDesktop) SupportsActions() bool      { return true }
func (n *memoryDesktop) Close() error {
	n.mu.Lock()
	n.closeCount++
	n.mu.Unlock()
	return nil
}

type sentMessage struct {
	message app.Message
	session string
}
type fixtureTransport struct {
	received chan relay.Received
	sent     chan sentMessage
}

func newFixtureTransport() *fixtureTransport {
	return &fixtureTransport{received: make(chan relay.Received, 16), sent: make(chan sentMessage, 16)}
}
func (f *fixtureTransport) Received() <-chan relay.Received { return f.received }
func (f *fixtureTransport) Send(ctx context.Context, _ string, session string, typ dcg.TransportMessageType, payload []byte) error {
	if typ != dcg.TransportMessageTypeApp {
		return fmt.Errorf("wrong transport type %d", typ)
	}
	message, err := app.UnmarshalMessage(payload)
	if err != nil {
		return err
	}
	select {
	case f.sent <- sentMessage{message: message, session: session}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func newFixtureClient(t *testing.T) (*Client, *memoryDesktop, *fixtureTransport) {
	t.Helper()
	native := newMemoryDesktop()
	transport := newFixtureTransport()
	client, err := New(transport, native, Config{Target: "phone", RequestTimeout: time.Second, RemoteActions: true, ReplyEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return client, native, transport
}

func startClient(t *testing.T, c *Client) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	return cancel, done
}

func stopClient(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not stop")
	}
}

func respond(t *testing.T, transport *fixtureTransport, request sentMessage, values app.ValueSet) {
	t.Helper()
	id, ok := request.message.Header(platform.HeaderRequestID)
	if !ok {
		t.Fatal("request lacks correlation ID")
	}
	payload, err := app.MarshalMessage(app.NewResponse(id, values))
	if err != nil {
		t.Fatal(err)
	}
	transport.received <- relay.Received{Source: "phone", TransportMessageType: dcg.TransportMessageTypeApp, Payload: payload}
}
func item(key, text string, postTime int64) *wire.Item {
	return &wire.Item{Key: key, AppName: "Synthetic app", Title: "Title", Text: text, PostTime: postTime, IsClearable: true, Actions: []wire.Action{{Name: "Reply", InlineReply: true, Index: 4}}}
}
func apply(t *testing.T, c *Client, typ int32, value *wire.Item) {
	t.Helper()
	if err := c.applyBatch(context.Background(), wire.Batch{Operations: []wire.Operation{{Type: typ, Key: value.Key, Item: value}}}); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationUpdateReplacesAndRemovalClosesWithoutRemoteEcho(t *testing.T) {
	c, native, transport := newFixtureClient(t)
	apply(t, c, wire.OperationNew, item("session-a|one", "first", 10))
	apply(t, c, wire.OperationNew, item("session-a|one", "second", 11))
	if len(native.visible) != 1 || native.visible[1].Body != "second" || native.visible[1].ReplaceID != 1 {
		t.Fatalf("replacement=%#v", native.visible)
	}
	if err := c.applyBatch(context.Background(), wire.Batch{Operations: []wire.Operation{{Type: wire.OperationRemove, Key: "session-a|one"}}}); err != nil {
		t.Fatal(err)
	}
	if len(native.visible) != 0 {
		t.Fatalf("removed desktop item retained: %#v", native.visible)
	}
	keys, _ := c.reconcileState()
	if len(keys) != 0 {
		t.Fatalf("removed Android key retained: %v", keys)
	}
	select {
	case request := <-transport.sent:
		t.Fatalf("phone removal echoed a remote mutation: %#v", request)
	default:
	}
}

func TestExistingItemsDoNotAlertAndReconcileKeepsPairedPostTimes(t *testing.T) {
	c, native, _ := newFixtureClient(t)
	apply(t, c, wire.OperationExisting, item("old|a", "already present", 10))
	apply(t, c, wire.OperationExisting, item("new|b", "already present", 20))
	if len(native.visible) != 0 {
		t.Fatal("reconciled items generated startup alerts")
	}
	keys, times := c.reconcileState()
	pairs := map[string]int64{}
	for i, key := range keys {
		pairs[key] = times[i]
	}
	if len(pairs) != 2 || pairs["old|a"] != 10 || pairs["new|b"] != 20 {
		t.Fatalf("reconcile identities=%v", pairs)
	}
	if err := c.applyBatch(context.Background(), wire.Batch{Operations: []wire.Operation{{Type: wire.OperationSessionChange, Key: "new"}}}); err != nil {
		t.Fatal(err)
	}
	keys, times = c.reconcileState()
	if len(keys) != 1 || keys[0] != "new|b" || times[0] != 20 {
		t.Fatalf("session reset retained stale keys %v %v", keys, times)
	}
}

func TestFailedRenderingCanRecoverOnIdenticalRetry(t *testing.T) {
	c, native, _ := newFixtureClient(t)
	native.failOnce = true
	batch := wire.Batch{Operations: []wire.Operation{{Type: wire.OperationNew, Key: "one", Item: item("one", "text", 10)}}}
	if err := c.applyBatch(context.Background(), batch); err == nil {
		t.Fatal("render failure accepted")
	}
	if err := c.applyBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if len(native.visible) != 1 || native.visible[1].Body != "text" {
		t.Fatal("successful retry acknowledged without rendering the notification")
	}
}

func TestOldDesktopActionAndRemovedReplyCannotMutateUpdatedPhoneItem(t *testing.T) {
	c, native, transport := newFixtureClient(t)
	c.ready = true
	apply(t, c, wire.OperationNew, item("one", "original", 10))
	oldAction := desktopReplyAction(t, native.visible[1])
	apply(t, c, wire.OperationNew, item("one", "updated", 11))
	if err := c.handleNative(context.Background(), NativeEvent{Kind: NativeAction, ID: 1, Action: oldAction}); !errors.Is(err, ErrStaleAction) {
		t.Fatalf("stale button error=%v", err)
	}
	opened := make(chan struct{})
	stopped := make(chan struct{})
	c.cfg.Prompt = func(ctx context.Context, _ ReplyPrompt) (string, bool, error) {
		close(opened)
		<-ctx.Done()
		close(stopped)
		return "", false, ctx.Err()
	}
	if err := c.handleNative(context.Background(), NativeEvent{Kind: NativeAction, ID: 1, Action: desktopReplyAction(t, native.visible[1])}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("reply did not open")
	}
	if err := c.applyBatch(context.Background(), wire.Batch{Operations: []wire.Operation{{Type: wire.OperationRemove, Key: "one"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
		c.prompts.Wait()
	case <-time.After(time.Second):
		t.Fatal("phone removal did not cancel the reply")
	}
	select {
	case mutation := <-transport.sent:
		t.Fatalf("stale action sent: %#v", mutation)
	default:
	}
}

func TestExpirationAndProgrammaticCloseNeverDismissPhone(t *testing.T) {
	for _, reason := range []uint32{1, 3} {
		t.Run(fmt.Sprint(reason), func(t *testing.T) {
			c, _, transport := newFixtureClient(t)
			c.ready = true
			apply(t, c, wire.OperationNew, item("one", "text", 10))
			if err := c.handleNative(context.Background(), NativeEvent{Kind: NativeClosed, ID: 1, Reason: reason}); err != nil {
				t.Fatal(err)
			}
			keys, _ := c.reconcileState()
			if len(keys) != 1 || keys[0] != "one" {
				t.Fatalf("desktop close erased Android state: %v", keys)
			}
			select {
			case mutation := <-transport.sent:
				t.Fatalf("non-user close sent mutation: %#v", mutation)
			default:
			}
		})
	}
}

func TestReceiveOnlyAndOngoingCannotSendPhoneMutations(t *testing.T) {
	c, _, transport := newFixtureClient(t)
	c.ready = true
	c.cfg.RemoteActions = false
	apply(t, c, wire.OperationNew, item("one", "text", 10))
	if err := c.Dismiss(context.Background(), "one"); !errors.Is(err, ErrReceiveOnly) {
		t.Fatalf("receive-only dismiss=%v", err)
	}
	c.cfg.RemoteActions = true
	c.records["one"].item.IsClearable = false
	if err := c.Clear(context.Background(), []string{"one"}); err == nil {
		t.Fatal("ongoing notification accepted for clear")
	}
	select {
	case mutation := <-transport.sent:
		t.Fatalf("forbidden mutation sent: %#v", mutation)
	default:
	}
}

func TestOtherAPPRouteDoesNotMasqueradeAsMalformedNotification(t *testing.T) {
	c, native, transport := newFixtureClient(t)
	events := make(chan string, 8)
	c.cfg.OnEvent = func(message string, _ map[string]string) { events <- message }
	cancel, done := startClient(t, c)
	defer stopClient(t, cancel, done)

	// DeviceProxyMessageReceiver accepts JSON on this APP route, not PBValueSet.
	other, err := platform.MarshalWithHeaderCount(platform.Message{
		Headers: []platform.Header{{Key: platform.HeaderRoute, Value: "/DeviceProxyClient/TransportMiddleware"}},
		Payload: []byte(`{"parameters":{"commandContent":"open"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	transport.received <- relay.Received{Source: "phone", TransportMessageType: dcg.TransportMessageTypeApp, Payload: other}

	value := item("one", "text", 10)
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := app.MarshalMessage(app.NewRequest(wire.RoutePhoneContent, "push", app.ValueSet{
		"contentType": "notifications", "notificationKeys": []string{"one"},
		"operations": []int32{wire.OperationNew}, "notifications": []string{string(body)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	transport.received <- relay.Received{Source: "phone", TransportMessageType: dcg.TransportMessageTypeApp, Payload: payload}
	if response := sentWithin(t, transport); response.message.Values["result"] != int32(0) {
		t.Fatalf("notification after unrelated APP message rejected: %#v", response)
	}
	native.mu.Lock()
	rendered := native.renderCount
	native.mu.Unlock()
	if rendered != 1 {
		t.Fatalf("following notification rendered %d times", rendered)
	}
	for {
		select {
		case event := <-events:
			if event == "malformed APP envelope" {
				t.Fatal("non-notification APP route logged as malformed notification")
			}
		default:
			return
		}
	}
}

func TestMalformedNotificationAPPReportsDecodeStage(t *testing.T) {
	c, _, transport := newFixtureClient(t)
	events := make(chan map[string]string, 1)
	c.cfg.OnEvent = func(message string, fields map[string]string) {
		if message == "malformed APP envelope" {
			events <- fields
		}
	}
	cancel, done := startClient(t, c)
	defer stopClient(t, cancel, done)
	payload, err := platform.MarshalWithHeaderCount(platform.Message{
		Headers: []platform.Header{{Key: platform.HeaderRoute, Value: wire.RoutePhoneContent}},
		Payload: []byte(`{"invalid":"PBValueSet"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	transport.received <- relay.Received{Source: "phone", TransportMessageType: dcg.TransportMessageTypeApp, Payload: payload}
	select {
	case fields := <-events:
		if fields["stage"] != "values" || fields["reason"] == "" {
			t.Fatalf("malformed notification lacks diagnostic stage: %#v", fields)
		}
	case <-time.After(time.Second):
		t.Fatal("malformed notification payload was silently ignored")
	}
}

func TestAPPResultAcknowledgementDuplicateAndMalformedBatch(t *testing.T) {
	c, native, transport := newFixtureClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second * 2):
			t.Error("client did not stop")
		}
	}()
	value := item("one", "text", 10)
	body, _ := json.Marshal(value)
	values := app.ValueSet{"contentType": "notifications", "notificationKeys": []string{"one"}, "operations": []int32{1}, "notifications": []string{string(body)}}
	payload, err := app.MarshalMessage(app.NewRequest(wire.RoutePhoneContent, "push-1", values))
	if err != nil {
		t.Fatal(err)
	}
	receive := relay.Received{Source: "phone", SessionID: "peer-session", TransportMessageType: dcg.TransportMessageTypeApp, Payload: payload}
	for range 2 {
		transport.received <- receive
		select {
		case response := <-transport.sent:
			original, _ := response.message.Header(platform.HeaderOriginalRequestID)
			if original != "push-1" || response.message.Values["result"] != int32(0) || response.session != "peer-session" {
				t.Fatalf("APP response=%#v", response)
			}
		case <-time.After(time.Second):
			t.Fatal("push lacked application response")
		}
	}
	native.mu.Lock()
	if native.renderCount != 1 {
		t.Errorf("duplicate push re-alerted %d times", native.renderCount)
	}
	native.mu.Unlock()
	values["notificationKeys"] = []string{"wrong"}
	payload, err = app.MarshalMessage(app.NewRequest(wire.RoutePhoneContent, "bad", values))
	if err != nil {
		t.Fatal(err)
	}
	receive.Payload = payload
	transport.received <- receive
	select {
	case response := <-transport.sent:
		if response.message.Values["result"] != int32(1) {
			t.Fatal("invalid key correlation acknowledged as successful")
		}
	case <-time.After(time.Second):
		t.Fatal("malformed batch lacked failure APP response")
	}
}

func sentWithin(t *testing.T, transport *fixtureTransport) sentMessage {
	t.Helper()
	select {
	case message := <-transport.sent:
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for application request")
		return sentMessage{}
	}
}

func pushBatch(t *testing.T, transport *fixtureTransport, id, dedupe string, value *wire.Item) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	values := app.ValueSet{
		"contentType":      wire.ContentType,
		"notificationKeys": []string{value.Key},
		"operations":       []int32{wire.OperationNew},
		"notifications":    []string{string(body)},
		"dedupeID":         dedupe,
	}
	payload, err := app.MarshalMessage(app.NewRequest(wire.RoutePhoneContent, id, values))
	if err != nil {
		t.Fatal(err)
	}
	transport.received <- relay.Received{Source: "phone", TransportMessageType: dcg.TransportMessageTypeApp, Payload: payload}
}

func TestUserDismissReasonForwardsOnlyClearableNotification(t *testing.T) {
	c, native, transport := newFixtureClient(t)
	c.ready = true
	apply(t, c, wire.OperationNew, item("one", "text", 10))
	cancel, done := startClient(t, c)
	defer stopClient(t, cancel, done)

	native.events <- NativeEvent{Kind: NativeClosed, ID: 1, Reason: 2}
	request := sentWithin(t, transport)
	if request.message.Values["operation"] != wire.OperationRemove || request.message.Values["key"] != "one" {
		t.Fatalf("user dismissal request=%#v", request.message.Values)
	}
	respond(t, transport, request, app.ValueSet{"result": int32(0)})
	c.stateMu.Lock()
	hidden := c.records["one"].hidden
	c.stateMu.Unlock()
	if !hidden {
		t.Fatal("user dismissal did not hide the local notification")
	}
}

func TestClearRejectsUnknownOngoingAndEmptyKeysWithoutSending(t *testing.T) {
	c, _, transport := newFixtureClient(t)
	c.ready = true
	apply(t, c, wire.OperationNew, item("clearable", "text", 1))
	ongoing := item("ongoing", "text", 2)
	ongoing.IsClearable = false
	apply(t, c, wire.OperationNew, ongoing)

	for name, keys := range map[string][]string{
		"empty":   nil,
		"unknown": {"clearable", "missing"},
		"ongoing": {"ongoing"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := c.Clear(context.Background(), keys); err == nil {
				t.Fatal("invalid clear was accepted")
			}
		})
	}
	select {
	case request := <-transport.sent:
		t.Fatalf("invalid clear sent request: %#v", request)
	default:
	}
}

func TestOriginalActionAndInlineReplyPayloads(t *testing.T) {
	c, native, transport := newFixtureClient(t)
	c.ready = true
	value := item("one", "text", 10)
	value.Actions = []wire.Action{
		{Name: "Open", Index: 7},
		{Name: "Reply", InlineReply: true, Index: 4},
	}
	apply(t, c, wire.OperationNew, value)
	prompted := make(chan struct{})
	c.cfg.Prompt = func(context.Context, ReplyPrompt) (string, bool, error) {
		close(prompted)
		return "  exact reply 👋  ", true, nil
	}
	cancel, done := startClient(t, c)
	defer stopClient(t, cancel, done)

	var normalID, replyID string
	for _, action := range native.visible[1].Actions {
		if action.Label == "Open" {
			normalID = action.ID
		}
		if action.Label == "Reply" {
			replyID = action.ID
		}
	}
	if normalID == "" || replyID == "" {
		t.Fatal("expected original and inline reply actions")
	}
	native.events <- NativeEvent{Kind: NativeAction, ID: 1, Action: normalID}
	request := sentWithin(t, transport)
	if request.message.Values["operation"] != wire.OperationLaunch || request.message.Values["actionIndex"] != int32(7) {
		t.Fatalf("original action request=%#v", request.message.Values)
	}
	if _, ok := request.message.Values["inlineReplyMessage"]; ok {
		t.Fatal("original action unexpectedly carried inline reply")
	}
	respond(t, transport, request, app.ValueSet{"result": int32(0)})

	native.events <- NativeEvent{Kind: NativeAction, ID: 1, Action: replyID}
	select {
	case <-prompted:
	case <-time.After(time.Second):
		t.Fatal("inline reply prompt did not open")
	}
	request = sentWithin(t, transport)
	if request.message.Values["actionIndex"] != int32(4) || request.message.Values["inlineReplyMessage"] != "  exact reply 👋  " {
		t.Fatalf("inline reply request=%#v", request.message.Values)
	}
	respond(t, transport, request, app.ValueSet{"result": int32(0)})
	c.prompts.Wait()
}

func TestConcurrentReplyPromptDoesNotBlockOtherAction(t *testing.T) {
	c, native, transport := newFixtureClient(t)
	c.ready = true
	value := item("one", "text", 10)
	value.Actions = []wire.Action{
		{Name: "Open", Index: 7},
		{Name: "Reply", InlineReply: true, Index: 4},
	}
	apply(t, c, wire.OperationNew, value)
	opened := make(chan struct{})
	release := make(chan struct{})
	c.cfg.Prompt = func(ctx context.Context, _ ReplyPrompt) (string, bool, error) {
		close(opened)
		select {
		case <-release:
			return "", false, nil
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	cancel, done := startClient(t, c)
	defer stopClient(t, cancel, done)
	var normalID, replyID string
	for _, action := range native.visible[1].Actions {
		if action.Label == "Open" {
			normalID = action.ID
		}
		if action.Label == "Reply" {
			replyID = action.ID
		}
	}
	native.events <- NativeEvent{Kind: NativeAction, ID: 1, Action: replyID}
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("reply prompt did not open")
	}
	native.events <- NativeEvent{Kind: NativeAction, ID: 1, Action: normalID}
	request := sentWithin(t, transport)
	if request.message.Values["actionIndex"] != int32(7) {
		t.Fatalf("other action was not dispatched while reply was open: %#v", request.message.Values)
	}
	respond(t, transport, request, app.ValueSet{"result": int32(0)})
	close(release)
	c.prompts.Wait()
}

func TestPendingMutationCancellationRemovesRequest(t *testing.T) {
	c, _, transport := newFixtureClient(t)
	c.ready = true
	apply(t, c, wire.OperationNew, item("one", "text", 10))
	cancelRun, done := startClient(t, c)
	defer stopClient(t, cancelRun, done)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 1)
	go func() { errs <- c.Dismiss(ctx, "one") }()
	_ = sentWithin(t, transport)
	cancel()
	select {
	case err := <-errs:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled mutation=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled mutation remained pending")
	}
	c.pendingMu.Lock()
	pending := len(c.pending)
	c.pendingMu.Unlock()
	if pending != 0 {
		t.Fatalf("pending request leaked after cancellation: %d", pending)
	}
}

func TestPermissionRevokeMakesSessionNotReady(t *testing.T) {
	c, _, transport := newFixtureClient(t)
	c.ready = true
	apply(t, c, wire.OperationNew, item("one", "text", 10))
	cancelRun, done := startClient(t, c)
	defer cancelRun()
	errs := make(chan error, 1)
	go func() { errs <- c.Dismiss(context.Background(), "one") }()
	request := sentWithin(t, transport)
	respond(t, transport, request, app.ValueSet{"result": int32(7)})
	select {
	case err := <-errs:
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("permission revoke mutation=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("permission revoke mutation did not finish")
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("client did not stop on permission revoke: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client remained running after permission revoke")
	}
	c.stateMu.Lock()
	ready := c.ready
	c.stateMu.Unlock()
	if ready {
		t.Fatal("permission revoke left session ready")
	}
}

func TestDelayedMutationGuardRejectsAfterUpdate(t *testing.T) {
	c, _, transport := newFixtureClient(t)
	c.ready = true
	apply(t, c, wire.OperationNew, item("one", "original", 10))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	errs := make(chan error, 1)
	go func() { errs <- c.Dismiss(ctx, "one") }()
	var queued sendJob
	select {
	case queued = <-c.sends:
	case <-ctx.Done():
		t.Fatal("mutation was not queued")
	}
	apply(t, c, wire.OperationNew, item("one", "updated", 11))
	c.sends <- queued
	loopDone := make(chan struct{})
	go func() {
		c.sendLoop(ctx, c.failures)
		close(loopDone)
	}()
	select {
	case err := <-errs:
		if !errors.Is(err, ErrStaleAction) {
			t.Fatalf("delayed mutation=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("delayed mutation did not complete")
	}
	cancel()
	<-loopDone
	select {
	case request := <-transport.sent:
		t.Fatalf("stale mutation reached phone: %#v", request)
	default:
	}
}

func TestDedupeFailureCanRetrySameBatch(t *testing.T) {
	c, native, transport := newFixtureClient(t)
	native.failOnce = true
	cancel, done := startClient(t, c)
	defer stopClient(t, cancel, done)
	value := item("one", "retry", 10)
	pushBatch(t, transport, "push-1", "same-batch", value)
	first := sentWithin(t, transport)
	if first.message.Values["result"] != int32(1) {
		t.Fatalf("failed batch result=%#v", first.message.Values)
	}
	pushBatch(t, transport, "push-2", "same-batch", value)
	second := sentWithin(t, transport)
	if second.message.Values["result"] != int32(0) {
		t.Fatalf("retry batch result=%#v", second.message.Values)
	}
	native.mu.Lock()
	renderCount := native.renderCount
	native.mu.Unlock()
	if renderCount != 1 {
		t.Fatalf("successful dedupe retry rendered %d times", renderCount)
	}
}

func TestRunTeardownClosesBackendAndLocalState(t *testing.T) {
	c, native, _ := newFixtureClient(t)
	apply(t, c, wire.OperationNew, item("one", "text", 10))
	cancel, done := startClient(t, c)
	stopClient(t, cancel, done)
	native.mu.Lock()
	closeCount, visible := native.closeCount, len(native.visible)
	native.mu.Unlock()
	if closeCount != 1 || visible != 0 {
		t.Fatalf("teardown backend close=%d visible=%d", closeCount, visible)
	}
	c.stateMu.Lock()
	records := len(c.records)
	c.stateMu.Unlock()
	if records != 0 {
		t.Fatalf("teardown retained %d records", records)
	}
}

func desktopReplyAction(t *testing.T, notification DesktopNotification) string {
	t.Helper()
	for _, action := range notification.Actions {
		if action.Label == "Reply" {
			return action.ID
		}
	}
	t.Fatal("reply action not exposed")
	return ""
}
