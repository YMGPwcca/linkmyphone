package phonehost

import (
	"context"
	"errors"
	"sync"

	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

const DefaultSubscriptionQueue = 64

type Matcher func(relay.Received) bool

type transport interface {
	Send(context.Context, string, string, dcg.TransportMessageType, []byte) error
	Received() <-chan relay.Received
}

type Router struct {
	transport transport

	mu          sync.RWMutex
	subscribers map[uint64]*subscriber
	nextID      uint64
}

type subscriber struct {
	name      string
	matcher   Matcher
	recv      chan relay.Received
	revoked   chan struct{}
	closeOnce sync.Once
}

func (s *subscriber) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.revoked)
		close(s.recv)
	})
}

type Endpoint struct {
	mu        sync.RWMutex
	router    *Router
	transport transport
	id        uint64
	recv      <-chan relay.Received
	revoked   <-chan struct{}
	closeOnce sync.Once
}

func newRouter(transport transport) *Router {
	return &Router{
		transport:   transport,
		subscribers: make(map[uint64]*subscriber),
	}
}

func (r *Router) Subscribe(name string, matcher Matcher, queueSize int) (*Endpoint, error) {
	if r == nil || r.transport == nil {
		return nil, errors.New("phonehost: router is unavailable")
	}
	if name == "" {
		return nil, errors.New("phonehost: subscription name is required")
	}
	if matcher == nil {
		return nil, errors.New("phonehost: subscription matcher is required")
	}
	if queueSize <= 0 {
		queueSize = DefaultSubscriptionQueue
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	ch := make(chan relay.Received, queueSize)
	sub := &subscriber{
		name:    name,
		matcher: matcher,
		recv:    ch,
		revoked: make(chan struct{}),
	}
	r.subscribers[id] = sub
	return &Endpoint{
		router:    r,
		transport: r.transport,
		id:        id,
		recv:      ch,
		revoked:   sub.revoked,
	}, nil
}

func (r *Router) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case message, ok := <-r.transport.Received():
			if !ok {
				return errors.New("phonehost: relay receive stream closed")
			}
			r.dispatch(message)
		}
	}
}

func (r *Router) dispatch(message relay.Received) {
	r.mu.RLock()
	overflowed := make([]uint64, 0)
	for id, sub := range r.subscribers {
		if !sub.matcher(message) {
			continue
		}
		select {
		case sub.recv <- cloneReceived(message):
		default:
			overflowed = append(overflowed, id)
		}
	}
	r.mu.RUnlock()

	// A feature queue overflow is a feature failure, not a shared transport
	// failure. Revoke only that subscription; the feature's receive channel
	// closes and its client/lifecycle reports failure without taking down the
	// Phone Link host or unrelated modules.
	for _, id := range overflowed {
		r.unsubscribe(id)
	}
}

func (r *Router) unsubscribe(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub := r.subscribers[id]
	if sub == nil {
		return
	}
	delete(r.subscribers, id)
	sub.close()
}

func (r *Router) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, sub := range r.subscribers {
		delete(r.subscribers, id)
		sub.close()
	}
}

func (e *Endpoint) Send(
	ctx context.Context,
	target string,
	sessionID string,
	messageType dcg.TransportMessageType,
	payload []byte,
) error {
	if e == nil {
		return errors.New("phonehost: endpoint is closed")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.transport == nil {
		return errors.New("phonehost: endpoint is closed")
	}
	select {
	case <-e.revoked:
		return errors.New("phonehost: endpoint is revoked")
	default:
	}
	return e.transport.Send(ctx, target, sessionID, messageType, payload)
}

func (e *Endpoint) Received() <-chan relay.Received {
	if e == nil {
		return nil
	}
	return e.recv
}

func (e *Endpoint) Close() {
	if e == nil {
		return
	}
	e.closeOnce.Do(func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.router != nil {
			e.router.unsubscribe(e.id)
		}
		e.router = nil
		e.transport = nil
	})
}

func cloneReceived(message relay.Received) relay.Received {
	message.Payload = append([]byte(nil), message.Payload...)
	return message
}
