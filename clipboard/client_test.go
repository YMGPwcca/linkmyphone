package clipboard

import (
	"context"
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
