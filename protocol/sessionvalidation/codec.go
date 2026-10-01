package sessionvalidation

import (
	"errors"
	"fmt"
)

type Capability int32

const (
	CapabilityUnspecified             Capability = 0
	CapabilitySessionValidation       Capability = 1
	CapabilityPersistentMessageChannel Capability = 2
	CapabilityNanoTransportPreference Capability = 3
)

type Request struct {
	Capabilities []Capability
}

type Response struct {
	Capabilities                      []Capability
	PersistentMessagingChannelVersion int32
	NanoTransportPreferenceVersion    int32
}

func (r Response) Has(cap Capability) bool {
	for _, got := range r.Capabilities {
		if got == cap {
			return true
		}
	}
	return false
}

// MarshalRequest mirrors ProtoPlatformSessionValidationRequest. Field 1 is a
// packed repeated enum; the source-confirmed baseline Windows capability set
// always contains SessionValidation.
func MarshalRequest(request Request) []byte {
	if len(request.Capabilities) == 0 {
		return nil
	}
	packed := make([]byte, 0, len(request.Capabilities))
	for _, cap := range request.Capabilities {
		packed = appendVarint(packed, uint64(cap))
	}
	out := []byte{0x0a}
	out = appendVarint(out, uint64(len(packed)))
	return append(out, packed...)
}

func MarshalResponse(response Response) []byte {
	var out []byte
	if len(response.Capabilities) != 0 {
		packed := make([]byte, 0, len(response.Capabilities))
		for _, cap := range response.Capabilities {
			packed = appendVarint(packed, uint64(cap))
		}
		out = append(out, 0x0a)
		out = appendVarint(out, uint64(len(packed)))
		out = append(out, packed...)
	}
	if response.PersistentMessagingChannelVersion != 0 {
		out = append(out, 0x10)
		out = appendVarint(out, uint64(response.PersistentMessagingChannelVersion))
	}
	if response.NanoTransportPreferenceVersion != 0 {
		out = append(out, 0x18)
		out = appendVarint(out, uint64(response.NanoTransportPreferenceVersion))
	}
	return out
}

func UnmarshalResponse(data []byte) (Response, error) {
	var out Response
	for len(data) != 0 {
		key, n, err := consumeVarint(data)
		if err != nil {
			return Response{}, err
		}
		data = data[n:]
		field := int(key >> 3)
		wire := int(key & 7)

		switch field {
		case 1:
			switch wire {
			case 0:
				value, consumed, err := consumeVarint(data)
				if err != nil {
					return Response{}, err
				}
				data = data[consumed:]
				out.Capabilities = append(out.Capabilities, Capability(value))
			case 2:
				length, consumed, err := consumeVarint(data)
				if err != nil {
					return Response{}, err
				}
				data = data[consumed:]
				if length > uint64(len(data)) {
					return Response{}, errors.New("sessionvalidation: truncated packed capabilities")
				}
				packed := data[:int(length)]
				data = data[int(length):]
				for len(packed) != 0 {
					value, used, err := consumeVarint(packed)
					if err != nil {
						return Response{}, err
					}
					packed = packed[used:]
					out.Capabilities = append(out.Capabilities, Capability(value))
				}
			default:
				return Response{}, fmt.Errorf("sessionvalidation: invalid capability wire type %d", wire)
			}
		case 2, 3:
			if wire != 0 {
				return Response{}, fmt.Errorf("sessionvalidation: invalid version wire type %d", wire)
			}
			value, consumed, err := consumeVarint(data)
			if err != nil {
				return Response{}, err
			}
			data = data[consumed:]
			if field == 2 {
				out.PersistentMessagingChannelVersion = int32(value)
			} else {
				out.NanoTransportPreferenceVersion = int32(value)
			}
		default:
			var err error
			data, err = skipField(data, wire)
			if err != nil {
				return Response{}, err
			}
		}
	}
	return out, nil
}

func Name(cap Capability) string {
	switch cap {
	case CapabilitySessionValidation:
		return "SessionValidation"
	case CapabilityPersistentMessageChannel:
		return "PersistentMessageChannel"
	case CapabilityNanoTransportPreference:
		return "NanoTransportPreference"
	case CapabilityUnspecified:
		return "Unspecified"
	default:
		return fmt.Sprintf("Unknown(%d)", cap)
	}
}

func appendVarint(dst []byte, value uint64) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}

func consumeVarint(data []byte) (uint64, int, error) {
	var value uint64
	for i := 0; i < len(data) && i < 10; i++ {
		b := data[i]
		if i == 9 && b > 1 {
			return 0, 0, errors.New("sessionvalidation: varint overflow")
		}
		value |= uint64(b&0x7f) << (7 * i)
		if b < 0x80 {
			return value, i + 1, nil
		}
	}
	if len(data) < 10 {
		return 0, 0, errors.New("sessionvalidation: truncated varint")
	}
	return 0, 0, errors.New("sessionvalidation: varint overflow")
}

func skipField(data []byte, wire int) ([]byte, error) {
	switch wire {
	case 0:
		_, n, err := consumeVarint(data)
		if err != nil {
			return nil, err
		}
		return data[n:], nil
	case 1:
		if len(data) < 8 {
			return nil, errors.New("sessionvalidation: truncated fixed64")
		}
		return data[8:], nil
	case 2:
		length, n, err := consumeVarint(data)
		if err != nil {
			return nil, err
		}
		data = data[n:]
		if length > uint64(len(data)) {
			return nil, errors.New("sessionvalidation: truncated bytes field")
		}
		return data[int(length):], nil
	case 5:
		if len(data) < 4 {
			return nil, errors.New("sessionvalidation: truncated fixed32")
		}
		return data[4:], nil
	default:
		return nil, fmt.Errorf("sessionvalidation: unsupported wire type %d", wire)
	}
}
