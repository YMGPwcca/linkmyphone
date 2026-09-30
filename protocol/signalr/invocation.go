package signalr

import (
	"encoding/binary"
	"errors"
	"math"
	"sort"

	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
)

const (
	HubMessageTypeInvocation = 1
	TargetSendMessageAsync    = "SendMessageAsync"
)

type TraceContextPacket struct {
	ParentID   *string
	TraceFlags uint8
	TraceID    *string
	TraceState map[string]string
}

// MarshalSendMessageAsync serializes the exact six-element SignalR MessagePack
// Invocation body used by HubRelayProxy.SendMessageAsync:
// [1, {}, invocationId, "SendMessageAsync", [trace, target, packet], []].
func MarshalSendMessageAsync(invocationID *string, trace TraceContextPacket, targetDcgClientID string, packet dcg.MultiplexPacket) ([]byte, error) {
	if targetDcgClientID == "" {
		return nil, errors.New("signalr: empty target DCG client id")
	}
	p := packer{}
	p.array(6)
	p.integer(HubMessageTypeInvocation)
	p.mapLen(0)
	p.nullableString(invocationID)
	p.str(TargetSendMessageAsync)
	p.array(3)
	p.trace(trace)
	p.str(targetDcgClientID)
	if err := p.multiplexPacket(packet); err != nil {
		return nil, err
	}
	p.array(0)
	return p.b, nil
}

func FrameSendMessageAsync(invocationID *string, trace TraceContextPacket, targetDcgClientID string, packet dcg.MultiplexPacket) ([]byte, error) {
	body, err := MarshalSendMessageAsync(invocationID, trace, targetDcgClientID, packet)
	if err != nil {
		return nil, err
	}
	return Frame(body)
}

type packer struct{ b []byte }

func (p *packer) array(n int) {
	if n < 16 {
		p.b = append(p.b, 0x90|byte(n))
		return
	}
	if n <= math.MaxUint16 {
		p.b = append(p.b, 0xdc, byte(n>>8), byte(n))
		return
	}
	p.b = append(p.b, 0xdd, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func (p *packer) mapLen(n int) {
	if n < 16 {
		p.b = append(p.b, 0x80|byte(n))
		return
	}
	if n <= math.MaxUint16 {
		p.b = append(p.b, 0xde, byte(n>>8), byte(n))
		return
	}
	p.b = append(p.b, 0xdf, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func (p *packer) str(s string) {
	n := len(s)
	switch {
	case n < 32:
		p.b = append(p.b, 0xa0|byte(n))
	case n <= math.MaxUint8:
		p.b = append(p.b, 0xd9, byte(n))
	case n <= math.MaxUint16:
		p.b = append(p.b, 0xda, byte(n>>8), byte(n))
	default:
		p.b = append(p.b, 0xdb, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	p.b = append(p.b, s...)
}

func (p *packer) nullableString(s *string) {
	if s == nil {
		p.b = append(p.b, 0xc0)
		return
	}
	p.str(*s)
}

func (p *packer) bin(v []byte) {
	n := len(v)
	switch {
	case n <= math.MaxUint8:
		p.b = append(p.b, 0xc4, byte(n))
	case n <= math.MaxUint16:
		p.b = append(p.b, 0xc5, byte(n>>8), byte(n))
	default:
		p.b = append(p.b, 0xc6, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	p.b = append(p.b, v...)
}

func (p *packer) integer(v int64) {
	switch {
	case v >= 0 && v <= 0x7f:
		p.b = append(p.b, byte(v))
	case v >= -32 && v < 0:
		p.b = append(p.b, byte(int8(v)))
	case v >= math.MinInt8 && v <= math.MaxInt8:
		p.b = append(p.b, 0xd0, byte(int8(v)))
	case v >= math.MinInt16 && v <= math.MaxInt16:
		p.b = append(p.b, 0xd1, byte(uint16(v)>>8), byte(uint16(v)))
	case v >= math.MinInt32 && v <= math.MaxInt32:
		u := uint32(v)
		p.b = append(p.b, 0xd2, byte(u>>24), byte(u>>16), byte(u>>8), byte(u))
	default:
		u := uint64(v)
		p.b = append(p.b, 0xd3, byte(u>>56), byte(u>>48), byte(u>>40), byte(u>>32), byte(u>>24), byte(u>>16), byte(u>>8), byte(u))
	}
}

func (p *packer) float64(v float64) {
	p.b = append(p.b, 0xcb)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], math.Float64bits(v))
	p.b = append(p.b, buf[:]...)
}

func (p *packer) trace(v TraceContextPacket) {
	p.mapLen(4)
	p.str("ParentId")
	p.nullableString(v.ParentID)
	p.str("TraceFlags")
	p.integer(int64(v.TraceFlags))
	p.str("TraceId")
	p.nullableString(v.TraceID)
	p.str("TraceState")
	if v.TraceState == nil {
		p.b = append(p.b, 0xc0)
	} else {
		keys := make([]string, 0, len(v.TraceState))
		for k := range v.TraceState {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		p.mapLen(len(keys))
		for _, k := range keys {
			p.str(k)
			p.str(v.TraceState[k])
		}
	}
}

func (p *packer) multiplexPacket(v dcg.MultiplexPacket) error {
	p.mapLen(3)
	p.str("Properties")
	keys := make([]string, 0, len(v.Properties))
	for k := range v.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	p.mapLen(len(keys))
	for _, k := range keys {
		p.str(k)
		if err := p.any(v.Properties[k]); err != nil {
			return err
		}
	}
	p.str("Raw")
	if v.Raw == nil {
		p.b = append(p.b, 0xc0)
	} else {
		p.bin(v.Raw)
	}
	p.str("Type")
	p.str(v.Type)
	return nil
}

func (p *packer) any(v any) error {
	switch x := v.(type) {
	case string:
		p.str(x)
	case int:
		p.integer(int64(x))
	case int32:
		p.integer(int64(x))
	case int64:
		p.integer(x)
	case uint8:
		p.integer(int64(x))
	case float64:
		p.float64(x)
	case bool:
		if x {
			p.b = append(p.b, 0xc3)
		} else {
			p.b = append(p.b, 0xc2)
		}
	case []byte:
		p.bin(x)
	case nil:
		p.b = append(p.b, 0xc0)
	default:
		return errors.New("signalr: unsupported MessagePack value")
	}
	return nil
}
