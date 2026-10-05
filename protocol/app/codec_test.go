package app

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

// Generated from synthetic values by the original Windows 1.26072.257.0
// YourPhone.Connectivity.Protocol.dll, not by this package's encoder.
const windowsPayload = "0a0c0a06726573756c74120208040a2b0a0b7065726d697373696f6e73121c08119201170a150a0d6e6f74696669636174696f6e731204080b60010a130a046b657973120b081d8202008202036b65790a180a036f707312110815c2010c0102f9ffffffffffffffff01"

func TestOriginalWindowsPayloadPreservesTypedDefaultsAndArrayPositions(t *testing.T) {
	wire, err := hex.DecodeString(windowsPayload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	want := ValueSet{"result": int32(0), "permissions": ValueSet{"notifications": true}, "keys": []string{"", "key"}, "ops": []int32{1, 2, -7}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded Windows payload=%#v", got)
	}
}

func TestUnpackedArrayAndMalformedVariants(t *testing.T) {
	// Variant Int32Array, with legal unpacked values 1 and 2.
	wire, _ := hex.DecodeString("0a0f0a036f707312080815c00101c00102")
	values, err := Unmarshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values["ops"], []int32{1, 2}) {
		t.Fatalf("unpacked values=%#v", values)
	}
	for _, malformed := range []string{
		"0a0e0a036f707312070815c2010201", // truncated length
		"0a0b0a0178120608046a026869",     // Int32 carrying String data
		"0a800080808080808080808002",     // overflowing varint
	} {
		b, _ := hex.DecodeString(malformed)
		if _, err := Unmarshal(b); !errors.Is(err, ErrMalformed) {
			t.Fatalf("malformed %s error=%v", malformed, err)
		}
	}
}

func TestPackedFloatLimitBeforeAllocation(t *testing.T) {
	if _, err := appendPackedFloat32Values(nil, make([]byte, 4*(maxArray+1))); !errors.Is(err, ErrMalformed) {
		t.Fatalf("float array limit error=%v", err)
	}
}

func TestProto3ZeroPointIsAValidObject(t *testing.T) {
	// Empty nested Point is how proto3 serializes X=0, Y=0.
	wire, err := hex.DecodeString("0a0e0a066f726967696e1204080e7a00")
	if err != nil {
		t.Fatal(err)
	}
	values, err := Unmarshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if values["origin"] != (Point{}) {
		t.Fatalf("zero Point=%#v", values["origin"])
	}
}
func TestDecoderRejectsTruncatedNestedAndWrongVariantFields(t *testing.T) {
	var malformed []byte
	malformed = appendBytesField(nil, 1, []byte{0x0a})
	cases := [][]byte{
		{0x0a},                              // map entry length is truncated
		{0x0a, 0x01, 0x0a},                  // entry key length is truncated
		{0x0a, 0x02, 0x0a, 0x00},            // entry has no value
		{0x0a, 0x04, 0x0a, 0x01, 'x', 0x10}, // value uses a varint wire type
		malformed,                           // point message is truncated
	}
	for _, wire := range cases {
		if _, err := Unmarshal(wire); !errors.Is(err, ErrMalformed) {
			t.Fatalf("accepted malformed wire %x: %v", wire, err)
		}
	}

	wrongFields := []struct {
		name string
		body []byte
	}{
		{"int32 carrying string", variantWithField(tInt32, appendStringField(nil, 13, "wrong"))},
		{"string carrying int32", variantWithField(tString, appendVarintField(nil, 5, 7))},
		{"object carrying string", variantWithField(tObject, appendStringField(nil, 13, "wrong"))},
	}
	for _, tc := range wrongFields {
		wire := appMapEntry("x", tc.body)
		if _, err := Unmarshal(wire); !errors.Is(err, ErrMalformed) {
			t.Fatalf("accepted %s: %v", tc.name, err)
		}
	}
}

func TestDecoderEnforcesArrayAndRecursionLimits(t *testing.T) {
	if _, err := Marshal(ValueSet{"x": make([]int32, maxArray+1)}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("marshal accepted oversized array: %v", err)
	}

	packed := make([]byte, 0, maxArray+1)
	for range maxArray + 1 {
		packed = appendUvarint(packed, 1)
	}
	oversized := variantWithField(tInt32Array, appendBytesField(nil, 24, packed))
	if _, err := Unmarshal(appMapEntry("x", oversized)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("unmarshal accepted oversized packed array: %v", err)
	}

	if _, err := Marshal(nestedValueSet(maxDepth)); err != nil {
		t.Fatalf("marshal rejected maximum supported nesting: %v", err)
	}
	if _, err := Marshal(nestedValueSet(maxDepth + 1)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("marshal accepted excessive nesting: %v", err)
	}
	if _, err := Unmarshal(nestedObjectWire(maxDepth)); err != nil {
		t.Fatalf("unmarshal rejected maximum supported nesting: %v", err)
	}
	if _, err := Unmarshal(nestedObjectWire(maxDepth + 1)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("unmarshal accepted excessive nesting: %v", err)
	}
}

func TestDecoderPreservesEmptyTypedArrays(t *testing.T) {
	for _, typ := range []uint64{
		tUInt8Array, tInt16Array, tUInt16Array, tInt32Array, tUInt32Array,
		tInt64Array, tUInt64Array, tSingleArray, tDoubleArray, tChar16Array,
		tBoolArray, tStringArray, tGuidArray, tPointArray, tSizeArray,
		tRectArray, tObjectArray, tDateTimeArray, tTimeSpanArray,
	} {
		body := variantHeader(typ)
		values, err := Unmarshal(appMapEntry("empty", body))
		if err != nil {
			t.Fatalf("type %d empty array: %v", typ, err)
		}
		switch got := values["empty"].(type) {
		case []byte, Int16Array, UInt16Array, []int32, []uint32, []int64, []uint64,
			[]float32, []float64, Char16Array, []bool, []string, []Guid, []Point,
			[]Size, []Rect, []ValueSet, DateTimeArray, TimeSpanArray:
			if reflect.ValueOf(got).Len() != 0 {
				t.Fatalf("type %d: absent repeated field produced elements", typ)
			}
		default:
			t.Fatalf("type %d decoded unexpected value %T", typ, values["empty"])
		}
	}
}

func TestPeekStringSkipsForeignImageAndUsesLastContentType(t *testing.T) {
	foreign := variantWithField(tUInt8Array, appendBytesField(nil, 21, make([]byte, maxArray+1)))
	payload := appMapEntry("image_bytes", foreign)
	for _, kind := range []string{"notifications", "copypaste_metadata"} {
		value, err := Marshal(ValueSet{"contentType": kind})
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload, value...)
	}
	kind, found, err := PeekString(payload, "contentType")
	if err != nil || !found || kind != "copypaste_metadata" {
		t.Fatalf("peek kind=%q found=%t error=%v", kind, found, err)
	}
	if _, err := Unmarshal(payload); !errors.Is(err, ErrMalformed) {
		t.Fatalf("full decode accepted oversized binary field: %v", err)
	}
	if _, _, err := PeekString([]byte{0x0a}, "contentType"); !errors.Is(err, ErrMalformed) {
		t.Fatalf("truncated metadata accepted: %v", err)
	}
}

func FuzzUnmarshalBounded(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{0x0a, 0x00})
	f.Add([]byte{0x0a, 7, 0x0a, 1, 'x', 0x12, 2, 8, 23})
	f.Fuzz(func(t *testing.T, wire []byte) {
		if len(wire) > 65536 {
			return
		}
		_, _ = Unmarshal(wire)
	})
}

func variantHeader(typ uint64) []byte {
	body := appendTag(nil, 1, 0)
	return appendUvarint(body, typ)
}

func variantWithField(typ uint64, field []byte) []byte {
	body := variantHeader(typ)
	return append(body, field...)
}

func appendVarintField(dst []byte, field int, value uint64) []byte {
	dst = appendTag(dst, field, 0)
	return appendUvarint(dst, value)
}

func appMapEntry(key string, variant []byte) []byte {
	entry := appendStringField(nil, 1, key)
	entry = appendBytesField(entry, 2, variant)
	return appendBytesField(nil, 1, entry)
}

func nestedValueSet(layers int) ValueSet {
	values := ValueSet{}
	for range layers {
		values = ValueSet{"next": values}
	}
	return values
}

func nestedObjectWire(layers int) []byte {
	wire := []byte{}
	for range layers {
		variant := variantWithField(tObject, appendBytesField(nil, 18, wire))
		wire = appMapEntry("next", variant)
	}
	return wire
}
