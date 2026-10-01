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

func TestSupersededSnapshotRejectsStaleContentRequest(t *testing.T) {
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
	if clipboardResponse.Status != proto.ResponseInvalidContent ||
		clipboardResponse.CorrelationID != "cid-old" {
		t.Fatalf("response=%#v", clipboardResponse)
	}
}

func TestNewerLocalGenerationKeepsOlderCorrelationSnapshot(t *testing.T) {
	fr := newFakeRelay()
	local := &fakeLocal{text: "newer-local"}
	c := New(fr, local, Config{
		Target:          "phone",
		SessionID:       "session",
		SelfDcgClientID: "desktop",
	})

	genA := c.ReserveLocalGeneration()
	if _, err := c.PublishLocalTextGeneration(
		context.Background(),
		"local-A",
		"cid-A",
		genA,
	); err != nil {
		t.Fatal(err)
	}
	_ = c.ReserveLocalGeneration()

	req := proto.MarshalDeviceResourceMessage(
		proto.WrapClipboardRequest(proto.NewContentRequest("cid-A")),
	)
	pm := platform.NewDeviceResourceRequest(req, "req-A")
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
	queued := <-c.incomingRequests
	if err := c.handleIncomingRequest(context.Background(), queued.msg, queued.pm); err != nil {
		t.Fatal(err)
	}

	fr.mu.Lock()
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
	response, err := proto.UnmarshalResponse(drm.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 ||
		response.Items[0].Text == nil ||
		*response.Items[0].Text != "local-A" {
		t.Fatalf("response=%#v", response)
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
