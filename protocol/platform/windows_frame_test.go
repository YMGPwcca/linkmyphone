package platform

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestUnmarshalWindowsHeaderLength(t *testing.T) {
	// Windows writes header count inside the advertised header section length.
	// The fixture is independent of this package's Marshal implementation.
	wire, err := hex.DecodeString("010f0000000100000001000000610200000062630200000000000000dead")
	if err != nil {
		t.Fatal(err)
	}
	message, err := Unmarshal(wire)
	if err != nil {
		t.Fatalf("decode Windows frame: %v", err)
	}
	if len(message.Headers) != 1 || message.Headers[0] != (Header{Key: "a", Value: "bc"}) || !bytes.Equal(message.Payload, []byte{0xde, 0xad}) {
		t.Fatalf("Windows frame decoded incorrectly: %#v", message)
	}
}

func TestUnmarshalRejectsInvalidLengthAndCount(t *testing.T) {
	fixture, err := hex.DecodeString("010f0000000100000001000000610200000062630200000000000000dead")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name string
		at   int
		data []byte
	}{
		{"unsupported header length", 1, []byte{14, 0, 0, 0}},
		{"unbounded header count", 5, []byte{255, 255, 255, 255}},
		{"truncated string length", 14, []byte{255, 255, 255, 255}},
		{"payload overrun", 20, []byte{3, 0, 0, 0, 0, 0, 0, 0}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			wire := append([]byte(nil), fixture...)
			copy(wire[mutation.at:], mutation.data)
			if _, err := Unmarshal(wire); err == nil {
				t.Fatal("malformed frame accepted")
			}
		})
	}
}
