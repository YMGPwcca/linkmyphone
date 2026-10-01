package dcg

import (
	"bytes"
	"testing"
)

func TestFragmentPacketRoundTrip(t *testing.T) {
	f := Fragment{SequenceNumber: 3, FragmentNumber: 1, FragmentCount: 2, MessageID: 77, Payload: []byte("ab"), TransportMessageType: int(TransportMessageTypePlatform), SessionID: "sid"}
	got, err := ParseFragmentPacket(ToMultiplexPacket(f, int(MessageTypeFragment)))
	if err != nil {
		t.Fatal(err)
	}
	if got.SequenceNumber != 3 || got.MessageID != 77 || got.TransportMessageType != int(TransportMessageTypePlatform) || !bytes.Equal(got.Payload, []byte("ab")) {
		t.Fatalf("got=%#v", got)
	}
}

func TestSuccessAckPacket(t *testing.T) {
	p := SuccessAckPacket(Fragment{SequenceNumber: 9, SessionID: "s"})
	a, err := ParseAckPacket(p)
	if err != nil {
		t.Fatal(err)
	}
	if a.SequenceNumber != 9 || !a.Success || a.ErrorNumber != 0 || a.SessionID != "s" {
		t.Fatalf("ack=%#v", a)
	}
	if p.Raw != nil {
		t.Fatalf("raw should be nil")
	}
}

func TestReassembler(t *testing.T) {
	r := NewReassembler(1024, 10)
	a := Fragment{FragmentNumber: 2, FragmentCount: 2, MessageID: 1, Payload: []byte("world"), TransportMessageType: int(TransportMessageTypePlatform), SessionID: "s"}
	b := a
	b.FragmentNumber = 1
	b.Payload = []byte("hello ")
	if p, ok, err := r.Add("peer", a); err != nil || ok || p != nil {
		t.Fatalf("first p=%q ok=%v err=%v", p, ok, err)
	}
	p, ok, err := r.Add("peer", b)
	if err != nil || !ok || string(p) != "hello world" {
		t.Fatalf("p=%q ok=%v err=%v", p, ok, err)
	}
}

func TestHubRelayTransportMessageTypeWireValues(t *testing.T) {
	if got := int(TransportMessageTypeApp); got != 0 {
		t.Fatalf("App=%d want=0", got)
	}
	if got := int(TransportMessageTypePlatform); got != 1 {
		t.Fatalf("Platform=%d want=1", got)
	}
	if got := int(TransportMessageTypeUnknown); got != 2 {
		t.Fatalf("Unknown=%d want=2", got)
	}
}
