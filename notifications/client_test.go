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
func (n *memoryDesktop) Close() error               { return nil }

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
