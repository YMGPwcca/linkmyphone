package clipboard

import (
	"errors"
	"fmt"
)

var ErrInvalidClipboardPublication = errors.New("clipboard: invalid PubSub publication")

// NewPCClipboardChangePublication builds the payload used by Windows when the
// local PC clipboard changes. The serialized ClipboardResponseMessage is
// published in PubSubPayload.Data.
func NewPCClipboardChangePublication(correlationID string) PubSubPayload {
	return PubSubPayload{Data: MarshalResponse(NewClipboardChange(correlationID))}
}

// ParsePhoneClipboardChangePublication parses the payload delivered to the
// Windows-side subscriber when the phone clipboard changes. On Windows the
// delivered ClipboardResponseMessage is in PubSubPayload.Additional.
func ParsePhoneClipboardChangePublication(p PubSubPayload) (string, error) {
	if len(p.Additional) == 0 {
		return "", fmt.Errorf("%w: missing Additional field", ErrInvalidClipboardPublication)
	}
	resp, err := UnmarshalResponse(p.Additional)
	if err != nil {
		return "", err
	}
	if resp.Status != ResponseClipboardChange {
		return "", fmt.Errorf("%w: status %d", ErrInvalidClipboardPublication, resp.Status)
	}
	return resp.CorrelationID, nil
}
