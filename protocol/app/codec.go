// Package app implements the typed APP ValueSet payload and envelope used by
// the Phone Link remote-app routes.  The payload is the PBValueSet schema,
// written directly here to avoid a generated-schema dependency.
package app

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/YMGPwcca/linkmyphone/protocol/platform"
)

// ValueSet is the WinRT-style string to typed-value map carried by APP.
type ValueSet map[string]any

// Optional named values cover PBValueSet variants which do not have a direct
// Go primitive without losing their intended width or meaning.
type UInt8 uint8
type Int16 int16
type UInt16 uint16
type Char16 uint16
type DateTime int64
type TimeSpan int64
type Guid string
type Point struct{ X, Y float32 }
type Size struct{ Width, Height float32 }
type Rect struct{ X, Y, Width, Height float32 }

type UInt8Array []uint8
type Int16Array []int16
type UInt16Array []uint16
type Char16Array []uint16
type DateTimeArray []int64
type TimeSpanArray []int64

var ErrMalformed = errors.New("malformed app values")

const (
	maxWire       = 16 << 20
	maxEntries    = 4096
	maxArray      = 4096
	maxString     = 1 << 20
	maxDepth      = 32
	maxHeader     = 64
	maxHeaderText = 1 << 16
)

// Message is an APP envelope containing typed values and transport headers.
type Message struct {
	Headers []platform.Header
	Values  ValueSet
}

// NewRequest constructs a request for an APP route.  Correlation belongs in
// the outer transport header; correlationVector remains an application value.
func NewRequest(route, requestID string, values ValueSet) Message {
	return Message{Headers: []platform.Header{{Key: platform.HeaderRoute, Value: route}, {Key: platform.HeaderRequestID, Value: requestID}}, Values: values}
}

// NewResponse constructs the standard APP response envelope.  Windows also
// places the result as a textual header when a typed result is present.
func NewResponse(originalRequestID string, values ValueSet) Message {
	headers := []platform.Header{{Key: platform.HeaderRoute, Value: platform.RouteInternalResponse}, {Key: platform.HeaderOriginalRequestID, Value: originalRequestID}}
	if values != nil {
		if result, ok := values["result"].(int32); ok {
			headers = append(headers, platform.Header{Key: "result", Value: fmt.Sprintf("%d", result)})
		}
	}
	return Message{Headers: headers, Values: values}
}

func (m Message) Header(key string) (string, bool) { return (&m).header(key) }
func (m *Message) header(key string) (string, bool) {
	for _, h := range m.Headers {
		if h.Key == key {
			return h.Value, true
		}
	}
	return "", false
}

func MarshalMessage(m Message) ([]byte, error) {
	payload, err := Marshal(m.Values)
	if err != nil {
		return nil, err
	}
	return platform.MarshalWithHeaderCount(platform.Message{Version: platform.Version1, Headers: m.Headers, Payload: payload})
}

// UnmarshalEnvelope validates APP framing and headers without assuming that
// every APP route carries PBValueSet. The returned payload belongs to the
// decoded frame; callers select a route before decoding its body.
func UnmarshalEnvelope(wire []byte) (platform.Message, error) {
	if len(wire) > maxWire+maxHeader*2*maxHeaderText+maxHeader*8+17 {
		return platform.Message{}, fmt.Errorf("%w: envelope too large", ErrMalformed)
	}
	outer, err := platform.Unmarshal(wire)
	if err != nil {
		return platform.Message{}, fmt.Errorf("%w: envelope: %v", ErrMalformed, err)
	}
	if len(outer.Headers) > maxHeader {
		return platform.Message{}, fmt.Errorf("%w: too many headers", ErrMalformed)
	}
	for _, h := range outer.Headers {
		if len(h.Key) > maxHeaderText || len(h.Value) > maxHeaderText {
			return platform.Message{}, fmt.Errorf("%w: header too large", ErrMalformed)
		}
	}
	return outer, nil
}

func UnmarshalMessage(wire []byte) (Message, error) {
	outer, err := UnmarshalEnvelope(wire)
	if err != nil {
		return Message{}, err
	}
	values, err := Unmarshal(outer.Payload)
	if err != nil {
		return Message{}, err
	}
	return Message{Headers: outer.Headers, Values: values}, nil
}

// Marshal encodes a PBValueSet ValueSet message.
func Marshal(values ValueSet) ([]byte, error) { return marshalSet(values, 0) }

func marshalSet(values ValueSet, depth int) ([]byte, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%w: depth limit", ErrMalformed)
	}
	if len(values) > maxEntries {
		return nil, fmt.Errorf("%w: too many entries", ErrMalformed)
	}
	var out []byte
	for key := range values {
		if len(key) > maxString {
			return nil, fmt.Errorf("%w: key too large", ErrMalformed)
		}
		variant, err := marshalVariant(values[key], depth)
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %v", ErrMalformed, key, err)
		}
		var entry []byte
		entry = appendStringField(entry, 1, key)
		entry = appendBytesField(entry, 2, variant)
		out = appendBytesField(out, 1, entry)
		if len(out) > maxWire {
			return nil, fmt.Errorf("%w: payload too large", ErrMalformed)
		}
	}
	return out, nil
}

// Unmarshal decodes a PBValueSet ValueSet message with strict size/depth
// limits. Unknown protobuf fields are skipped for forward compatibility.
func Unmarshal(wire []byte) (ValueSet, error) {
	if len(wire) > maxWire {
		return nil, fmt.Errorf("%w: payload too large", ErrMalformed)
	}
	return unmarshalSet(wire, 0)
}

// PeekString inspects one top-level PBValueSet string without decoding the
// other values. APP routes can share a PBValueSet envelope while carrying
// large binary content belonging to another feature.
func PeekString(wire []byte, key string) (string, bool, error) {
	if len(wire) > maxWire {
		return "", false, fmt.Errorf("%w: payload too large", ErrMalformed)
	}
	var result string
	found := false
	for off, entries := 0, 0; off < len(wire); entries++ {
		if entries >= maxEntries {
			return "", false, fmt.Errorf("%w: too many entries", ErrMalformed)
		}
		tag, n, err := readVarint(wire, off)
		if err != nil {
			return "", false, err
		}
		off += n
		field, wt := int(tag>>3), int(tag&7)
		if field == 0 {
			return "", false, fmt.Errorf("%w: field zero", ErrMalformed)
		}
		if field != 1 {
			skip, err := skipWire(wire, off, wt)
			if err != nil {
				return "", false, err
			}
			off += skip
			continue
		}
		if wt != 2 {
			return "", false, fmt.Errorf("%w: map entry wire type", ErrMalformed)
		}
		entry, used, err := readBytes(wire, off)
		if err != nil {
			return "", false, err
		}
		off += used
		name, encoded, err := splitEntry(entry)
		if err != nil {
			return "", false, err
		}
		if string(name) != key {
			continue
		}
		value, err := unmarshalVariant(encoded, 0)
		if err != nil {
			return "", false, err
		}
		result, found = value.(string)
	}
	return result, found, nil
}

func unmarshalSet(wire []byte, depth int) (ValueSet, error) {
	if depth > maxDepth || len(wire) > maxWire {
		return nil, fmt.Errorf("%w: depth or size limit", ErrMalformed)
	}
	result := make(ValueSet)
	for off, entries := 0, 0; off < len(wire); entries++ {
		if entries >= maxEntries {
			return nil, fmt.Errorf("%w: too many entries", ErrMalformed)
		}
		tag, n, err := readVarint(wire, off)
		if err != nil {
			return nil, err
		}
		off += n
		field, wt := int(tag>>3), int(tag&7)
		if field == 0 {
			return nil, fmt.Errorf("%w: field zero", ErrMalformed)
		}
		if field != 1 {
			var skip int
			skip, err = skipWire(wire, off, wt)
			if err != nil {
				return nil, err
			}
			off += skip
			continue
		}
		if wt != 2 {
			return nil, fmt.Errorf("%w: map entry wire type", ErrMalformed)
		}
		entry, used, err := readBytes(wire, off)
		if err != nil {
			return nil, err
		}
		off += used
		key, value, err := unmarshalEntry(entry, depth)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

func unmarshalEntry(wire []byte, depth int) (string, any, error) {
	key, value, err := splitEntry(wire)
	if err != nil {
		return "", nil, err
	}
	v, err := unmarshalVariant(value, depth)
	return string(key), v, err
}

func splitEntry(wire []byte) ([]byte, []byte, error) {
	var key, value []byte
	gotKey, gotValue := false, false
	for off := 0; off < len(wire); {
		tag, n, err := readVarint(wire, off)
		if err != nil {
			return nil, nil, err
		}
		off += n
		switch int(tag >> 3) {
		case 1:
			if tag&7 != 2 {
				return nil, nil, fmt.Errorf("%w: map key wire type", ErrMalformed)
			}
			b, used, err := readBytes(wire, off)
			if err != nil {
				return nil, nil, err
			}
			off += used
			if len(b) > maxString {
				return nil, nil, fmt.Errorf("%w: key too large", ErrMalformed)
			}
			key, gotKey = b, true
		case 2:
			if tag&7 != 2 {
				return nil, nil, fmt.Errorf("%w: map value wire type", ErrMalformed)
			}
			b, used, err := readBytes(wire, off)
			if err != nil {
				return nil, nil, err
			}
			off += used
			value, gotValue = b, true
		default:
			skip, err := skipWire(wire, off, int(tag&7))
			if err != nil {
				return nil, nil, err
			}
			off += skip
		}
	}
	if !gotKey || !gotValue {
		return nil, nil, fmt.Errorf("%w: incomplete map entry", ErrMalformed)
	}
	return key, value, nil
}

const (
	tInvalid       = 0
	tUInt8         = 1
	tInt16         = 2
	tUInt16        = 3
	tInt32         = 4
	tUInt32        = 5
	tInt64         = 6
	tUInt64        = 7
	tSingle        = 8
	tDouble        = 9
	tChar16        = 10
	tBool          = 11
	tString        = 12
	tGuid          = 13
	tPoint         = 14
	tSize          = 15
	tRect          = 16
	tObject        = 17
	tUInt8Array    = 18
	tInt16Array    = 19
	tUInt16Array   = 20
	tInt32Array    = 21
	tUInt32Array   = 22
	tInt64Array    = 23
	tUInt64Array   = 24
	tSingleArray   = 25
	tDoubleArray   = 26
	tChar16Array   = 27
	tBoolArray     = 28
	tStringArray   = 29
	tGuidArray     = 30
	tPointArray    = 31
	tSizeArray     = 32
	tRectArray     = 33
	tObjectArray   = 34
	tEmpty         = 35
	tDateTime      = 36
	tTimeSpan      = 37
	tDateTimeArray = 38
	tTimeSpanArray = 39
)

func marshalVariant(value any, depth int) ([]byte, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("depth limit")
	}
	var typ int
	var out []byte
	putVarint := func(field int, value uint64) { out = appendTag(out, field, 0); out = appendUvarint(out, value) }
	putInt := func(field int, value int64) { putVarint(field, uint64(value)) }
	switch v := value.(type) {
	case UInt8:
		typ = tUInt8
		if v > 255 {
			return nil, fmt.Errorf("uint8 range")
		}
		putInt(2, int64(v))
	case Int16:
		typ = tInt16
		putInt(3, int64(v))
	case UInt16:
		typ = tUInt16
		putInt(4, int64(v))
	case int32:
		typ = tInt32
		putInt(5, int64(v))
	case uint32:
		typ = tUInt32
		putVarint(6, uint64(v))
	case int64:
		typ = tInt64
		putInt(7, v)
	case uint64:
		typ = tUInt64
		putVarint(8, v)
	case float32:
		typ = tSingle
		out = appendTag(out, 9, 5)
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		out = append(out, b[:]...)
	case float64:
		typ = tDouble
		out = appendTag(out, 10, 1)
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
		out = append(out, b[:]...)
	case Char16:
		typ = tChar16
		putVarint(11, uint64(v))
	case bool:
		typ = tBool
		if v {
			putVarint(12, 1)
		} else {
			putVarint(12, 0)
		}
	case string:
		typ = tString
		if len(v) > maxString {
			return nil, fmt.Errorf("string too large")
		}
		out = appendStringField(out, 13, v)
	case Guid:
		typ = tGuid
		if len(v) > maxString {
			return nil, fmt.Errorf("guid too large")
		}
		out = appendStringField(out, 14, string(v))
	case Point:
		typ = tPoint
		out = appendBytesField(out, 15, marshalPoint(v))
	case Size:
		typ = tSize
		out = appendBytesField(out, 16, marshalSize(v))
	case Rect:
		typ = tRect
		out = appendBytesField(out, 17, marshalRect(v))
	case ValueSet:
		typ = tObject
		nested, err := marshalSet(v, depth+1)
		if err != nil {
			return nil, err
		}
		out = appendBytesField(out, 18, nested)
	case DateTime:
		typ = tDateTime
		putInt(19, int64(v))
	case TimeSpan:
		typ = tTimeSpan
		putInt(20, int64(v))
	case []byte:
		typ = tUInt8Array
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		out = appendBytesField(out, 21, v)
	case UInt8Array:
		typ = tUInt8Array
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		out = appendBytesField(out, 21, []byte(v))
	case Int16Array:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tInt16Array
		out = appendPackedInts(out, 22, []int16(v), maxArray)
	case UInt16Array:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tUInt16Array
		out = appendPackedInts(out, 23, []uint16(v), maxArray)
	case []int32:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tInt32Array
		out = appendPackedInts(out, 24, v, maxArray)
	case []uint32:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tUInt32Array
		out = appendPackedUints(out, 25, v, maxArray)
	case []int64:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tInt64Array
		out = appendPackedInts(out, 26, v, maxArray)
	case []uint64:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tUInt64Array
		out = appendPackedUints(out, 27, v, maxArray)
	case []float32:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tSingleArray
		out = appendPackedFloat32(out, 28, v, maxArray)
	case []float64:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tDoubleArray
		out = appendPackedFloat64(out, 29, v, maxArray)
	case Char16Array:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tChar16Array
		out = appendPackedUints(out, 30, []uint16(v), maxArray)
	case []bool:
		typ = tBoolArray
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		var p []byte
		for _, x := range v {
			if x {
				p = appendUvarint(p, 1)
			} else {
				p = appendUvarint(p, 0)
			}
		}
		if len(p) != 0 {
			out = appendBytesField(out, 31, p)
		}
	case []string:
		typ = tStringArray
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		for _, x := range v {
			if len(x) > maxString {
				return nil, fmt.Errorf("string too large")
			}
			out = appendStringField(out, 32, x)
		}
	case []Guid:
		typ = tGuidArray
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		for _, x := range v {
			if len(x) > maxString {
				return nil, fmt.Errorf("guid too large")
			}
			out = appendStringField(out, 33, string(x))
		}
	case []Point:
		typ = tPointArray
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		for _, x := range v {
			out = appendBytesField(out, 34, marshalPoint(x))
		}
	case []Size:
		typ = tSizeArray
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		for _, x := range v {
			out = appendBytesField(out, 35, marshalSize(x))
		}
	case []Rect:
		typ = tRectArray
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		for _, x := range v {
			out = appendBytesField(out, 36, marshalRect(x))
		}
	case []ValueSet:
		typ = tObjectArray
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		for _, x := range v {
			nested, err := marshalSet(x, depth+1)
			if err != nil {
				return nil, err
			}
			out = appendBytesField(out, 37, nested)
		}
	case DateTimeArray:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tDateTimeArray
		out = appendPackedInts(out, 38, []int64(v), maxArray)
	case TimeSpanArray:
		if len(v) > maxArray {
			return nil, fmt.Errorf("array too large")
		}
		typ = tTimeSpanArray
		out = appendPackedInts(out, 39, []int64(v), maxArray)
	case nil:
		return nil, fmt.Errorf("nil values unsupported")
	default:
		return nil, fmt.Errorf("unsupported %T", value)
	}
	var result []byte
	result = appendTag(result, 1, 0)
	result = appendUvarint(result, uint64(typ))
	result = append(result, out...)
	return result, nil
}

func unmarshalVariant(wire []byte, depth int) (any, error) {
	if depth > maxDepth || len(wire) > maxWire {
		return nil, fmt.Errorf("%w: variant depth or size", ErrMalformed)
	}
	var typ uint64
	var typeSeen bool
	var seen uint64
	var i32 int32
	var u32 uint32
	var i64 int64
	var u64 uint64
	var f32 float32
	var f64 float64
	var bval bool
	var sval, gval string
	var raw []byte
	var nested ValueSet
	var ints16 []int16
	var uints16 []uint16
	var int32s []int32
	var uint32s []uint32
	var int64s []int64
	var uint64s []uint64
	var singles []float32
	var doubles []float64
	var chars []uint16
	var bools []bool
	var strings []string
	var guids []Guid
	var points []Point
	var sizes []Size
	var rects []Rect
	var objects []ValueSet
	var datetimes []int64
	var timespans []int64
	for off := 0; off < len(wire); {
		tag, n, err := readVarint(wire, off)
		if err != nil {
			return nil, err
		}
		off += n
		field, wt := int(tag>>3), int(tag&7)
		if field == 0 {
			return nil, fmt.Errorf("%w: field zero", ErrMalformed)
		}
		if field == 1 {
			if wt != 0 {
				return nil, fmt.Errorf("%w: type wire", ErrMalformed)
			}
			x, n, err := readVarint(wire, off)
			if err != nil {
				return nil, err
			}
			off += n
			typ, typeSeen = x, true
			continue
		}
		switch field {
		case 2, 3, 4, 5, 7, 11, 12, 19, 20:
			if wt != 0 {
				return nil, fmt.Errorf("%w: scalar wire", ErrMalformed)
			}
			x, n, err := readVarint(wire, off)
			if err != nil {
				return nil, err
			}
			off += n
			seen |= 1 << field
			switch field {
			case 2:
				i32 = int32(x)
			case 3:
				i32 = int32(int16(x))
			case 4:
				i32 = int32(uint16(x))
			case 5:
				i32 = int32(x)
			case 7:
				i64 = int64(x)
			case 11:
				u32 = uint32(x)
			case 12:
				bval = x != 0
			case 19:
				i64 = int64(x)
			case 20:
				i64 = int64(x)
			}
		case 6, 8:
			if wt != 0 {
				return nil, fmt.Errorf("%w: scalar wire", ErrMalformed)
			}
			x, n, err := readVarint(wire, off)
			if err != nil {
				return nil, err
			}
			off += n
			seen |= 1 << field
			if field == 6 {
				u32 = uint32(x)
			} else {
				u64 = x
			}
		case 9:
			if wt != 5 {
				return nil, fmt.Errorf("%w: float wire", ErrMalformed)
			}
			if len(wire)-off < 4 {
				return nil, fmt.Errorf("%w: float bounds", ErrMalformed)
			}
			f32 = math.Float32frombits(binary.LittleEndian.Uint32(wire[off:]))
			off += 4
			seen |= 1 << field
		case 10:
			if wt != 1 {
				return nil, fmt.Errorf("%w: double wire", ErrMalformed)
			}
			if len(wire)-off < 8 {
				return nil, fmt.Errorf("%w: double bounds", ErrMalformed)
			}
			f64 = math.Float64frombits(binary.LittleEndian.Uint64(wire[off:]))
			off += 8
			seen |= 1 << field
		case 13, 14:
			if wt != 2 {
				return nil, fmt.Errorf("%w: string wire", ErrMalformed)
			}
			x, used, err := readBytes(wire, off)
			if err != nil {
				return nil, err
			}
			off += used
			if len(x) > maxString {
				return nil, fmt.Errorf("%w: string too large", ErrMalformed)
			}
			seen |= 1 << field
			if field == 13 {
				sval = string(x)
			} else {
				gval = string(x)
			}
		case 15, 16, 17, 18:
			if wt != 2 {
				return nil, fmt.Errorf("%w: object wire", ErrMalformed)
			}
			x, used, err := readBytes(wire, off)
			if err != nil {
				return nil, err
			}
			off += used
			seen |= 1 << field
			switch field {
			case 15:
				p, err := unmarshalPoint(x)
				if err != nil {
					return nil, err
				}
				points = append(points, p)
			case 16:
				s, err := unmarshalSize(x)
				if err != nil {
					return nil, err
				}
				sizes = append(sizes, s)
			case 17:
				r, err := unmarshalRect(x)
				if err != nil {
					return nil, err
				}
				rects = append(rects, r)
			case 18:
				nested, err = unmarshalSet(x, depth+1)
				if err != nil {
					return nil, err
				}
			}
		case 21:
			if wt != 2 {
				return nil, fmt.Errorf("%w: bytes wire", ErrMalformed)
			}
			x, used, err := readBytes(wire, off)
			if err != nil {
				return nil, err
			}
			off += used
			if len(x) > maxArray {
				return nil, fmt.Errorf("%w: byte array limit", ErrMalformed)
			}
			raw = append(raw[:0], x...)
			seen |= 1 << field
		case 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39:
			var used int
			switch wt {
			case 0:
				x, n, err := readVarint(wire, off)
				if err != nil {
					return nil, err
				}
				used = n
				switch field {
				case 22:
					ints16 = append(ints16, int16(x))
				case 23:
					uints16 = append(uints16, uint16(x))
				case 24:
					int32s = append(int32s, int32(x))
				case 25:
					uint32s = append(uint32s, uint32(x))
				case 26:
					int64s = append(int64s, int64(x))
				case 27:
					uint64s = append(uint64s, x)
				case 30:
					chars = append(chars, uint16(x))
				case 31:
					bools = append(bools, x != 0)
				case 38:
					datetimes = append(datetimes, int64(x))
				case 39:
					timespans = append(timespans, int64(x))
				default:
					return nil, fmt.Errorf("%w: repeated wire", ErrMalformed)
				}
			case 5:
				if field != 28 || len(wire)-off < 4 {
					return nil, fmt.Errorf("%w: fixed32 array", ErrMalformed)
				}
				singles = append(singles, math.Float32frombits(binary.LittleEndian.Uint32(wire[off:])))
				used = 4
			case 1:
				if field != 29 || len(wire)-off < 8 {
					return nil, fmt.Errorf("%w: fixed64 array", ErrMalformed)
				}
				doubles = append(doubles, math.Float64frombits(binary.LittleEndian.Uint64(wire[off:])))
				used = 8
			case 2:
				x, n, err := readBytes(wire, off)
				if err != nil {
					return nil, err
				}
				used = n
				switch field {
				case 22:
					ints16, err = appendPackedInt16(ints16, x)
				case 23:
					uints16, err = appendPackedUint16(uints16, x)
				case 24:
					int32s, err = appendPackedInt32(int32s, x)
				case 25:
					uint32s, err = appendPackedUint32(uint32s, x)
				case 26:
					int64s, err = appendPackedInt64(int64s, x)
				case 27:
					uint64s, err = appendPackedUint64(uint64s, x)
				case 28:
					singles, err = appendPackedFloat32Values(singles, x)
				case 29:
					doubles, err = appendPackedFloat64Values(doubles, x)
				case 30:
					chars, err = appendPackedUint16(chars, x)
				case 31:
					bools, err = appendPackedBool(bools, x)
				case 38:
					datetimes, err = appendPackedInt64(datetimes, x)
				case 39:
					timespans, err = appendPackedInt64(timespans, x)
				default:
					if field < 32 || field > 37 {
						return nil, fmt.Errorf("%w: repeated wire", ErrMalformed)
					}
					switch field {
					case 32:
						if len(x) > maxString {
							return nil, fmt.Errorf("%w: string too large", ErrMalformed)
						}
						strings = append(strings, string(x))
					case 33:
						if len(x) > maxString {
							return nil, fmt.Errorf("%w: guid too large", ErrMalformed)
						}
						guids = append(guids, Guid(string(x)))
					case 34:
						p, err := unmarshalPoint(x)
						if err != nil {
							return nil, err
						}
						points = append(points, p)
					case 35:
						s, err := unmarshalSize(x)
						if err != nil {
							return nil, err
						}
						sizes = append(sizes, s)
					case 36:
						r, err := unmarshalRect(x)
						if err != nil {
							return nil, err
						}
						rects = append(rects, r)
					case 37:
						o, err := unmarshalSet(x, depth+1)
						if err != nil {
							return nil, err
						}
						objects = append(objects, o)
					}
				}
				if err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("%w: repeated wire", ErrMalformed)
			}
			off += used
			seen |= 1 << field
		case 40:
			return nil, fmt.Errorf("%w: invalid field", ErrMalformed)
		default:
			skip, err := skipWire(wire, off, wt)
			if err != nil {
				return nil, err
			}
			off += skip
		}
		if len(ints16)+len(uints16)+len(int32s)+len(uint32s)+len(int64s)+len(uint64s)+len(singles)+len(doubles)+len(chars)+len(bools)+len(strings)+len(guids)+len(points)+len(sizes)+len(rects)+len(objects)+len(datetimes)+len(timespans) > maxArray {
			return nil, fmt.Errorf("%w: array limit", ErrMalformed)
		}
	}
	if !typeSeen || typ == tInvalid || typ > tTimeSpanArray {
		return nil, fmt.Errorf("%w: invalid value type", ErrMalformed)
	}
	valueFields := [...]uint8{0, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 0, 19, 20, 38, 39}
	if seen & ^(uint64(1)<<valueFields[typ]) != 0 {
		return nil, fmt.Errorf("%w: value field mismatch", ErrMalformed)
	}
	switch typ {
	case tUInt8:
		return UInt8(i32), nil
	case tInt16:
		return Int16(i32), nil
	case tUInt16:
		return UInt16(i32), nil
	case tInt32:
		return i32, nil
	case tUInt32:
		return u32, nil
	case tInt64:
		return i64, nil
	case tUInt64:
		return u64, nil
	case tSingle:
		return f32, nil
	case tDouble:
		return f64, nil
	case tChar16:
		return Char16(u32), nil
	case tBool:
		return bval, nil
	case tString:
		return sval, nil
	case tGuid:
		return Guid(gval), nil
	case tPoint:
		if len(points) == 0 {
			return Point{}, nil
		}
		return points[0], nil
	case tSize:
		if len(sizes) == 0 {
			return Size{}, nil
		}
		return sizes[0], nil
	case tRect:
		if len(rects) == 0 {
			return Rect{}, nil
		}
		return rects[0], nil
	case tObject:
		return nested, nil
	case tUInt8Array:
		return raw, nil
	case tInt16Array:
		return Int16Array(ints16), nil
	case tUInt16Array:
		return UInt16Array(uints16), nil
	case tInt32Array:
		return int32s, nil
	case tUInt32Array:
		return uint32s, nil
	case tInt64Array:
		return int64s, nil
	case tUInt64Array:
		return uint64s, nil
	case tSingleArray:
		return singles, nil
	case tDoubleArray:
		return doubles, nil
	case tChar16Array:
		return Char16Array(chars), nil
	case tBoolArray:
		return bools, nil
	case tStringArray:
		return strings, nil
	case tGuidArray:
		return guids, nil
	case tPointArray:
		return points, nil
	case tSizeArray:
		return sizes, nil
	case tRectArray:
		return rects, nil
	case tObjectArray:
		return objects, nil
	case tEmpty:
		return nil, nil
	case tDateTime:
		return DateTime(i64), nil
	case tTimeSpan:
		return TimeSpan(i64), nil
	case tDateTimeArray:
		return DateTimeArray(datetimes), nil
	case tTimeSpanArray:
		return TimeSpanArray(timespans), nil
	}
	return nil, fmt.Errorf("%w: unsupported value type", ErrMalformed)
}

func appendTag(dst []byte, field, wt int) []byte { return appendUvarint(dst, uint64(field<<3|wt)) }
func appendUvarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}
func appendBytesField(dst []byte, field int, b []byte) []byte {
	dst = appendTag(dst, field, 2)
	dst = appendUvarint(dst, uint64(len(b)))
	return append(dst, b...)
}
func appendStringField(dst []byte, field int, s string) []byte {
	return appendBytesField(dst, field, []byte(s))
}
func appendPackedInts[T ~int16 | ~uint16 | ~int32 | ~int64](dst []byte, field int, values []T, limit int) []byte {
	if len(values) == 0 || len(values) > limit {
		return dst
	}
	var p []byte
	for _, v := range values {
		p = appendUvarint(p, uint64(int64(v)))
	}
	return appendBytesField(dst, field, p)
}
func appendPackedUints[T ~uint16 | ~uint32 | ~uint64](dst []byte, field int, values []T, limit int) []byte {
	if len(values) == 0 || len(values) > limit {
		return dst
	}
	var p []byte
	for _, v := range values {
		p = appendUvarint(p, uint64(v))
	}
	return appendBytesField(dst, field, p)
}
func appendPackedFloat32(dst []byte, field int, values []float32, limit int) []byte {
	if len(values) == 0 {
		return dst
	}
	var p []byte
	for _, v := range values {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		p = append(p, b[:]...)
	}
	return appendBytesField(dst, field, p)
}
func appendPackedFloat64(dst []byte, field int, values []float64, limit int) []byte {
	if len(values) == 0 {
		return dst
	}
	var p []byte
	for _, v := range values {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
		p = append(p, b[:]...)
	}
	return appendBytesField(dst, field, p)
}

func readVarint(b []byte, off int) (uint64, int, error) {
	var x uint64
	for i := 0; i < 10; i++ {
		if off+i >= len(b) {
			return 0, 0, fmt.Errorf("%w: varint bounds", ErrMalformed)
		}
		c := b[off+i]
		if i == 9 && c > 1 {
			return 0, 0, fmt.Errorf("%w: varint overflow", ErrMalformed)
		}
		x |= uint64(c&0x7f) << uint(7*i)
		if c < 0x80 {
			return x, i + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("%w: varint overflow", ErrMalformed)
}
func readBytes(b []byte, off int) ([]byte, int, error) {
	n, k, err := readVarint(b, off)
	if err != nil {
		return nil, 0, err
	}
	if n > uint64(len(b)-off-k) || n > maxWire {
		return nil, 0, fmt.Errorf("%w: length bounds", ErrMalformed)
	}
	start := off + k
	return b[start : start+int(n)], k + int(n), nil
}
func skipWire(b []byte, off, wt int) (int, error) {
	switch wt {
	case 0:
		_, n, e := readVarint(b, off)
		return n, e
	case 1:
		if len(b)-off < 8 {
			return 0, fmt.Errorf("%w: fixed64 bounds", ErrMalformed)
		}
		return 8, nil
	case 2:
		_, n, e := readBytes(b, off)
		return n, e
	case 5:
		if len(b)-off < 4 {
			return 0, fmt.Errorf("%w: fixed32 bounds", ErrMalformed)
		}
		return 4, nil
	default:
		return 0, fmt.Errorf("%w: wire type", ErrMalformed)
	}
}

func marshalPoint(p Point) []byte {
	var o []byte
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(p.X))
	o = appendTag(o, 1, 5)
	o = append(o, b[:]...)
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(p.Y))
	o = appendTag(o, 2, 5)
	return append(o, b[:]...)
}
func marshalSize(s Size) []byte { return marshalPoint(Point{s.Width, s.Height}) }
func marshalRect(r Rect) []byte {
	var o []byte
	for i, v := range []float32{r.X, r.Y, r.Width, r.Height} {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		o = appendTag(o, i+1, 5)
		o = append(o, b[:]...)
	}
	return o
}
func unmarshalPoint(b []byte) (Point, error) {
	var p Point
	for o := 0; o < len(b); {
		t, n, e := readVarint(b, o)
		if e != nil {
			return p, e
		}
		o += n
		if t&7 != 5 || int(t>>3) < 1 || int(t>>3) > 2 || len(b)-o < 4 {
			return p, fmt.Errorf("%w: point", ErrMalformed)
		}
		v := math.Float32frombits(binary.LittleEndian.Uint32(b[o:]))
		o += 4
		if t>>3 == 1 {
			p.X = v
		} else {
			p.Y = v
		}
	}
	return p, nil
}
func unmarshalSize(b []byte) (Size, error) { p, e := unmarshalPoint(b); return Size{p.X, p.Y}, e }
func unmarshalRect(b []byte) (Rect, error) {
	var r Rect
	for o := 0; o < len(b); {
		t, n, e := readVarint(b, o)
		if e != nil {
			return r, e
		}
		o += n
		f := int(t >> 3)
		if t&7 != 5 || f < 1 || f > 4 || len(b)-o < 4 {
			return r, fmt.Errorf("%w: rect", ErrMalformed)
		}
		v := math.Float32frombits(binary.LittleEndian.Uint32(b[o:]))
		o += 4
		switch f {
		case 1:
			r.X = v
		case 2:
			r.Y = v
		case 3:
			r.Width = v
		case 4:
			r.Height = v
		}
	}
	return r, nil
}

func appendPackedInt16(dst []int16, b []byte) ([]int16, error) {
	return appendVarints(dst, b, func(x uint64) int16 { return int16(x) })
}
func appendPackedUint16(dst []uint16, b []byte) ([]uint16, error) {
	return appendVarints(dst, b, func(x uint64) uint16 { return uint16(x) })
}
func appendPackedInt32(dst []int32, b []byte) ([]int32, error) {
	return appendVarints(dst, b, func(x uint64) int32 { return int32(x) })
}
func appendPackedUint32(dst []uint32, b []byte) ([]uint32, error) {
	return appendVarints(dst, b, func(x uint64) uint32 { return uint32(x) })
}
func appendPackedInt64(dst []int64, b []byte) ([]int64, error) {
	return appendVarints(dst, b, func(x uint64) int64 { return int64(x) })
}
func appendPackedUint64(dst []uint64, b []byte) ([]uint64, error) {
	return appendVarints(dst, b, func(x uint64) uint64 { return x })
}
func appendVarints[T any](dst []T, b []byte, conv func(uint64) T) ([]T, error) {
	for o := 0; o < len(b); {
		x, n, e := readVarint(b, o)
		if e != nil {
			return nil, e
		}
		o += n
		dst = append(dst, conv(x))
		if len(dst) > maxArray {
			return nil, fmt.Errorf("%w: array limit", ErrMalformed)
		}
	}
	return dst, nil
}
func appendPackedFloat32Values(dst []float32, b []byte) ([]float32, error) {
	if len(b)%4 != 0 || len(b)/4 > maxArray-len(dst) {
		return nil, fmt.Errorf("%w: float array", ErrMalformed)
	}
	for o := 0; o < len(b); o += 4 {
		dst = append(dst, math.Float32frombits(binary.LittleEndian.Uint32(b[o:])))
	}
	return dst, nil
}
func appendPackedFloat64Values(dst []float64, b []byte) ([]float64, error) {
	if len(b)%8 != 0 || len(b)/8 > maxArray-len(dst) {
		return nil, fmt.Errorf("%w: double array", ErrMalformed)
	}
	for o := 0; o < len(b); o += 8 {
		dst = append(dst, math.Float64frombits(binary.LittleEndian.Uint64(b[o:])))
	}
	return dst, nil
}
func appendPackedBool(dst []bool, b []byte) ([]bool, error) {
	return appendVarints(dst, b, func(x uint64) bool { return x != 0 })
}
