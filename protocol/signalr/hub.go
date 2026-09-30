package signalr

import (
	"errors"
	"fmt"
)

const (
	HubMessageTypeStreamItem       = 2
	HubMessageTypeCompletion       = 3
	HubMessageTypeStreamInvocation = 4
	HubMessageTypeCancelInvocation = 5
	HubMessageTypePing             = 6
	HubMessageTypeClose            = 7
)

// ParseHubMessage decodes a MessagePack Hub Protocol body and returns its
// numeric message type plus the complete top-level array.
func ParseHubMessage(body []byte) (int64, []any, error) {
	v, err := DecodeValue(body)
	if err != nil {
		return 0, nil, err
	}
	a, ok := v.([]any)
	if !ok || len(a) == 0 {
		return 0, nil, errors.New("signalr: hub message is not a non-empty array")
	}
	mt, ok := asInt64(a[0])
	if !ok {
		return 0, nil, ErrMessagePack
	}
	return mt, a, nil
}

func CloseError(a []any) error {
	if len(a) < 1 {
		return ErrMessagePack
	}
	if len(a) >= 2 && a[1] != nil {
		s, ok := a[1].(string)
		if !ok {
			return ErrMessagePack
		}
		if s != "" {
			return fmt.Errorf("signalr: server closed hub: %s", s)
		}
	}
	return errors.New("signalr: server closed hub")
}
