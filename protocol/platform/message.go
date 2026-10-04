// Package platform implements the shared binary APP/PLATFORM message framing
// and the PLATFORM request routes used by Phone Link.
package platform

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	Version1 = byte(1)

	HeaderContentType       = "ms-content-type"
	HeaderRoute             = "_route"
	HeaderRejectionVersion  = "_rejectionVersion"
	HeaderRequestID         = "_requestId"
	HeaderOriginalRequestID = "_originalRequestId"
	HeaderRejectedReason    = "_rejectedReason"

	ContentTypeBinary          = "application/x-binary"
	RouteDeviceResourceManager = "/DeviceResourceManager"
	RouteContextPublish        = "/Context/Publish"
	RouteSessionValidation     = "/SessionValidation"
	RouteInternalResponse      = "/internal/response"
)

var ErrMalformed = errors.New("malformed platform message")

type Header struct {
	Key   string
	Value string
}

type Message struct {
	Version byte
	Headers []Header
	Payload []byte
}

func NewSessionValidationRequest(payload []byte, requestID string) Message {
	return Message{
		Version: Version1,
		Headers: []Header{
			{Key: HeaderContentType, Value: ContentTypeBinary},
			{Key: HeaderRoute, Value: RouteSessionValidation},
			{Key: HeaderRejectionVersion, Value: "1"},
			{Key: HeaderRequestID, Value: requestID},
		},
		Payload: append([]byte(nil), payload...),
	}
}

func NewDeviceResourceRequest(payload []byte, requestID string) Message {
	return Message{
		Version: Version1,
		Headers: []Header{
			{Key: HeaderContentType, Value: ContentTypeBinary},
			{Key: HeaderRoute, Value: RouteDeviceResourceManager},
			{Key: HeaderRejectionVersion, Value: "1"},
			{Key: HeaderRequestID, Value: requestID},
		},
		Payload: append([]byte(nil), payload...),
	}
}

// NewContextPublish builds the standard PLATFORM request envelope used by
// SignalRContextProvider for cloud PubSub publications.
func NewContextPublish(payload []byte, requestID string) Message {
	return Message{
		Version: Version1,
		Headers: []Header{
			{Key: HeaderContentType, Value: ContentTypeBinary},
			{Key: HeaderRoute, Value: RouteContextPublish},
			{Key: HeaderRejectionVersion, Value: "1"},
			{Key: HeaderRequestID, Value: requestID},
		},
		Payload: append([]byte(nil), payload...),
	}
}

func NewInternalResponse(payload []byte, originalRequestID string) Message {
	return Message{
		Version: Version1,
		Headers: []Header{
			{Key: HeaderContentType, Value: ContentTypeBinary},
			{Key: HeaderRoute, Value: RouteInternalResponse},
			{Key: HeaderRejectionVersion, Value: "1"},
			{Key: HeaderOriginalRequestID, Value: originalRequestID},
		},
		Payload: append([]byte(nil), payload...),
	}
}

func Marshal(m Message) ([]byte, error) {
	return marshal(m, false)
}

// MarshalWithHeaderCount uses Windows's length convention, where the header
// count is included in the advertised header section length.
func MarshalWithHeaderCount(m Message) ([]byte, error) {
	return marshal(m, true)
}

func marshal(m Message, includeHeaderCount bool) ([]byte, error) {
	version := m.Version
	if version == 0 {
		version = Version1
	}
	if version != Version1 {
		return nil, fmt.Errorf("platform: unsupported version %d", version)
	}
	if len(m.Headers) > math.MaxUint32 {
		return nil, errors.New("platform: too many headers")
	}

	var headerBytes bytes.Buffer
	for _, h := range m.Headers {
		if err := writeU32String(&headerBytes, h.Key); err != nil {
			return nil, err
		}
		if err := writeU32String(&headerBytes, h.Value); err != nil {
			return nil, err
		}
	}
	headerLength := uint64(headerBytes.Len())
	if includeHeaderCount {
		headerLength += 4
	}
	if headerLength > math.MaxUint32 {
		return nil, errors.New("platform: headers too large")
	}

	var out bytes.Buffer
	out.Grow(1 + 4 + 4 + headerBytes.Len() + 8 + len(m.Payload))
	out.WriteByte(version)
	_ = binary.Write(&out, binary.LittleEndian, uint32(headerLength))
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(m.Headers)))
	out.Write(headerBytes.Bytes())
	_ = binary.Write(&out, binary.LittleEndian, uint64(len(m.Payload)))
	out.Write(m.Payload)
	return out.Bytes(), nil
}

func Unmarshal(b []byte) (Message, error) {
	if len(b) < 17 {
		return Message{}, ErrMalformed
	}
	if b[0] != Version1 {
		return Message{}, fmt.Errorf("%w: unsupported version %d", ErrMalformed, b[0])
	}
	headerLength := binary.LittleEndian.Uint32(b[1:5])
	headerCount := binary.LittleEndian.Uint32(b[5:9])
	// Even an empty key/value pair consumes two four-byte length fields.
	// Bound the allocation by actual input, not an untrusted header count.
	if uint64(headerCount) > uint64((len(b)-17)/8) {
		return Message{}, ErrMalformed
	}
	m := Message{Version: Version1, Headers: make([]Header, 0, int(headerCount))}
	offset := 9
	for range headerCount {
		key, err := readStringAt(b, &offset)
		if err != nil {
			return Message{}, err
		}
		value, err := readStringAt(b, &offset)
		if err != nil {
			return Message{}, err
		}
		m.Headers = append(m.Headers, Header{Key: key, Value: value})
	}
	actualHeaderLength := uint64(offset - 9)
	// Android excludes the count; Windows includes it. No other mismatch is
	// accepted, and payload bounds remain independent of either convention.
	if uint64(headerLength) != actualHeaderLength && uint64(headerLength) != actualHeaderLength+4 {
		return Message{}, fmt.Errorf("%w: header byte length/count mismatch", ErrMalformed)
	}
	if len(b)-offset < 8 {
		return Message{}, ErrMalformed
	}
	payloadLength := binary.LittleEndian.Uint64(b[offset : offset+8])
	offset += 8
	if payloadLength > uint64(len(b)-offset) {
		return Message{}, ErrMalformed
	}
	if payloadLength != uint64(len(b)-offset) {
		return Message{}, fmt.Errorf("%w: trailing bytes", ErrMalformed)
	}
	m.Payload = append([]byte(nil), b[offset:]...)
	return m, nil
}

func (m Message) Header(key string) (string, bool) {
	for _, h := range m.Headers {
		if h.Key == key {
			return h.Value, true
		}
	}
	return "", false
}

func writeU32String(w io.Writer, s string) error {
	if len(s) > math.MaxUint32 {
		return errors.New("platform: string too large")
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(s))); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

func readStringAt(b []byte, offset *int) (string, error) {
	if len(b)-*offset < 4 {
		return "", ErrMalformed
	}
	length := binary.LittleEndian.Uint32(b[*offset : *offset+4])
	*offset += 4
	if uint64(length) > uint64(len(b)-*offset) {
		return "", ErrMalformed
	}
	end := *offset + int(length)
	value := string(b[*offset:end])
	*offset = end
	return value, nil
}
