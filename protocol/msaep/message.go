// Package msaep implements the protobuf envelope used by the Platform SDK
// Ambient Experience PubSub layer.
//
// Source-confirmed schema:
//   1: message_tag (int32)
//   2: dcg_client_id (string)
//   3: message_id (string)
//   4: payload (bytes)
//   5: platform_major_version (int32)
//   6: platform_minor_version (int32)
package msaep

import "errors"

var ErrMalformed = errors.New("msaep: malformed protobuf")

const (
	PlatformMajorVersion = int32(1)
	PlatformMinorVersion = int32(1)
)

type Message struct {
	MessageTag           int32
	DcgClientID          string
	MessageID            string
	Payload              []byte
	PlatformMajorVersion int32
	PlatformMinorVersion int32
}

func New(messageID, dcgClientID string, messageTag int32, payload []byte) Message {
	return Message{
		MessageTag:           messageTag,
		DcgClientID:          dcgClientID,
		MessageID:            messageID,
		Payload:              append([]byte(nil), payload...),
		PlatformMajorVersion: PlatformMajorVersion,
		PlatformMinorVersion: PlatformMinorVersion,
	}
}

func Marshal(m Message) []byte {
	var out []byte
	out = appendInt32(out, 1, m.MessageTag)
	out = appendString(out, 2, m.DcgClientID)
	out = appendString(out, 3, m.MessageID)
	out = appendBytes(out, 4, m.Payload)
	out = appendInt32(out, 5, m.PlatformMajorVersion)
	out = appendInt32(out, 6, m.PlatformMinorVersion)
	return out
}

func Unmarshal(b []byte) (Message, error) {
	var m Message
	for i := 0; i < len(b); {
		key, err := readVarint(b, &i)
		if err != nil {
			return m, err
		}
		field, wire := key>>3, key&7
		switch field {
		case 1:
			if wire != 0 {
				return m, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return m, err
			}
			m.MessageTag = int32(v)
		case 2:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			m.DcgClientID = string(v)
		case 3:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			m.MessageID = string(v)
		case 4:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			m.Payload = append([]byte(nil), v...)
		case 5:
			if wire != 0 {
				return m, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return m, err
			}
			m.PlatformMajorVersion = int32(v)
		case 6:
			if wire != 0 {
				return m, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return m, err
			}
			m.PlatformMinorVersion = int32(v)
		default:
			if err := skipField(b, &i, wire); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

func appendInt32(dst []byte, field uint64, v int32) []byte {
	if v == 0 {
		return dst
	}
	dst = appendVarint(dst, field<<3)
	return appendVarint(dst, uint64(uint32(v)))
}

func appendString(dst []byte, field uint64, s string) []byte {
	if s == "" {
		return dst
	}
	return appendBytes(dst, field, []byte(s))
}

func appendBytes(dst []byte, field uint64, b []byte) []byte {
	if len(b) == 0 {
		return dst
	}
	dst = appendVarint(dst, field<<3|2)
	dst = appendVarint(dst, uint64(len(b)))
	return append(dst, b...)
}

func appendVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func readVarint(b []byte, i *int) (uint64, error) {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if *i >= len(b) {
			return 0, ErrMalformed
		}
		c := b[*i]
		*i++
		v |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return v, nil
		}
	}
	return 0, ErrMalformed
}

func readBytes(b []byte, i *int) ([]byte, error) {
	n, err := readVarint(b, i)
	if err != nil || n > uint64(len(b)-*i) {
		return nil, ErrMalformed
	}
	out := b[*i : *i+int(n)]
	*i += int(n)
	return out, nil
}

func skipField(b []byte, i *int, wire uint64) error {
	switch wire {
	case 0:
		_, err := readVarint(b, i)
		return err
	case 1:
		if len(b)-*i < 8 {
			return ErrMalformed
		}
		*i += 8
		return nil
	case 2:
		_, err := readBytes(b, i)
		return err
	case 5:
		if len(b)-*i < 4 {
			return ErrMalformed
		}
		*i += 4
		return nil
	default:
		return ErrMalformed
	}
}
