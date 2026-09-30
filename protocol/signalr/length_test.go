package signalr

import (
	"bytes"
	"testing"
)

func TestLengthExamples(t *testing.T) {
	tests := []struct {
		n    int
		want []byte
	}{
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{300, []byte{0xac, 0x02}},
		{16384, []byte{0x80, 0x80, 0x01}},
	}
	for _, tt := range tests {
		got, err := EncodeLength(tt.n)
		if err != nil {
			t.Fatalf("%d: %v", tt.n, err)
		}
		if !bytes.Equal(got, tt.want) {
			t.Fatalf("%d: %x want %x", tt.n, got, tt.want)
		}
		n, used, err := DecodeLength(got)
		if err != nil || n != tt.n || used != len(got) {
			t.Fatalf("%d: decoded n=%d used=%d err=%v", tt.n, n, used, err)
		}
	}
}

func TestFrame(t *testing.T) {
	got, err := Frame(bytes.Repeat([]byte{0xaa}, 128))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 130 || got[0] != 0x80 || got[1] != 0x01 {
		t.Fatalf("frame prefix=%x len=%d", got[:2], len(got))
	}
}

func TestRejectTooLargeFifthByte(t *testing.T) {
	if _, _, err := DecodeLength([]byte{0xff, 0xff, 0xff, 0xff, 0x08}); err == nil {
		t.Fatal("expected error")
	}
}
