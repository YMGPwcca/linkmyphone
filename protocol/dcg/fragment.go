// Package dcg models the DCG fragment envelope carried as a Hub Relay
// MultiplexPacket.
package dcg

import (
	"errors"
	"sync/atomic"
)

const HandlerType = "ms-dcg"

const (
	PropertyVersion        = "Version"
	PropertyType           = "Type"
	PropertySessionID      = "SessionId"
	PropertySequenceNumber = "SequenceNumber"
	PropertyMessageID      = "MessageId"
	PropertyFragmentID     = "FragmentId"
	PropertyFragmentCount  = "FragmentCount"
	PropertyMessageType    = "MessageType"
)

type Fragment struct {
	SequenceNumber       int
	FragmentNumber       int
	FragmentCount        int
	MessageID            int
	Payload              []byte
	TransportMessageType int
	SessionID            string
}

type MultiplexPacket struct {
	Properties map[string]any
	Raw        []byte
	Type       string
}

func FragmentPayload(payload []byte, fragmentSize int, messageID int, sessionID string, transportMessageType int) ([]Fragment, error) {
	if fragmentSize <= 0 {
		return nil, errors.New("dcg: fragment size must be positive")
	}
	count := (len(payload) + fragmentSize - 1) / fragmentSize
	if count == 0 {
		count = 1
	}
	out := make([]Fragment, 0, count)
	for i := 0; i < count; i++ {
		start := i * fragmentSize
		end := start + fragmentSize
		if end > len(payload) {
			end = len(payload)
		}
		part := append([]byte(nil), payload[start:end]...)
		out = append(out, Fragment{
			FragmentNumber:       i + 1,
			FragmentCount:        count,
			MessageID:            messageID,
			Payload:              part,
			TransportMessageType: transportMessageType,
			SessionID:            sessionID,
		})
	}
	return out, nil
}

func ToMultiplexPacket(f Fragment, dcgMessageType int) MultiplexPacket {
	return MultiplexPacket{
		Type: HandlerType,
		Properties: map[string]any{
			PropertyVersion:        float64(1),
			PropertyType:           dcgMessageType,
			PropertySessionID:      f.SessionID,
			PropertySequenceNumber: f.SequenceNumber,
			PropertyMessageID:      f.MessageID,
			PropertyFragmentID:     f.FragmentNumber,
			PropertyFragmentCount:  f.FragmentCount,
			PropertyMessageType:    f.TransportMessageType,
		},
		Raw: append([]byte(nil), f.Payload...),
	}
}

type Sequencer struct {
	value atomic.Int64
}

func NewSequencer() *Sequencer {
	s := &Sequencer{}
	s.value.Store(0)
	return s
}

// Next returns the next per-peer send sequence number. Call once immediately
// before an initial fragment send; retain the value on the Fragment for retries.
func (s *Sequencer) Next() int {
	return int(s.value.Add(1))
}
