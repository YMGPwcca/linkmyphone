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

type generationLocal struct {
	mu     sync.Mutex
	text   string
	writes int
}

func (l *generationLocal) ReadText(context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text, nil
}

func (l *generationLocal) WriteText(_ context.Context, text string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.text = text
	l.writes++
	return nil
}

func TestNewerLocalGenerationDropsOlderPhoneResponse(t *testing.T) {
	fr := newFakeRelay()
	local := &generationLocal{text: "before"}
	applied := make(chan RemoteApplyEvent, 1)
	c := New(fr, local, Config{
		Target:         "phone",
		SessionID:      "session",
		RequestTimeout: time.Second,
		OnRemoteApplied: func(event RemoteApplyEvent) {
			applied <- event
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	publication := proto.PubSubPayload{
		Additional: proto.MarshalResponse(proto.NewClipboardChange("phone-cid")),
	}
	envelope := msaep.New(
		"phone-message",
		"phone",
		int32(proto.ClipboardMessageTag),
		proto.MarshalPubSubPayload(publication),
	)
	contextPublish := platform.NewContextPublish(msaep.Marshal(envelope), "phone-pub")
	wire, err := platform.Marshal(contextPublish)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		SessionID:            "session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              wire,
	}

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
		t.Fatal("phone CONTENT request was not sent")
	}

	// The local copy is observed after the phone publication but before the
	// phone CONTENT response arrives, so the local generation must win.
	_ = c.ReserveLocalGeneration()
	local.mu.Lock()
	local.text = "newer-local"
	local.mu.Unlock()

	requestMessage, err := platform.Unmarshal(request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	requestID, _ := requestMessage.Header(platform.HeaderRequestID)
	response := proto.NewTextResponse("phone-cid", "stale-phone", nil)
	drm := proto.DeviceResourceResponse{
		ResponseType: proto.DeviceResourceResponseSuccess,
		Payload:      proto.MarshalResponse(response),
	}
	reply := platform.NewInternalResponse(
		proto.MarshalDeviceResourceResponse(drm),
		requestID,
	)
	replyWire, err := platform.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		SessionID:            "session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              replyWire,
	}

	time.Sleep(20 * time.Millisecond)
	local.mu.Lock()
	text := local.text
	writes := local.writes
	local.mu.Unlock()
	if text != "newer-local" || writes != 0 {
		t.Fatalf("text=%q writes=%d", text, writes)
	}
	select {
	case event := <-applied:
		t.Fatalf("stale phone content reported as applied: %#v", event)
	default:
	}
}
