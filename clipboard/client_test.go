package clipboard

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	proto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/msaep"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

type fakeRelay struct {
	recv chan relay.Received
	mu   sync.Mutex
	sent []relay.Received
}

func newFakeRelay() *fakeRelay {
	return &fakeRelay{recv: make(chan relay.Received, 8)}
}

func (f *fakeRelay) Received() <-chan relay.Received { return f.recv }

func (f *fakeRelay) Send(_ context.Context, target, session string, mt dcg.TransportMessageType, payload []byte) error {
	f.mu.Lock()
	f.sent = append(f.sent, relay.Received{
		Source: target, SessionID: session, TransportMessageType: mt,
		Payload: append([]byte(nil), payload...),
	})
	f.mu.Unlock()
	return nil
}

type fakeLocal struct{ text string }

func (l *fakeLocal) ReadText(context.Context) (string, error) { return l.text, nil }
func (l *fakeLocal) WriteText(_ context.Context, s string) error {
	l.text = s
	return nil
}

func TestGetTextMatchesInternalResponse(t *testing.T) {
	fr := newFakeRelay()
	c := New(fr, &fakeLocal{}, Config{Target: "phone", SessionID: "s", RequestTimeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		text, err := c.GetText(ctx, "cid")
		done <- result{text, err}
	}()

	var sent relay.Received
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) > 0 {
			sent = fr.sent[0]
		}
		fr.mu.Unlock()
		if sent.Payload != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if sent.Payload == nil {
		t.Fatal("no request")
	}

	pm, err := platform.Unmarshal(sent.Payload)
	if err != nil {
		t.Fatal(err)
	}
	rid, ok := pm.Header(platform.HeaderRequestID)
	if !ok {
		t.Fatal("no request id")
	}
	text := "from phone"
	clip := proto.Response{
		Items: []proto.Item{{Type: proto.ItemTextPlain, Text: &text}},
		Status: proto.ResponseOK, CorrelationID: "cid",
	}
	drm := proto.DeviceResourceResponse{
		ResponseType: proto.DeviceResourceResponseSuccess,
		Payload:      proto.MarshalResponse(clip),
	}
	resp := platform.NewInternalResponse(proto.MarshalDeviceResourceResponse(drm), rid)
	wire, _ := platform.Marshal(resp)
	fr.recv <- relay.Received{
		Source: "phone", SessionID: "s",
		TransportMessageType: dcg.TransportMessageTypePlatform, Payload: wire,
	}

	select {
	case got := <-done:
		if got.err != nil || got.text != text {
			t.Fatalf("text=%q err=%v", got.text, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestIncomingContentRequestReturnsLocalClipboard(t *testing.T) {
	fr := newFakeRelay()
	local := &fakeLocal{text: "desktop text"}
	c := New(fr, local, Config{Target: "phone", SessionID: "s"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	req := proto.MarshalDeviceResourceMessage(proto.WrapClipboardRequest(proto.NewContentRequest("cid")))
	pm := platform.NewDeviceResourceRequest(req, "req-1")
	wire, _ := platform.Marshal(pm)
	fr.recv <- relay.Received{
		Source: "phone", SessionID: "s",
		TransportMessageType: dcg.TransportMessageTypePlatform, Payload: wire,
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) > 0 {
			sent := fr.sent[0]
			fr.mu.Unlock()
			rpm, err := platform.Unmarshal(sent.Payload)
			if err != nil {
				t.Fatal(err)
			}
			orig, _ := rpm.Header(platform.HeaderOriginalRequestID)
			if orig != "req-1" {
				t.Fatalf("orig=%q", orig)
			}
			drm, err := proto.UnmarshalDeviceResourceResponse(rpm.Payload)
			if err != nil {
				t.Fatal(err)
			}
			clip, err := proto.UnmarshalResponse(drm.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(clip.Items) != 1 || clip.Items[0].Text == nil || *clip.Items[0].Text != "desktop text" {
				t.Fatalf("clip=%#v", clip)
			}
			return
		}
		fr.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no response")
}

func TestPublishLocalChangeBuildsContextPublishMSAEP(t *testing.T) {
	fr := newFakeRelay()
	c := New(fr, &fakeLocal{}, Config{
		Target: "phone", SessionID: "session", SelfDcgClientID: "desktop-dcg",
	})

	correlationID, err := c.PublishLocalChange(context.Background(), "clip-cid")
	if err != nil {
		t.Fatal(err)
	}
	if correlationID != "clip-cid" {
		t.Fatalf("correlation id=%q", correlationID)
	}

	fr.mu.Lock()
	if len(fr.sent) != 1 {
		fr.mu.Unlock()
		t.Fatalf("sent=%d", len(fr.sent))
	}
	sent := fr.sent[0]
	fr.mu.Unlock()

	if sent.TransportMessageType != dcg.TransportMessageTypePlatform {
		t.Fatalf("transport type=%d", sent.TransportMessageType)
	}
	pm, err := platform.Unmarshal(sent.Payload)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := pm.Header(platform.HeaderRoute)
	if route != platform.RouteContextPublish {
		t.Fatalf("route=%q", route)
	}
	env, err := msaep.Unmarshal(pm.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if env.MessageTag != int32(proto.ClipboardMessageTag) || env.DcgClientID != "desktop-dcg" {
		t.Fatalf("envelope=%#v", env)
	}
	pubsub, err := proto.UnmarshalPubSubPayload(env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(pubsub.Data) == 0 || len(pubsub.Additional) != 0 {
		t.Fatalf("pubsub=%#v", pubsub)
	}
	change, err := proto.UnmarshalResponse(pubsub.Data)
	if err != nil {
		t.Fatal(err)
	}
	if change.Status != proto.ResponseClipboardChange || change.CorrelationID != "clip-cid" {
		t.Fatalf("change=%#v", change)
	}
}

func TestPublishLocalTextSnapshotsContentByCorrelation(t *testing.T) {
	fr := newFakeRelay()
	local := &fakeLocal{text: "newer clipboard"}
	c := New(fr, local, Config{
		Target: "phone", SessionID: "session", SelfDcgClientID: "desktop-dcg",
	})

	correlationID, err := c.PublishLocalText(context.Background(), "published snapshot", "cid-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if correlationID != "cid-snapshot" {
		t.Fatalf("correlation id=%q", correlationID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	req := proto.MarshalDeviceResourceMessage(
		proto.WrapClipboardRequest(proto.NewContentRequest(correlationID)),
	)
	pm := platform.NewDeviceResourceRequest(req, "req-content")
	wire, _ := platform.Marshal(pm)
	fr.recv <- relay.Received{
		Source: "phone", SessionID: "session",
		TransportMessageType: dcg.TransportMessageTypePlatform, Payload: wire,
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) >= 2 {
			sent := fr.sent[1]
			fr.mu.Unlock()
			rpm, err := platform.Unmarshal(sent.Payload)
			if err != nil {
				t.Fatal(err)
			}
			drm, err := proto.UnmarshalDeviceResourceResponse(rpm.Payload)
			if err != nil {
				t.Fatal(err)
			}
			clip, err := proto.UnmarshalResponse(drm.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(clip.Items) != 1 ||
				clip.Items[0].Text == nil ||
				*clip.Items[0].Text != "published snapshot" {
				t.Fatalf("clip=%#v", clip)
			}
			return
		}
		fr.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no CONTENT response")
}

type countingLocal struct {
	text   string
	writes int
}

func (l *countingLocal) ReadText(context.Context) (string, error) {
	return l.text, nil
}

func (l *countingLocal) WriteText(_ context.Context, text string) error {
	l.text = text
	l.writes++
	return nil
}

func TestPullToLocalSkipsIdenticalReflectedText(t *testing.T) {
	fr := newFakeRelay()
	local := &countingLocal{text: "same text"}
	c := New(fr, local, Config{
		Target: "phone",
		SessionID: "session",
		RequestTimeout: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	done := make(chan error, 1)
	go func() {
		done <- c.PullToLocal(ctx)
	}()

	var request relay.Received
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) != 0 {
			request = fr.sent[0]
		}
		fr.mu.Unlock()
		if request.Payload != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if request.Payload == nil {
		t.Fatal("no CONTENT request")
	}
	pm, err := platform.Unmarshal(request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	requestID, _ := pm.Header(platform.HeaderRequestID)
	response := proto.NewTextResponse("", "same text", nil)
	drm := proto.DeviceResourceResponse{
		ResponseType: proto.DeviceResourceResponseSuccess,
		Payload:      proto.MarshalResponse(response),
	}
	reply := platform.NewInternalResponse(
		proto.MarshalDeviceResourceResponse(drm),
		requestID,
	)
	wire, err := platform.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source: "phone",
		SessionID: "session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload: wire,
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		if local.writes != 0 {
			t.Fatalf("writes=%d", local.writes)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

type orderedLocal struct {
	mu     sync.Mutex
	text   string
	writes []string
}

func (l *orderedLocal) ReadText(context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text, nil
}

func (l *orderedLocal) WriteText(_ context.Context, text string) error {
	l.mu.Lock()
	l.text = text
	l.writes = append(l.writes, text)
	l.mu.Unlock()
	return nil
}

func phoneClipboardPublicationWire(t *testing.T, correlationID string) []byte {
	t.Helper()
	publication := proto.PubSubPayload{
		Additional: proto.MarshalResponse(proto.NewClipboardChange(correlationID)),
	}
	envelope := msaep.New(
		"phone-message-"+correlationID,
		"phone",
		int32(proto.ClipboardMessageTag),
		proto.MarshalPubSubPayload(publication),
	)
	pm := platform.NewContextPublish(msaep.Marshal(envelope), "phone-pub-"+correlationID)
	wire, err := platform.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func waitForClipboardRequest(
	t *testing.T,
	fr *fakeRelay,
	index int,
) (relay.Received, platform.Message, proto.Request) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) > index {
			sent := fr.sent[index]
			fr.mu.Unlock()
			pm, err := platform.Unmarshal(sent.Payload)
			if err != nil {
				t.Fatal(err)
			}
			drm, err := proto.UnmarshalDeviceResourceMessage(pm.Payload)
			if err != nil {
				t.Fatal(err)
			}
			req, err := proto.UnmarshalRequest(drm.Payload)
			if err != nil {
				t.Fatal(err)
			}
			return sent, pm, req
		}
		fr.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no clipboard request at index %d", index)
	return relay.Received{}, platform.Message{}, proto.Request{}
}

func TestNewPhonePublicationCancelsOlderPullAndAppliesLatest(t *testing.T) {
	fr := newFakeRelay()
	local := &orderedLocal{text: "desktop"}
	c := New(fr, local, Config{
		Target: "phone",
		SessionID: "session",
		RequestTimeout: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	fr.recv <- relay.Received{
		Source: "phone",
		SessionID: "session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload: phoneClipboardPublicationWire(t, "cid-a"),
	}

	_, requestA, reqA := waitForClipboardRequest(t, fr, 0)
	if reqA.Type != proto.RequestContent || reqA.CorrelationID != "cid-a" {
		t.Fatalf("request A=%#v", reqA)
	}
	requestIDA, _ := requestA.Header(platform.HeaderRequestID)

	fr.recv <- relay.Received{
		Source: "phone",
		SessionID: "session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload: phoneClipboardPublicationWire(t, "cid-b"),
	}

	_, requestB, reqB := waitForClipboardRequest(t, fr, 1)
	if reqB.Type != proto.RequestContent || reqB.CorrelationID != "cid-b" {
		t.Fatalf("request B=%#v", reqB)
	}
	requestIDB, _ := requestB.Header(platform.HeaderRequestID)

	// A late response for the canceled request must be harmless.
	textA := "stale A"
	responseA := proto.NewTextResponse("cid-a", textA, nil)
	drmA := proto.DeviceResourceResponse{
		ResponseType: proto.DeviceResourceResponseSuccess,
		Payload: proto.MarshalResponse(responseA),
	}
	replyA := platform.NewInternalResponse(
		proto.MarshalDeviceResourceResponse(drmA),
		requestIDA,
	)
	wireA, err := platform.Marshal(replyA)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source: "phone",
		SessionID: "session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload: wireA,
	}

	textB := "latest B"
	responseB := proto.NewTextResponse("cid-b", textB, nil)
	drmB := proto.DeviceResourceResponse{
		ResponseType: proto.DeviceResourceResponseSuccess,
		Payload: proto.MarshalResponse(responseB),
	}
	replyB := platform.NewInternalResponse(
		proto.MarshalDeviceResourceResponse(drmB),
		requestIDB,
	)
	wireB, err := platform.Marshal(replyB)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source: "phone",
		SessionID: "session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload: wireB,
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		local.mu.Lock()
		text := local.text
		writes := append([]string(nil), local.writes...)
		local.mu.Unlock()
		if text == textB {
			if len(writes) != 1 || writes[0] != textB {
				t.Fatalf("writes=%#v", writes)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("latest phone publication was not applied")
}

func TestPhonePublicationFromOtherPeerIsIgnored(t *testing.T) {
	fr := newFakeRelay()
	c := New(fr, &fakeLocal{}, Config{
		Target: "phone",
		SessionID: "session",
		RequestTimeout: 50 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	fr.recv <- relay.Received{
		Source: "other-phone",
		SessionID: "session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload: phoneClipboardPublicationWire(t, "cid-other"),
	}

	time.Sleep(20 * time.Millisecond)
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if len(fr.sent) != 0 {
		t.Fatalf("unexpected request to configured target: sent=%d", len(fr.sent))
	}
}

func TestDuplicateInternalResponseDoesNotBlockReceiveLoop(t *testing.T) {
	fr := newFakeRelay()
	c := New(fr, &fakeLocal{}, Config{Target: "phone"})

	full := make(chan pendingResponse, 1)
	full <- pendingResponse{}
	c.mu.Lock()
	c.pending["rid-full"] = full
	c.mu.Unlock()

	drm := proto.DeviceResourceResponse{
		ResponseType: proto.DeviceResourceResponseSuccess,
	}
	pm := platform.NewInternalResponse(
		proto.MarshalDeviceResourceResponse(drm),
		"rid-full",
	)
	wire, err := platform.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- c.handlePlatform(context.Background(), relay.Received{
			Source: "phone",
			TransportMessageType: dcg.TransportMessageTypePlatform,
			Payload: wire,
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("duplicate internal response blocked receive handling")
	}
}

func TestPublishedSnapshotSurvivesDuplicateContentRequest(t *testing.T) {
	fr := newFakeRelay()
	local := &fakeLocal{text: "newer clipboard"}
	c := New(fr, local, Config{
		Target: "phone",
		SessionID: "session",
		SelfDcgClientID: "desktop-dcg",
	})

	if _, err := c.PublishLocalText(
		context.Background(),
		"stable snapshot",
		"cid-retry",
	); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	for i := 0; i < 2; i++ {
		req := proto.MarshalDeviceResourceMessage(
			proto.WrapClipboardRequest(proto.NewContentRequest("cid-retry")),
		)
		pm := platform.NewDeviceResourceRequest(req, fmt.Sprintf("content-%d", i))
		wire, err := platform.Marshal(pm)
		if err != nil {
			t.Fatal(err)
		}
		fr.recv <- relay.Received{
			Source: "phone",
			SessionID: "session",
			TransportMessageType: dcg.TransportMessageTypePlatform,
			Payload: wire,
		}

		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			fr.mu.Lock()
			if len(fr.sent) >= 2+i {
				sent := fr.sent[1+i]
				fr.mu.Unlock()
				reply, err := platform.Unmarshal(sent.Payload)
				if err != nil {
					t.Fatal(err)
				}
				drmReply, err := proto.UnmarshalDeviceResourceResponse(reply.Payload)
				if err != nil {
					t.Fatal(err)
				}
				clipReply, err := proto.UnmarshalResponse(drmReply.Payload)
				if err != nil {
					t.Fatal(err)
				}
				if len(clipReply.Items) != 1 ||
					clipReply.Items[0].Text == nil ||
					*clipReply.Items[0].Text != "stable snapshot" {
					t.Fatalf("reply %d=%#v", i, clipReply)
				}
				break
			}
			fr.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}
}
