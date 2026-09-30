package relay

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	psignalr "github.com/YMGPwcca/phonelink-linux/protocol/signalr"
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
	sequence int
}

type Client struct {
	hub Hub

	fragmentSize int
	ackTimeout   time.Duration
	ackRetries   int

	reassembler *dcg.Reassembler
	messageID   atomic.Int64
	invocation  atomic.Uint64

	mu         sync.Mutex
	sequencers map[string]*dcg.Sequencer
	pending    map[pendingKey]chan dcg.Ack

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
		hub:          hub,
		fragmentSize: cfg.FragmentSize,
		ackTimeout:   cfg.AckTimeout,
		ackRetries:   cfg.AckRetries,
		reassembler:  dcg.NewReassembler(32<<20, 4096),
		sequencers:   make(map[string]*dcg.Sequencer),
		pending:      make(map[pendingKey]chan dcg.Ack),
		received:     make(chan Received, 32),
	}
}

func (c *Client) Received() <-chan Received { return c.received }
func (c *Client) Close() error             { return c.hub.Close() }

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
				if inv.Target != "OnReceiveMessage" {
					continue
				}
				msg, err := psignalr.ParseOnReceiveMessage(inv)
				if err != nil {
					return err
				}
				if err := c.handlePacket(ctx, msg); err != nil {
					return err
				}
			case psignalr.HubMessageTypeCompletion, psignalr.HubMessageTypePing:
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

func (c *Client) Send(ctx context.Context, target, sessionID string, transportType dcg.TransportMessageType, payload []byte) error {
	if target == "" || sessionID == "" {
		return errors.New("relay: target and session id are required")
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
		if err := c.sendPacket(target, psignalr.TraceContextPacket{}, packet); err != nil {
			return err
		}
		timer := time.NewTimer(c.ackTimeout)
		select {
		case ack := <-ackCh:
			if !timer.Stop() {
				<-timer.C
			}
			if !ack.Success {
				return fmt.Errorf("DCG acknowledgement failed with error %d", ack.ErrorNumber)
			}
			return nil
		case <-timer.C:
			if attempt == c.ackRetries {
				return errors.New("DCG acknowledgement timeout")
			}
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
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
		if err := c.sendPacket(msg.SourceDcgClientID, msg.Trace, dcg.SuccessAckPacket(f)); err != nil {
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
		}
	default:
		return nil
	}
}

func (c *Client) sendPacket(target string, trace psignalr.TraceContextPacket, packet dcg.MultiplexPacket) error {
	id := strconv.FormatUint(c.invocation.Add(1), 10)
	frame, err := psignalr.FrameSendMessageAsync(&id, trace, target, packet)
	if err != nil {
		return err
	}
	return c.hub.SendBinary(frame)
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
