package clipboard

import (
	"context"
	"testing"
	"time"

	proto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

func TestExpiredPublishedCorrelationNeverReturnsCurrentClipboard(t *testing.T) {
	fr := newFakeRelay()
	local := &fakeLocal{text: "unrelated-current"}
	c := New(fr, local, Config{Target: "phone", SessionID: "session"})

	c.mu.Lock()
	c.published["expired-cid"] = publishedSnapshot{
		text:      "old-snapshot",
		createdAt: time.Now().Add(-publishedSnapshotTTL - time.Second),
	}
	c.mu.Unlock()

	req := proto.MarshalDeviceResourceMessage(
		proto.WrapClipboardRequest(proto.NewContentRequest("expired-cid")),
	)
	pm := platform.NewDeviceResourceRequest(req, "expired-request")
	wire, err := platform.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handlePlatform(context.Background(), relay.Received{
		Source:               "phone",
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
	if len(fr.sent) != 1 {
		fr.mu.Unlock()
		t.Fatalf("responses=%d want=1", len(fr.sent))
	}
	responseWire := append([]byte(nil), fr.sent[0].Payload...)
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
	if response.Status != proto.ResponseInvalidContent ||
		response.CorrelationID != "expired-cid" ||
		len(response.Items) != 0 {
		t.Fatalf("response=%#v", response)
	}
}
