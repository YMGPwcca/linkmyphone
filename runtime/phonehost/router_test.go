package phonehost

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

type fakeTransport struct {
	recv chan relay.Received
	mu   sync.Mutex
	sent []relay.Received
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{recv: make(chan relay.Received, 8)}
}

func (f *fakeTransport) Send(
	_ context.Context,
	target string,
	sessionID string,
	messageType dcg.TransportMessageType,
	payload []byte,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, relay.Received{
		Source:               target,
		SessionID:            sessionID,
		TransportMessageType: messageType,
		Payload:              append([]byte(nil), payload...),
	})
	return nil
}

func (f *fakeTransport) Received() <-chan relay.Received {
	return f.recv
}

func TestRouterFanoutAndFilter(t *testing.T) {
	transport := newFakeTransport()
	router := newRouter(transport, nil)

	a, err := router.Subscribe("a", func(message relay.Received) bool {
		return message.MessageID%2 == 0
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	b, err := router.Subscribe("b", func(relay.Received) bool { return true }, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	message := relay.Received{
		Source:    "phone",
		MessageID: 2,
		Payload:   []byte("payload"),
	}
	router.dispatch(message)
	message.Payload[0] = 'X'

	gotA := <-a.Received()
	gotB := <-b.Received()
	if string(gotA.Payload) != "payload" || string(gotB.Payload) != "payload" {
		t.Fatalf("fanout payloads: A=%q B=%q", gotA.Payload, gotB.Payload)
	}

	router.dispatch(relay.Received{MessageID: 3})
	select {
	case <-a.Received():
		t.Fatal("filtered endpoint received unmatched message")
	default:
	}
	select {
	case <-b.Received():
	default:
		t.Fatal("unfiltered endpoint did not receive message")
	}
}

func TestRouterReportsBoundedSubscriberOverflow(t *testing.T) {
	transport := newFakeTransport()
	errs := make(chan error, 1)
	router := newRouter(transport, func(err error) {
		select {
		case errs <- err:
		default:
		}
	})
	endpoint, err := router.Subscribe("slow", func(relay.Received) bool { return true }, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()

	router.dispatch(relay.Received{MessageID: 1})
	router.dispatch(relay.Received{MessageID: 2})

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("expected overflow error")
		}
	default:
		t.Fatal("subscriber overflow was not reported")
	}
}

func TestEndpointCloseRevokesSubscription(t *testing.T) {
	transport := newFakeTransport()
	router := newRouter(transport, nil)
	endpoint, err := router.Subscribe("feature", func(relay.Received) bool { return true }, 1)
	if err != nil {
		t.Fatal(err)
	}
	endpoint.Close()
	endpoint.Close()

	router.dispatch(relay.Received{MessageID: 1})
	if err := endpoint.Send(
		context.Background(),
		"phone",
		"session",
		dcg.TransportMessageTypePlatform,
		[]byte("x"),
	); err == nil {
		t.Fatal("closed endpoint accepted send")
	}

	_, ok := <-endpoint.Received()
	if ok {
		t.Fatal("closed endpoint receive channel remained open")
	}
}

func TestRouterRunHonorsCancellation(t *testing.T) {
	transport := newFakeTransport()
	router := newRouter(transport, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := router.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
