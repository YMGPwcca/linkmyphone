package relay

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	psignalr "github.com/YMGPwcca/linkmyphone/protocol/signalr"
)

const (
	DefaultFragmentSize = 64 * 1024
	DefaultAckTimeout   = 5 * time.Second
	DefaultAckRetries   = 2
)

type Hub interface {
	SendBinary([]byte) error
	ReadBinary() ([]byte, error)
	Close() error
}

type Config struct {
	FragmentSize int
	AckTimeout   time.Duration
	AckRetries   int
}

type Received struct {
	Source               string
	SessionID            string
	MessageID            int
	TransportMessageType dcg.TransportMessageType
	Payload              []byte
}

type pendingKey struct {
	source    string
	sessionID string
	sequence  int
}

type Client struct {
	hub Hub

	fragmentSize int
	ackTimeout   time.Duration
	ackRetries   int

	reassembler *dcg.Reassembler
	messageID   atomic.Int64
	invocation  atomic.Uint64

	mu                 sync.Mutex
	sendGates          map[string]chan struct{}
	sequencers         map[string]*dcg.Sequencer
	sessions           map[string]string
	pending            map[pendingKey]chan dcg.Ack
	completions        map[string]chan psignalr.Completion
	partners           map[string]bool
	partnerDisconnects map[string]uint64
	partnerWaiters     map[string][]chan struct{}
	hubConnected       *psignalr.OnConnectedPayload
	hubWaiters         []chan psignalr.OnConnectedPayload

	received chan Received
}

func New(hub Hub, cfg Config) *Client {
	if cfg.FragmentSize <= 0 {
		cfg.FragmentSize = DefaultFragmentSize
	}
	if cfg.AckTimeout <= 0 {
		cfg.AckTimeout = DefaultAckTimeout
	}
	if cfg.AckRetries < 0 {
		cfg.AckRetries = 0
	}
	if cfg.AckRetries == 0 {
		cfg.AckRetries = DefaultAckRetries
	}
	return &Client{
		hub:                hub,
		fragmentSize:       cfg.FragmentSize,
		ackTimeout:         cfg.AckTimeout,
		ackRetries:         cfg.AckRetries,
		reassembler:        dcg.NewReassembler(32<<20, 4096),
		sendGates:          make(map[string]chan struct{}),
		sequencers:         make(map[string]*dcg.Sequencer),
		sessions:           make(map[string]string),
		pending:            make(map[pendingKey]chan dcg.Ack),
		completions:        make(map[string]chan psignalr.Completion),
		partners:           make(map[string]bool),
		partnerDisconnects: make(map[string]uint64),
		partnerWaiters:     make(map[string][]chan struct{}),
		received:           make(chan Received, 32),
	}
}

func (c *Client) Received() <-chan Received { return c.received }
func (c *Client) Close() error              { return c.hub.Close() }

// Run owns the hub read side. It must be running while Send waits for DCG ACKs.
func (c *Client) Run(ctx context.Context) error {
	defer close(c.received)
	for {
		payload, err := c.hub.ReadBinary()
		if err != nil {
			return err
		}
		frames, err := psignalr.SplitFrames(payload)
		if err != nil {
			return err
		}
		for _, body := range frames {
			mt, a, err := psignalr.ParseHubMessage(body)
			if err != nil {
				return err
			}
			switch mt {
			case psignalr.HubMessageTypeInvocation:
				inv, err := psignalr.ParseInvocation(body)
				if err != nil {
					return err
				}
				var msg psignalr.ReceiveMessage
				switch inv.Target {
				case psignalr.TargetOnConnected:
					connected, err := psignalr.ParseOnConnected(inv)
					if err != nil {
						return err
					}
					for _, partner := range connected.Partners {
						c.markPartnerConnected(partner)
					}
					c.markHubConnected(connected)
					continue
				case psignalr.TargetOnPartnerConnected:
					partner, err := psignalr.ParseOnPartnerConnected(inv)
					if err != nil {
						return err
					}
					c.markPartnerConnected(partner.SourceDcgClientID)
					// Windows answers OnPartnerConnected with SendConnectedAsync so the
					// partner sees reciprocal presence on the Hub Relay.
					if err := c.SendConnected(partner.SourceDcgClientID, partner.Trace); err != nil {
						return err
					}
					continue
				case psignalr.TargetOnPartnerDisconnected:
					source, err := psignalr.ParseOnPartnerDisconnected(inv)
					if err != nil {
						return err
					}
					c.markPartnerDisconnected(source)
					continue
				case psignalr.TargetOnReceiveMessage:
					msg, err = psignalr.ParseOnReceiveMessage(inv)
				case psignalr.TargetOnReceiveSessionBasedMessage:
					msg, err = psignalr.ParseOnReceiveSessionBasedMessage(inv)
				default:
					continue
				}
				if err != nil {
					return err
				}
				c.markPartnerConnected(msg.SourceDcgClientID)
				if err := c.handlePacket(ctx, msg); err != nil {
					return err
				}
			case psignalr.HubMessageTypeCompletion:
				completion, err := psignalr.ParseCompletion(body)
				if err != nil {
					return err
				}
				c.deliverCompletion(completion)
				continue
			case psignalr.HubMessageTypePing:
				continue
			case psignalr.HubMessageTypeClose:
				return psignalr.CloseError(a)
			default:
				continue
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (c *Client) FlushPartner(ctx context.Context, target string, trace psignalr.TraceContextPacket) error {
	if target == "" {
		return errors.New("relay: target is required")
	}
	var err error
	trace, err = psignalr.NormalizeTraceContextPacket(trace)
	if err != nil {
		return err
	}
	id := strconv.FormatUint(c.invocation.Add(1), 10)
	completionCh := make(chan psignalr.Completion, 1)
	c.mu.Lock()
	c.completions[id] = completionCh
	c.mu.Unlock()
	defer c.removeCompletionWaiter(id)

	frame, err := psignalr.FrameSendConnectedAsync(&id, trace, target)
	if err != nil {
		return err
	}
	if err := c.hub.SendBinary(frame); err != nil {
		return err
	}

	select {
	case completion := <-completionCh:
		if completion.Error != "" {
			return fmt.Errorf("relay: Hub rejected SendConnectedAsync: %s", completion.Error)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("relay: SendConnectedAsync completion: %w", ctx.Err())
	}
}

func (c *Client) SendConnected(target string, trace psignalr.TraceContextPacket) error {
	if target == "" {
		return errors.New("relay: target is required")
	}
	var err error
	trace, err = psignalr.NormalizeTraceContextPacket(trace)
	if err != nil {
		return err
	}
	id := strconv.FormatUint(c.invocation.Add(1), 10)
	frame, err := psignalr.FrameSendConnectedAsync(&id, trace, target)
	if err != nil {
		return err
	}
	return c.hub.SendBinary(frame)
}

func (c *Client) PartnerConnected(target string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.partners[target]
}

// PartnerDisconnects changes even when a phone reconnects between health ticks.
// Its platform session and feature state still need to be renegotiated.
func (c *Client) PartnerDisconnects(target string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.partnerDisconnects[target]
}

func (c *Client) WaitHubConnected(ctx context.Context) (psignalr.OnConnectedPayload, error) {
	c.mu.Lock()
	if c.hubConnected != nil {
		payload := cloneOnConnectedPayload(*c.hubConnected)
		c.mu.Unlock()
		return payload, nil
	}
	waiter := make(chan psignalr.OnConnectedPayload, 1)
	c.hubWaiters = append(c.hubWaiters, waiter)
	c.mu.Unlock()
	defer c.removeHubWaiter(waiter)

	select {
	case payload := <-waiter:
		return payload, nil
	case <-ctx.Done():
		return psignalr.OnConnectedPayload{}, ctx.Err()
	}
}

func (c *Client) WaitPartnerConnected(ctx context.Context, target string) error {
	if target == "" {
		return errors.New("relay: target is required")
	}
	waiter := make(chan struct{})
	c.mu.Lock()
	if c.partners[target] {
		c.mu.Unlock()
		return nil
	}
	c.partnerWaiters[target] = append(c.partnerWaiters[target], waiter)
	c.mu.Unlock()
	defer c.removePartnerWaiter(target, waiter)

	select {
	case <-waiter:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) Send(ctx context.Context, target, sessionID string, transportType dcg.TransportMessageType, payload []byte) error {
	if target == "" {
		return errors.New("relay: target is required")
	}
	sendGate := c.sendGateForTarget(target)
	select {
	case sendGate <- struct{}{}:
		defer func() { <-sendGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if sessionID == "" {
		var err error
		sessionID, err = c.sessionIDForTarget(target)
		if err != nil {
			return fmt.Errorf("relay: generate DCG session id: %w", err)
		}
	}
	if transportType != dcg.TransportMessageTypeApp && transportType != dcg.TransportMessageTypePlatform {
		return errors.New("relay: unsupported transport message type")
	}
	messageID := int(c.messageID.Add(1))
	fragments, err := dcg.FragmentPayload(payload, c.fragmentSize, messageID, sessionID, int(transportType))
	if err != nil {
		return err
	}
	seq := c.sequencer(target)
	for i := range fragments {
		fragments[i].SequenceNumber = seq.Next()
		if err := c.sendFragment(ctx, target, fragments[i]); err != nil {
			return fmt.Errorf("relay: fragment %d/%d: %w", i+1, len(fragments), err)
		}
	}
	return nil
}

func (c *Client) sendFragment(ctx context.Context, target string, f dcg.Fragment) error {
	key := pendingKey{source: target, sessionID: f.SessionID, sequence: f.SequenceNumber}
	ackCh := make(chan dcg.Ack, 1)
	c.mu.Lock()
	c.pending[key] = ackCh
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
	}()

	packet := dcg.ToMultiplexPacket(f, int(dcg.MessageTypeFragment))
	for attempt := 0; attempt <= c.ackRetries; attempt++ {
		trace, traceErr := psignalr.NewTraceContextPacket()
		if traceErr != nil {
			return traceErr
		}
		invocationID, completionCh, err := c.sendPacketWithCompletion(
			target,
			"",
			trace,
			packet,
		)
		if err != nil {
			return err
		}

		hubAccepted := false
		timer := time.NewTimer(c.ackTimeout)
		attemptDone := false
		for !attemptDone {
			select {
			case ack := <-ackCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				c.removeCompletionWaiter(invocationID)
				if !ack.Success {
					return fmt.Errorf("DCG acknowledgement failed with error %d", ack.ErrorNumber)
				}
				return nil

			case completion := <-completionCh:
				c.removeCompletionWaiter(invocationID)
				completionCh = nil
				if completion.Error != "" {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					return fmt.Errorf("Hub Relay rejected SendMessageAsync: %s", completion.Error)
				}
				hubAccepted = true

			case <-timer.C:
				c.removeCompletionWaiter(invocationID)
				attemptDone = true
				if attempt == c.ackRetries {
					if hubAccepted {
						return errors.New("Hub Relay accepted SendMessageAsync, but peer DCG acknowledgement timed out")
					}
					return errors.New("Hub Relay completion and peer DCG acknowledgement timed out")
				}

			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				c.removeCompletionWaiter(invocationID)
				return ctx.Err()
			}
		}
	}
	return errors.New("relay: unreachable retry state")
}

func (c *Client) handlePacket(ctx context.Context, msg psignalr.ReceiveMessage) error {
	mt, err := dcg.PacketMessageType(msg.Packet)
	if err != nil {
		return err
	}
	switch mt {
	case dcg.MessageTypeAcknowledgement:
		ack, err := dcg.ParseAckPacket(msg.Packet)
		if err != nil {
			return err
		}
		key := pendingKey{source: msg.SourceDcgClientID, sessionID: ack.SessionID, sequence: ack.SequenceNumber}
		c.mu.Lock()
		ch := c.pending[key]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- ack:
			default:
			}
		}
		return nil

	case dcg.MessageTypeFragment:
		f, err := dcg.ParseFragmentPacket(msg.Packet)
		if err != nil {
			return err
		}
		if err := c.sendPacket(msg.SourceDcgClientID, msg.ConnectionSessionID, msg.Trace, dcg.SuccessAckPacket(f)); err != nil {
			return err
		}
		payload, complete, err := c.reassembler.Add(msg.SourceDcgClientID, f)
		if err != nil || !complete {
			return err
		}
		event := Received{
			Source:               msg.SourceDcgClientID,
			SessionID:            f.SessionID,
			MessageID:            f.MessageID,
			TransportMessageType: dcg.TransportMessageType(f.TransportMessageType),
			Payload:              payload,
		}
		select {
		case c.received <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			return errors.New("relay: application receive queue is full")
		}
	default:
		return nil
	}
}

func (c *Client) sendPacketWithCompletion(
	target,
	connectionSessionID string,
	trace psignalr.TraceContextPacket,
	packet dcg.MultiplexPacket,
) (string, <-chan psignalr.Completion, error) {
	var err error
	trace, err = psignalr.NormalizeTraceContextPacket(trace)
	if err != nil {
		return "", nil, err
	}
	id := strconv.FormatUint(c.invocation.Add(1), 10)
	completionCh := make(chan psignalr.Completion, 1)
	c.mu.Lock()
	c.completions[id] = completionCh
	c.mu.Unlock()

	var frame []byte
	if connectionSessionID != "" {
		frame, err = psignalr.FrameSendSessionBasedMessageAsync(&id, trace, target, packet, connectionSessionID)
	} else {
		frame, err = psignalr.FrameSendMessageAsync(&id, trace, target, packet)
	}
	if err != nil {
		c.removeCompletionWaiter(id)
		return "", nil, err
	}
	if err := c.hub.SendBinary(frame); err != nil {
		c.removeCompletionWaiter(id)
		return "", nil, err
	}
	return id, completionCh, nil
}

func (c *Client) sendPacket(target, connectionSessionID string, trace psignalr.TraceContextPacket, packet dcg.MultiplexPacket) error {
	var err error
	trace, err = psignalr.NormalizeTraceContextPacket(trace)
	if err != nil {
		return err
	}
	id := strconv.FormatUint(c.invocation.Add(1), 10)
	var frame []byte
	if connectionSessionID != "" {
		frame, err = psignalr.FrameSendSessionBasedMessageAsync(&id, trace, target, packet, connectionSessionID)
	} else {
		frame, err = psignalr.FrameSendMessageAsync(&id, trace, target, packet)
	}
	if err != nil {
		return err
	}
	return c.hub.SendBinary(frame)
}

func (c *Client) deliverCompletion(completion psignalr.Completion) {
	c.mu.Lock()
	waiter := c.completions[completion.InvocationID]
	c.mu.Unlock()
	if waiter == nil {
		return
	}
	select {
	case waiter <- completion:
	default:
	}
}

func (c *Client) removeCompletionWaiter(invocationID string) {
	if invocationID == "" {
		return
	}
	c.mu.Lock()
	delete(c.completions, invocationID)
	c.mu.Unlock()
}

func (c *Client) markHubConnected(payload psignalr.OnConnectedPayload) {
	payload = cloneOnConnectedPayload(payload)
	c.mu.Lock()
	c.hubConnected = &payload
	waiters := append([]chan psignalr.OnConnectedPayload(nil), c.hubWaiters...)
	c.hubWaiters = nil
	c.mu.Unlock()
	for _, waiter := range waiters {
		select {
		case waiter <- cloneOnConnectedPayload(payload):
		default:
		}
	}
}

func (c *Client) removeHubWaiter(waiter chan psignalr.OnConnectedPayload) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, candidate := range c.hubWaiters {
		if candidate != waiter {
			continue
		}
		c.hubWaiters = append(c.hubWaiters[:i], c.hubWaiters[i+1:]...)
		return
	}
}

func cloneOnConnectedPayload(payload psignalr.OnConnectedPayload) psignalr.OnConnectedPayload {
	payload.Partners = append([]string(nil), payload.Partners...)
	return payload
}

func (c *Client) markPartnerConnected(target string) {
	if target == "" {
		return
	}
	c.mu.Lock()
	c.partners[target] = true
	waiters := c.partnerWaiters[target]
	delete(c.partnerWaiters, target)
	c.mu.Unlock()
	for _, waiter := range waiters {
		close(waiter)
	}
}

func (c *Client) markPartnerDisconnected(target string) {
	if target == "" {
		return
	}
	c.mu.Lock()
	c.partners[target] = false
	c.partnerDisconnects[target]++
	c.mu.Unlock()
}

func (c *Client) removePartnerWaiter(target string, waiter chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	waiters := c.partnerWaiters[target]
	for i, candidate := range waiters {
		if candidate != waiter {
			continue
		}
		waiters = append(waiters[:i], waiters[i+1:]...)
		if len(waiters) == 0 {
			delete(c.partnerWaiters, target)
		} else {
			c.partnerWaiters[target] = waiters
		}
		return
	}
}

func (c *Client) sendGateForTarget(target string) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	gate := c.sendGates[target]
	if gate == nil {
		gate = make(chan struct{}, 1)
		c.sendGates[target] = gate
	}
	return gate
}

func (c *Client) sequencer(target string) *dcg.Sequencer {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sequencers[target]
	if s == nil {
		s = dcg.NewSequencer()
		c.sequencers[target] = s
	}
	return s
}

func (c *Client) sessionIDForTarget(target string) (string, error) {
	if target == "" {
		return "", errors.New("relay: target is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if sessionID := c.sessions[target]; sessionID != "" {
		return sessionID, nil
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	sessionID := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	c.sessions[target] = sessionID
	return sessionID, nil
}
