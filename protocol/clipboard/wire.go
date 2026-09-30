package clipboard

import (
	"errors"
	"fmt"
)

var ErrMalformed = errors.New("malformed protobuf payload")

func appendVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func appendKey(dst []byte, field uint64, wire uint64) []byte {
	return appendVarint(dst, field<<3|wire)
}

func appendBytesField(dst []byte, field uint64, b []byte) []byte {
	if len(b) == 0 {
		return dst
	}
	dst = appendKey(dst, field, 2)
	dst = appendVarint(dst, uint64(len(b)))
	return append(dst, b...)
}

func appendStringField(dst []byte, field uint64, s string) []byte {
	if s == "" {
		return dst
	}
	return appendBytesField(dst, field, []byte(s))
}

func appendVarintField(dst []byte, field, v uint64) []byte {
	if v == 0 {
		return dst
	}
	dst = appendKey(dst, field, 0)
	return appendVarint(dst, v)
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
	if err != nil {
		return nil, err
	}
	if n > uint64(len(b)-*i) {
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
		return fmt.Errorf("%w: unsupported wire type %d", ErrMalformed, wire)
	}
}
