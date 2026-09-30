// Package platform implements the binary platform-message framing used below
// Phone Link's DeviceResourceManager route.
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

	ContentTypeBinary          = "application/x-binary"
	RouteDeviceResourceManager = "/DeviceResourceManager"
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
	if headerBytes.Len() > math.MaxUint32 {
		return nil, errors.New("platform: headers too large")
	}

	var out bytes.Buffer
	out.Grow(1 + 4 + 4 + headerBytes.Len() + 8 + len(m.Payload))
	out.WriteByte(version)
	_ = binary.Write(&out, binary.LittleEndian, uint32(headerBytes.Len()))
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(m.Headers)))
	out.Write(headerBytes.Bytes())
	_ = binary.Write(&out, binary.LittleEndian, uint64(len(m.Payload)))
	out.Write(m.Payload)
	return out.Bytes(), nil
}

func Unmarshal(b []byte) (Message, error) {
	var m Message
	r := bytes.NewReader(b)

	version, err := r.ReadByte()
	if err != nil {
		return m, ErrMalformed
	}
	if version != Version1 {
		return m, fmt.Errorf("%w: unsupported version %d", ErrMalformed, version)
	}
	m.Version = version

	var headerBytesLen uint32
	var headerCount uint32
	if binary.Read(r, binary.LittleEndian, &headerBytesLen) != nil ||
		binary.Read(r, binary.LittleEndian, &headerCount) != nil {
		return Message{}, ErrMalformed
	}
	if uint64(headerBytesLen) > uint64(r.Len()) {
		return Message{}, ErrMalformed
	}

	headerSection := make([]byte, int(headerBytesLen))
	if _, err := io.ReadFull(r, headerSection); err != nil {
		return Message{}, ErrMalformed
	}
	hr := bytes.NewReader(headerSection)
	m.Headers = make([]Header, 0, int(headerCount))
	for i := uint32(0); i < headerCount; i++ {
		key, err := readU32String(hr)
		if err != nil {
			return Message{}, err
		}
		value, err := readU32String(hr)
		if err != nil {
			return Message{}, err
		}
		m.Headers = append(m.Headers, Header{Key: key, Value: value})
	}
	if hr.Len() != 0 {
		return Message{}, fmt.Errorf("%w: header byte length/count mismatch", ErrMalformed)
	}

	var payloadLen uint64
	if binary.Read(r, binary.LittleEndian, &payloadLen) != nil {
		return Message{}, ErrMalformed
	}
	if payloadLen > uint64(r.Len()) || payloadLen > uint64(math.MaxInt) {
		return Message{}, ErrMalformed
	}
	m.Payload = make([]byte, int(payloadLen))
	if _, err := io.ReadFull(r, m.Payload); err != nil {
		return Message{}, ErrMalformed
	}
	if r.Len() != 0 {
		return Message{}, fmt.Errorf("%w: trailing bytes", ErrMalformed)
	}
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

func readU32String(r *bytes.Reader) (string, error) {
	var n uint32
	if binary.Read(r, binary.LittleEndian, &n) != nil {
		return "", ErrMalformed
	}
	if uint64(n) > uint64(r.Len()) {
		return "", ErrMalformed
	}
	v := make([]byte, int(n))
	if _, err := io.ReadFull(r, v); err != nil {
		return "", ErrMalformed
	}
	return string(v), nil
}
