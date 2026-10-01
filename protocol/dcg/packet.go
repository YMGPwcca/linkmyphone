package dcg

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sync"
)

const (
	PropertySuccess      = "Success"
	PropertyErrorNumber  = "ErrorNo"
	PropertyErrorMessage = "ErrorMessage"
)

type Ack struct {
	SequenceNumber int
	Success        bool
	ErrorNumber    int
	SessionID      string
}

func ParseFragmentPacket(p MultiplexPacket) (Fragment, error) {
	var f Fragment
	if p.Type != HandlerType {
		return f, errors.New("dcg: wrong handler type")
	}
	if v, ok := propertyInt(p.Properties, PropertyType); !ok || v != int(MessageTypeFragment) {
		return f, errors.New("dcg: packet is not a fragment")
	}
	version, ok := propertyFloat(p.Properties, PropertyVersion)
	if !ok || version != 1 {
		return f, errors.New("dcg: unsupported version")
	}
	if f.SequenceNumber, ok = propertyInt(p.Properties, PropertySequenceNumber); !ok {
		return f, errors.New("dcg: missing sequence number")
	}
	if f.MessageID, ok = propertyInt(p.Properties, PropertyMessageID); !ok {
		return f, errors.New("dcg: missing message id")
	}
	if f.FragmentNumber, ok = propertyInt(p.Properties, PropertyFragmentID); !ok {
		return f, errors.New("dcg: missing fragment id")
	}
	if f.FragmentCount, ok = propertyInt(p.Properties, PropertyFragmentCount); !ok {
		return f, errors.New("dcg: missing fragment count")
	}
	if f.FragmentNumber < 1 || f.FragmentCount < 1 || f.FragmentNumber > f.FragmentCount {
		return f, errors.New("dcg: invalid fragment bounds")
	}
	if mt, exists := p.Properties[PropertyMessageType]; exists {
		v, ok := numberInt(mt)
		if !ok {
			return f, errors.New("dcg: invalid message type")
		}
		f.TransportMessageType = v
	} else {
		f.TransportMessageType = int(TransportMessageTypeApp)
	}
	sid, ok := p.Properties[PropertySessionID].(string)
	if !ok || sid == "" {
		return f, errors.New("dcg: missing session id")
	}
	f.SessionID = sid
	f.Payload = append([]byte(nil), p.Raw...)
	return f, nil
}

func ParseAckPacket(p MultiplexPacket) (Ack, error) {
	var a Ack
	if p.Type != HandlerType {
		return a, errors.New("dcg: wrong handler type")
	}
	if v, ok := propertyInt(p.Properties, PropertyType); !ok || v != int(MessageTypeAcknowledgement) {
		return a, errors.New("dcg: packet is not an acknowledgement")
	}
	if v, ok := propertyFloat(p.Properties, PropertyVersion); !ok || v != 1 {
		return a, errors.New("dcg: unsupported version")
	}
	var ok bool
	if a.SequenceNumber, ok = propertyInt(p.Properties, PropertySequenceNumber); !ok {
		return a, errors.New("dcg: missing ack sequence number")
	}
	s, ok := p.Properties[PropertySuccess].(bool)
	if !ok {
		return a, errors.New("dcg: missing ack success")
	}
	a.Success = s
	if e, exists := p.Properties[PropertyErrorNumber]; exists {
		v, ok := numberInt(e)
		if !ok {
			return a, errors.New("dcg: invalid ack error number")
		}
		a.ErrorNumber = v
	}
	sid, ok := p.Properties[PropertySessionID].(string)
	if !ok || sid == "" {
		return a, errors.New("dcg: missing session id")
	}
	a.SessionID = sid
	return a, nil
}

func SuccessAckPacket(f Fragment) MultiplexPacket {
	return MultiplexPacket{Type: HandlerType, Properties: map[string]any{
		PropertyVersion: float64(1), PropertyType: int(MessageTypeAcknowledgement), PropertySessionID: f.SessionID,
		PropertySequenceNumber: float64(f.SequenceNumber), PropertySuccess: true, PropertyErrorNumber: float64(0),
	}, Raw: nil}
}

func PacketMessageType(p MultiplexPacket) (MessageType, error) {
	v, ok := propertyInt(p.Properties, PropertyType)
	if !ok {
		return MessageTypeUnspecified, errors.New("dcg: missing packet type")
	}
	mt := MessageType(v)
	switch mt {
	case MessageTypeAcknowledgement, MessageTypeFragment, MessageTypePresenceAnnouncement, MessageTypePresenceRequest, MessageTypePresenceResponse:
		return mt, nil
	default:
		return mt, fmt.Errorf("dcg: unsupported packet type %d", v)
	}
}

func propertyInt(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	return numberInt(v)
}

func propertyFloat(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	}
	return 0, false
}

func numberInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		if int64(int(x)) == x {
			return int(x), true
		}
	case uint64:
		if x <= uint64(math.MaxInt) {
			return int(x), true
		}
	case float64:
		if x == math.Trunc(x) && x >= float64(math.MinInt) && x <= float64(math.MaxInt) {
			return int(x), true
		}
	}
	return 0, false
}

type MessageKey struct {
	Source               string
	SessionID            string
	MessageID            int
	TransportMessageType int
}

type partialMessage struct {
	count int
	parts map[int][]byte
	size  int
}

type Reassembler struct {
	mu              sync.Mutex
	messages        map[MessageKey]*partialMessage
	maxMessageBytes int
	maxFragments    int
}

func NewReassembler(maxMessageBytes, maxFragments int) *Reassembler {
	if maxMessageBytes <= 0 {
		maxMessageBytes = 16 << 20
	}
	if maxFragments <= 0 {
		maxFragments = 4096
	}
	return &Reassembler{
		messages:        make(map[MessageKey]*partialMessage),
		maxMessageBytes: maxMessageBytes,
		maxFragments:    maxFragments,
	}
}

func (r *Reassembler) Add(source string, f Fragment) ([]byte, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if source == "" {
		return nil, false, errors.New("dcg: empty source")
	}
	if f.FragmentCount < 1 || f.FragmentCount > r.maxFragments || f.FragmentNumber < 1 || f.FragmentNumber > f.FragmentCount {
		return nil, false, errors.New("dcg: invalid fragment bounds")
	}
	k := MessageKey{Source: source, SessionID: f.SessionID, MessageID: f.MessageID, TransportMessageType: f.TransportMessageType}
	pm := r.messages[k]
	if pm == nil {
		pm = &partialMessage{count: f.FragmentCount, parts: make(map[int][]byte, f.FragmentCount)}
		r.messages[k] = pm
	} else if pm.count != f.FragmentCount {
		delete(r.messages, k)
		return nil, false, errors.New("dcg: fragment count changed")
	}
	if old, ok := pm.parts[f.FragmentNumber]; ok {
		if !bytes.Equal(old, f.Payload) {
			delete(r.messages, k)
			return nil, false, errors.New("dcg: duplicate fragment payload mismatch")
		}
		return nil, false, nil
	}
	if pm.size+len(f.Payload) > r.maxMessageBytes {
		delete(r.messages, k)
		return nil, false, errors.New("dcg: reassembled message too large")
	}
	pm.parts[f.FragmentNumber] = append([]byte(nil), f.Payload...)
	pm.size += len(f.Payload)
	if len(pm.parts) != pm.count {
		return nil, false, nil
	}
	out := make([]byte, 0, pm.size)
	for i := 1; i <= pm.count; i++ {
		part, ok := pm.parts[i]
		if !ok {
			return nil, false, nil
		}
		out = append(out, part...)
	}
	delete(r.messages, k)
	return out, true, nil
}

func (r *Reassembler) Reset() {
	r.mu.Lock()
	r.messages = make(map[MessageKey]*partialMessage)
	r.mu.Unlock()
}
