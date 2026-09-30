// Package signalr implements the SignalR MessagePack Hub Protocol framing
// pieces confirmed in the Link to Windows APK.
package signalr

import "errors"

const MaxMessageLength = int(^uint32(0) >> 1)

var ErrLength = errors.New("signalr: invalid message length prefix")

func EncodeLength(n int) ([]byte, error) {
	if n < 0 || n > MaxMessageLength {
		return nil, ErrLength
	}
	v := uint32(n)
	out := make([]byte, 0, 5)
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out, nil
		}
	}
}

// DecodeLength returns the decoded length and number of prefix bytes consumed.
func DecodeLength(b []byte) (int, int, error) {
	var value uint32
	for i := 0; i < 5; i++ {
		if i >= len(b) {
			return 0, 0, ErrLength
		}
		c := b[i]
		if i == 4 && c > 7 {
			return 0, 0, ErrLength
		}
		value |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			if value > uint32(MaxMessageLength) {
				return 0, 0, ErrLength
			}
			return int(value), i + 1, nil
		}
	}
	return 0, 0, ErrLength
}

func Frame(body []byte) ([]byte, error) {
	prefix, err := EncodeLength(len(body))
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(prefix)+len(body))
	out = append(out, prefix...)
	out = append(out, body...)
	return out, nil
}
