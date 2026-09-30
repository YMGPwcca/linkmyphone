package msaep

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestWindowsSchemaWireLayout(t *testing.T) {
	m := New("m", "d", 9, []byte{0xaa})
	wire := Marshal(m)

	const wantHex = "08091201641a016d2201aa28013001"
	if got := hex.EncodeToString(wire); got != wantHex {
		t.Fatalf("wire=%s want=%s", got, wantHex)
	}

	got, err := Unmarshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("got=%#v want=%#v", got, m)
	}
}

func TestNewUsesPlatformVersion11(t *testing.T) {
	m := New("message-id", "dcg-id", 9, []byte("payload"))
	if m.PlatformMajorVersion != 1 || m.PlatformMinorVersion != 1 {
		t.Fatalf("version=%d.%d", m.PlatformMajorVersion, m.PlatformMinorVersion)
	}
	if !bytes.Equal(m.Payload, []byte("payload")) {
		t.Fatalf("payload=%x", m.Payload)
	}
}

func TestUnknownFieldIsSkipped(t *testing.T) {
	wire := Marshal(New("m", "d", 9, []byte{1}))
	wire = append(wire, 0x98, 0x06, 0x01) // field 99, varint 1

	got, err := Unmarshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageTag != 9 || got.MessageID != "m" || got.DcgClientID != "d" {
		t.Fatalf("got=%#v", got)
	}
}
