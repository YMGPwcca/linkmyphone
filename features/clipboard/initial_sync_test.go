package clipboard

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	clipclient "github.com/YMGPwcca/linkmyphone/clipboard"
	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	"github.com/YMGPwcca/linkmyphone/protocol/platform"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
)

type initialRelay struct {
	received chan relay.Received
	sent     chan relay.Received
}

func (r *initialRelay) Send(_ context.Context, target, session string, kind dcg.TransportMessageType, payload []byte) error {
	r.sent <- relay.Received{Source: target, SessionID: session, TransportMessageType: kind, Payload: payload}
	return nil
}

func (r *initialRelay) Received() <-chan relay.Received { return r.received }

type initialReporter struct{ events chan kernel.Event }

func (r initialReporter) Report(event kernel.Event) { r.events <- event }

func TestInitialClipboardWaitsForPhoneLinkConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := &initialRelay{received: make(chan relay.Received), sent: make(chan relay.Received, 4)}
	local := newTrackedLocalClipboard(&fakeLocal{text: "initial"}, "initial")
	client := clipclient.New(transport, local, clipclient.Config{Target: "phone", SelfDcgClientID: "desktop"})
	queue, results := startClipboardPublisher(ctx, client)
	ready := make(chan struct{})
	reporter := initialReporter{events: make(chan kernel.Event, 4)}
	i := &instance{
		moduleID:       "linkmyphone.clipboard",
		cfg:            Config{PollIntervalMS: 1000},
		reporter:       reporter,
		client:         client,
		local:          local,
		remoteApplied:  make(chan clipclient.RemoteApplyEvent, 8),
		remoteSeen:     &atomic.Bool{},
		initialReady:   ready,
		publishQueue:   queue,
		publishResults: results,
		cancel:         cancel,
		done:           make(chan struct{}),
		errors:         make(chan error, 1),
	}
	go i.run(ctx, make(chan error))
	select {
	case <-reporter.events:
	case <-time.After(time.Second):
		t.Fatal("local observer did not start")
	}
	select {
	case <-transport.sent:
		t.Fatal("initial clipboard was published before phone connection")
	case <-time.After(20 * time.Millisecond):
	}
	close(ready)
	select {
	case sent := <-transport.sent:
		frame, err := platform.Unmarshal(sent.Payload)
		if err != nil {
			t.Fatal(err)
		}
		route, _ := frame.Header(platform.HeaderRoute)
		if route != platform.RouteContextPublish {
			t.Fatalf("published route=%q", route)
		}
	case <-time.After(time.Second):
		t.Fatal("initial clipboard was not published after phone connection")
	}
	cancel()
	select {
	case <-i.done:
	case <-time.After(time.Second):
		t.Fatal("clipboard instance did not stop")
	}
}
