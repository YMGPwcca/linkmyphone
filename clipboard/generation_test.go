package clipboard

import (
	"context"
	"errors"
	"testing"

	proto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

func TestVersionedPublicationSupersededBeforeSend(t *testing.T) {
	fr := newFakeRelay()
	c := New(fr, &fakeLocal{}, Config{
		Target:          "phone",
		SessionID:       "session",
		SelfDcgClientID: "desktop",
	})

	c.SupersedeLocalPublications(2)
	if _, err := c.PublishLocalTextGeneration(
		context.Background(),
		"old",
		"cid-old",
		1,
	); !errors.Is(err, ErrPublicationSuperseded) {
		t.Fatalf("err=%v", err)
	}

	fr.mu.Lock()
	defer fr.mu.Unlock()
	if len(fr.sent) != 0 {
		t.Fatalf("superseded publication reached relay: sent=%d", len(fr.sent))
	}
}

func TestSupersededSnapshotFallsBackToCurrentLocalClipboard(t *testing.T) {
	fr := newFakeRelay()
	local := &fakeLocal{text: "phone-new"}
	c := New(fr, local, Config{
		Target:          "phone",
		SessionID:       "session",
		SelfDcgClientID: "desktop",
	})

	if _, err := c.PublishLocalTextGeneration(
		context.Background(),
		"linux-old",
		"cid-old",
		1,
	); err != nil {
		t.Fatal(err)
	}
	c.SupersedeLocalPublications(2)

	req := proto.MarshalDeviceResourceMessage(
		proto.WrapClipboardRequest(proto.NewContentRequest("cid-old")),
	)
	pm := platform.NewDeviceResourceRequest(req, "req-content")
	wire, err := platform.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handlePlatform(context.Background(), relay.Received{
		Source:               "phone",
		SessionID:            "session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              wire,
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case queued := <-c.incomingRequests:
		if err := c.handleIncomingRequest(context.Background(), queued.msg, queued.pm); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("CONTENT request was not queued")
	}

	fr.mu.Lock()
	if len(fr.sent) < 2 {
		fr.mu.Unlock()
		t.Fatalf("sent=%d want at least 2", len(fr.sent))
	}
	responseWire := append([]byte(nil), fr.sent[len(fr.sent)-1].Payload...)
	fr.mu.Unlock()

	responseMessage, err := platform.Unmarshal(responseWire)
	if err != nil {
		t.Fatal(err)
	}
	drm, err := proto.UnmarshalDeviceResourceResponse(responseMessage.Payload)
	if err != nil {
		t.Fatal(err)
	}
	clipboardResponse, err := proto.UnmarshalResponse(drm.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(clipboardResponse.Items) != 1 ||
		clipboardResponse.Items[0].Text == nil ||
		*clipboardResponse.Items[0].Text != "phone-new" {
		t.Fatalf("response=%#v", clipboardResponse)
	}
}

func TestIncomingRequestQueueFailsWhenFull(t *testing.T) {
	c := New(newFakeRelay(), &fakeLocal{}, Config{Target: "phone"})
	for i := 0; i < cap(c.incomingRequests); i++ {
		c.incomingRequests <- incomingRequest{}
	}
	if err := c.enqueueIncomingRequest(relay.Received{}, platform.Message{}); err == nil {
		t.Fatal("expected bounded request queue overflow error")
	}
}
