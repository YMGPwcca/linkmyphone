package dcg

import (
	"bytes"
	"testing"
)

func TestFragmentPayload(t *testing.T) {
	frags, err := FragmentPayload([]byte("abcdefgh"), 3, 9, "sid", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 3 {
		t.Fatalf("count=%d", len(frags))
	}
	want := [][]byte{[]byte("abc"), []byte("def"), []byte("gh")}
	for i, f := range frags {
		if f.FragmentNumber != i+1 || f.FragmentCount != 3 {
			t.Fatalf("fragment metadata=%#v", f)
		}
		if !bytes.Equal(f.Payload, want[i]) {
			t.Fatalf("payload[%d]=%q", i, f.Payload)
		}
	}
}

func TestEmptyPayloadStillHasOneFragment(t *testing.T) {
	frags, err := FragmentPayload(nil, 1024, 9, "sid", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 1 || frags[0].FragmentNumber != 1 || frags[0].FragmentCount != 1 {
		t.Fatalf("unexpected fragments: %#v", frags)
	}
}

func TestMultiplexPacket(t *testing.T) {
	f := Fragment{
		SequenceNumber:       12,
		FragmentNumber:       2,
		FragmentCount:        4,
		MessageID:            44,
		Payload:              []byte{1, 2},
		TransportMessageType: 9,
		SessionID:            "session",
	}
	p := ToMultiplexPacket(f, 3)
	if p.Type != "ms-dcg" {
		t.Fatalf("type=%q", p.Type)
	}
	if p.Properties[PropertyVersion] != float64(1) ||
		p.Properties[PropertyType] != 3 ||
		p.Properties[PropertySessionID] != "session" ||
		p.Properties[PropertySequenceNumber] != 12 ||
		p.Properties[PropertyMessageID] != 44 ||
		p.Properties[PropertyFragmentID] != 2 ||
		p.Properties[PropertyFragmentCount] != 4 ||
		p.Properties[PropertyMessageType] != 9 {
		t.Fatalf("properties=%#v", p.Properties)
	}
}

func TestSequencer(t *testing.T) {
	s := NewSequencer()
	if got := s.Next(); got != 1 {
		t.Fatalf("first=%d", got)
	}
	if got := s.Next(); got != 2 {
		t.Fatalf("second=%d", got)
	}
}
