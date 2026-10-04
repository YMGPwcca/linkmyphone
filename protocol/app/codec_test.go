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
