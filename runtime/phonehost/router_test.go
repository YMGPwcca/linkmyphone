package phonehost

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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
	router := newRouter(transport)

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

func TestRouterOverflowRevokesOnlySlowSubscriber(t *testing.T) {
	transport := newFakeTransport()
	router := newRouter(transport)

	slow, err := router.Subscribe("slow", func(relay.Received) bool { return true }, 1)
	if err != nil {
		t.Fatal(err)
	}
	fast, err := router.Subscribe("fast", func(relay.Received) bool { return true }, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()

	router.dispatch(relay.Received{MessageID: 1})
	router.dispatch(relay.Received{MessageID: 2})

	// The slow subscriber filled on message 1 and is revoked on message 2.
	first, ok := <-slow.Received()
	if !ok || first.MessageID != 1 {
		t.Fatalf("slow first=%#v ok=%t", first, ok)
	}
	if _, ok := <-slow.Received(); ok {
		t.Fatal("overflowed subscriber channel remained open")
	}
	if err := slow.Send(
		context.Background(),
		"phone",
		"session",
		dcg.TransportMessageTypePlatform,
		[]byte("must-not-send"),
	); err == nil {
		t.Fatal("overflowed subscriber retained send capability")
	}

	// The healthy subscriber remains alive and receives both messages.
	for _, want := range []int{1, 2} {
		got, ok := <-fast.Received()
		if !ok || got.MessageID != want {
			t.Fatalf("fast got=%#v ok=%t want=%d", got, ok, want)
		}
	}
}

func TestEndpointCloseRevokesSubscription(t *testing.T) {
	transport := newFakeTransport()
	router := newRouter(transport)
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
	router := newRouter(transport)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := router.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestSessionSubscribeStartsRouterAfterRegistration(t *testing.T) {
	transport := newFakeTransport()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session := &Session{
		router: newRouter(transport),
		runCtx: ctx,
		cancel: cancel,
		errors: make(chan error, 1),
	}
	endpoint, err := session.Subscribe(
		"feature",
		func(relay.Received) bool { return true },
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()

	transport.recv <- relay.Received{
		Source: "phone",
		MessageID: 7,
		Payload: []byte("ready"),
	}

	select {
	case got, ok := <-endpoint.Received():
		if !ok || got.MessageID != 7 || string(got.Payload) != "ready" {
			t.Fatalf("got=%#v ok=%t", got, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("router did not deliver message after first subscription")
	}
}

func TestRouterRunDrainsWithoutSubscribers(t *testing.T) {
	transport := newFakeTransport()
	transport.recv = make(chan relay.Received)
	router := newRouter(transport)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- router.Run(ctx)
	}()

	sent := make(chan struct{})
	go func() {
		transport.recv <- relay.Received{
			Source:               "phone",
			MessageID:            1,
			TransportMessageType: dcg.TransportMessageTypePlatform,
			Payload:              []byte("unmatched"),
		}
		close(sent)
	}()

	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("router did not drain raw relay traffic without subscribers")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("router err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("router did not stop after cancellation")
	}
}
