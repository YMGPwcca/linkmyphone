package platform

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMarshalLayout(t *testing.T) {
	m := Message{
		Version: Version1,
		Headers: []Header{{Key: "a", Value: "bc"}},
		Payload: []byte{0xde, 0xad},
	}
	wire, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	if wire[0] != 1 {
		t.Fatalf("version=%d", wire[0])
	}
	if got := binary.LittleEndian.Uint32(wire[1:5]); got != 11 {
		t.Fatalf("header byte length=%d", got)
	}
	if got := binary.LittleEndian.Uint32(wire[5:9]); got != 1 {
		t.Fatalf("header count=%d", got)
	}

	got, err := Unmarshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != Version1 || len(got.Headers) != 1 ||
		got.Headers[0] != (Header{Key: "a", Value: "bc"}) ||
		!bytes.Equal(got.Payload, []byte{0xde, 0xad}) {
		t.Fatalf("round trip mismatch: %#v", got)
	}
}

func TestDeviceResourceRequestHeaders(t *testing.T) {
	m := NewDeviceResourceRequest([]byte{1, 2, 3}, "request-1")
	tests := map[string]string{
		HeaderContentType:      ContentTypeBinary,
		HeaderRoute:            RouteDeviceResourceManager,
		HeaderRejectionVersion: "1",
		HeaderRequestID:        "request-1",
	}
	for key, want := range tests {
		got, ok := m.Header(key)
		if !ok || got != want {
			t.Fatalf("%s=(%q,%v), want %q", key, got, ok, want)
		}
	}
}

func TestRejectHeaderCountMismatch(t *testing.T) {
	wire, err := Marshal(Message{
		Version: Version1,
		Headers: []Header{{Key: "a", Value: "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(wire[5:9], 2)
	if _, err := Unmarshal(wire); err == nil {
		t.Fatal("expected malformed message")
	}
}

func TestRejectTrailingBytes(t *testing.T) {
	wire, err := Marshal(Message{Version: Version1, Payload: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	wire = append(wire, 0)
	if _, err := Unmarshal(wire); err == nil {
		t.Fatal("expected trailing byte error")
	}
}
