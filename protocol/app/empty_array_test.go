package app

import (
	"bytes"
	"testing"
)

func TestEmptyNumericArraysUseAndroidCompatibleTypedDefaults(t *testing.T) {
	// Java lite's stream parser attempts one element for a present zero-length
	// packed Int64 field. Omit empty repeated fields, as generated serializers do,
	// but preserve DataVariant's type discriminator for reconcile postTimes.
	cases := []struct {
		value any
		typ   byte
	}{
		{[]int32{}, 21}, {[]int64{}, 23}, {[]uint32{}, 22}, {[]uint64{}, 24},
		{[]float32{}, 25}, {[]float64{}, 26}, {[]bool{}, 28},
	}
	for _, tc := range cases {
		wire, err := Marshal(ValueSet{"x": tc.value})
		if err != nil {
			t.Fatal(err)
		}
		want := []byte{0x0a, 7, 0x0a, 1, 'x', 0x12, 2, 8, tc.typ}
		if !bytes.Equal(wire, want) {
			t.Fatalf("%T: noncanonical empty packed field: %x; want %x", tc.value, wire, want)
		}
	}
}
