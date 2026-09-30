package signalr

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
)

var ErrMessagePack = errors.New("signalr: malformed MessagePack")

type Invocation struct {
	MessageType  int64
	Headers      map[string]any
	InvocationID *string
	Target       string
	Arguments    []any
	StreamIDs    []string
}

type ReceiveMessage struct {
	SourceDcgClientID string
	Trace             TraceContextPacket
	Packet            dcg.MultiplexPacket
}

func SplitFrames(payload []byte) ([][]byte, error) {
	var frames [][]byte
	for len(payload) > 0 {
		n, used, err := DecodeLength(payload)
		if err != nil {
			return nil, err
		}
		if n < 0 || used+n > len(payload) {
			return nil, ErrLength
		}
		frames = append(frames, append([]byte(nil), payload[used:used+n]...))
		payload = payload[used+n:]
	}
	return frames, nil
}

func DecodeValue(b []byte) (any, error) {
	i := 0
	v, err := decodeValue(b, &i, 0)
	if err != nil {
		return nil, err
	}
	if i != len(b) {
		return nil, fmt.Errorf("%w: trailing bytes", ErrMessagePack)
	}
	return v, nil
}

func ParseInvocation(body []byte) (Invocation, error) {
	var out Invocation
	v, err := DecodeValue(body)
	if err != nil {
		return out, err
	}
	a, ok := v.([]any)
	if !ok || len(a) != 6 {
		return out, fmt.Errorf("%w: invocation is not a six-element array", ErrMessagePack)
	}
	mt, ok := asInt64(a[0])
	if !ok {
		return out, ErrMessagePack
	}
	out.MessageType = mt
	headers, ok := a[1].(map[string]any)
	if !ok {
		return out, ErrMessagePack
	}
	out.Headers = headers
	switch x := a[2].(type) {
	case nil:
	case string:
		out.InvocationID = &x
	default:
		return out, ErrMessagePack
	}
	target, ok := a[3].(string)
	if !ok {
		return out, ErrMessagePack
	}
	out.Target = target
	args, ok := a[4].([]any)
	if !ok {
		return out, ErrMessagePack
	}
	out.Arguments = args
	ids, ok := a[5].([]any)
	if !ok {
		return out, ErrMessagePack
	}
	for _, id := range ids {
		s, ok := id.(string)
		if !ok {
			return out, ErrMessagePack
		}
		out.StreamIDs = append(out.StreamIDs, s)
	}
	return out, nil
}

func ParseOnReceiveMessage(inv Invocation) (ReceiveMessage, error) {
	var out ReceiveMessage
	if inv.MessageType != HubMessageTypeInvocation ||
		inv.Target != "OnReceiveMessage" ||
		len(inv.Arguments) != 3 {
		return out, errors.New("signalr: not an OnReceiveMessage invocation")
	}
	source, ok := inv.Arguments[0].(string)
	if !ok || source == "" {
		return out, ErrMessagePack
	}
	out.SourceDcgClientID = source
	traceMap, ok := inv.Arguments[1].(map[string]any)
	if !ok {
		return out, ErrMessagePack
	}
	trace, err := traceFromMap(traceMap)
	if err != nil {
		return out, err
	}
	out.Trace = trace
	packetMap, ok := inv.Arguments[2].(map[string]any)
	if !ok {
		return out, ErrMessagePack
	}
	packet, err := packetFromMap(packetMap)
	if err != nil {
		return out, err
	}
	out.Packet = packet
	return out, nil
}

func traceFromMap(m map[string]any) (TraceContextPacket, error) {
	var t TraceContextPacket
	if v, ok := m["ParentId"]; ok {
		if v != nil {
			s, ok := v.(string)
			if !ok {
				return t, ErrMessagePack
			}
			t.ParentID = &s
		}
	}
	if v, ok := m["TraceFlags"]; ok {
		n, ok := asInt64(v)
		if !ok || n < 0 || n > 255 {
			return t, ErrMessagePack
		}
		t.TraceFlags = uint8(n)
	}
	if v, ok := m["TraceId"]; ok {
		if v != nil {
			s, ok := v.(string)
			if !ok {
				return t, ErrMessagePack
			}
			t.TraceID = &s
		}
	}
	if v, ok := m["TraceState"]; ok && v != nil {
		mm, ok := v.(map[string]any)
		if !ok {
			return t, ErrMessagePack
		}
		t.TraceState = make(map[string]string, len(mm))
		for k, v := range mm {
			s, ok := v.(string)
			if !ok {
				return t, ErrMessagePack
			}
			t.TraceState[k] = s
		}
	}
	return t, nil
}

func packetFromMap(m map[string]any) (dcg.MultiplexPacket, error) {
	var p dcg.MultiplexPacket
	t, ok := m["Type"].(string)
	if !ok {
		return p, ErrMessagePack
	}
	p.Type = t
	raw, ok := m["Raw"].([]byte)
	if !ok {
		return p, ErrMessagePack
	}
	p.Raw = append([]byte(nil), raw...)
	props, ok := m["Properties"].(map[string]any)
	if !ok {
		return p, ErrMessagePack
	}
	p.Properties = props
	return p, nil
}

func asInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case uint64:
		if x <= math.MaxInt64 {
			return int64(x), true
		}
	case float64:
		if x == math.Trunc(x) && x >= math.MinInt64 && x <= math.MaxInt64 {
			return int64(x), true
		}
	}
	return 0, false
}

func decodeValue(b []byte, i *int, depth int) (any, error) {
	if depth > 64 || *i >= len(b) {
		return nil, ErrMessagePack
	}
	c := b[*i]
	*i++
	switch {
	case c <= 0x7f:
		return int64(c), nil
	case c >= 0xe0:
		return int64(int8(c)), nil
	case c >= 0xa0 && c <= 0xbf:
		return readString(b, i, int(c&0x1f))
	case c >= 0x90 && c <= 0x9f:
		return readArray(b, i, int(c&0x0f), depth)
	case c >= 0x80 && c <= 0x8f:
		return readMap(b, i, int(c&0x0f), depth)
	}
	switch c {
	case 0xc0:
		return nil, nil
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xc4:
		n, err := readUint(b, i, 1)
		if err != nil {
			return nil, err
		}
		return readBinary(b, i, n)
	case 0xc5:
		n, err := readUint(b, i, 2)
		if err != nil {
			return nil, err
		}
		return readBinary(b, i, n)
	case 0xc6:
		n, err := readUint(b, i, 4)
		if err != nil {
			return nil, err
		}
		return readBinary(b, i, n)
	case 0xca:
		u, err := readUint(b, i, 4)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(uint32(u))), nil
	case 0xcb:
		u, err := readUint(b, i, 8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(u), nil
	case 0xcc:
		u, err := readUint(b, i, 1)
		return intOrErr(u, err)
	case 0xcd:
		u, err := readUint(b, i, 2)
		return intOrErr(u, err)
	case 0xce:
		u, err := readUint(b, i, 4)
		return intOrErr(u, err)
	case 0xcf:
		u, err := readUint(b, i, 8)
		if err != nil {
			return nil, err
		}
		if u <= math.MaxInt64 {
			return int64(u), nil
		}
		return u, nil
	case 0xd0:
		u, err := readUint(b, i, 1)
		if err != nil {
			return nil, err
		}
		return int64(int8(u)), nil
	case 0xd1:
		u, err := readUint(b, i, 2)
		if err != nil {
			return nil, err
		}
		return int64(int16(u)), nil
	case 0xd2:
		u, err := readUint(b, i, 4)
		if err != nil {
			return nil, err
		}
		return int64(int32(u)), nil
	case 0xd3:
		u, err := readUint(b, i, 8)
		if err != nil {
			return nil, err
		}
		return int64(u), nil
	case 0xd9:
		n, err := readUint(b, i, 1)
		if err != nil {
			return nil, err
		}
		return readString(b, i, int(n))
	case 0xda:
		n, err := readUint(b, i, 2)
		if err != nil {
			return nil, err
		}
		return readString(b, i, int(n))
	case 0xdb:
		n, err := readUint(b, i, 4)
		if err != nil || n > math.MaxInt32 {
			return nil, ErrMessagePack
		}
		return readString(b, i, int(n))
	case 0xdc:
		n, err := readUint(b, i, 2)
		if err != nil {
			return nil, err
		}
		return readArray(b, i, int(n), depth)
	case 0xdd:
		n, err := readUint(b, i, 4)
		if err != nil || n > 1<<20 {
			return nil, ErrMessagePack
		}
		return readArray(b, i, int(n), depth)
	case 0xde:
		n, err := readUint(b, i, 2)
		if err != nil {
			return nil, err
		}
		return readMap(b, i, int(n), depth)
	case 0xdf:
		n, err := readUint(b, i, 4)
		if err != nil || n > 1<<20 {
			return nil, ErrMessagePack
		}
		return readMap(b, i, int(n), depth)
	default:
		return nil, fmt.Errorf("%w: unsupported code 0x%02x", ErrMessagePack, c)
	}
}

func intOrErr(u uint64, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return int64(u), nil
}

func readUint(b []byte, i *int, n int) (uint64, error) {
	if n < 0 || *i+n > len(b) {
		return 0, ErrMessagePack
	}
	var v uint64
	switch n {
	case 1:
		v = uint64(b[*i])
	case 2:
		v = uint64(binary.BigEndian.Uint16(b[*i : *i+2]))
	case 4:
		v = uint64(binary.BigEndian.Uint32(b[*i : *i+4]))
	case 8:
		v = binary.BigEndian.Uint64(b[*i : *i+8])
	default:
		return 0, ErrMessagePack
	}
	*i += n
	return v, nil
}

func readString(b []byte, i *int, n int) (any, error) {
	if n < 0 || *i+n > len(b) {
		return nil, ErrMessagePack
	}
	s := string(b[*i : *i+n])
	*i += n
	return s, nil
}

func readBinary(b []byte, i *int, n uint64) (any, error) {
	if n > uint64(len(b)-*i) || n > 1<<28 {
		return nil, ErrMessagePack
	}
	v := append([]byte(nil), b[*i:*i+int(n)]...)
	*i += int(n)
	return v, nil
}

func readArray(b []byte, i *int, n, depth int) (any, error) {
	if n < 0 || n > 1<<20 {
		return nil, ErrMessagePack
	}
	a := make([]any, 0, n)
	for j := 0; j < n; j++ {
		v, err := decodeValue(b, i, depth+1)
		if err != nil {
			return nil, err
		}
		a = append(a, v)
	}
	return a, nil
}

func readMap(b []byte, i *int, n, depth int) (any, error) {
	if n < 0 || n > 1<<20 {
		return nil, ErrMessagePack
	}
	m := make(map[string]any, n)
	for j := 0; j < n; j++ {
		k, err := decodeValue(b, i, depth+1)
		if err != nil {
			return nil, err
		}
		ks, ok := k.(string)
		if !ok {
			return nil, ErrMessagePack
		}
		v, err := decodeValue(b, i, depth+1)
		if err != nil {
			return nil, err
		}
		m[ks] = v
	}
	return m, nil
}
