package signalr

import (
	"testing"

	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
)

func TestDecodeOwnSendInvocation(t *testing.T) {
	id := "7"
	traceID := "t"
	parentID := "p"
	f := dcg.Fragment{
		SequenceNumber:       1,
		FragmentNumber:       1,
		FragmentCount:        1,
		MessageID:            5,
		Payload:              []byte{1},
		TransportMessageType: int(dcg.TransportMessageTypePlatform),
		SessionID:            "s",
	}
	body, err := MarshalSendMessageAsync(
		&id,
		TraceContextPacket{
			ParentID:   &parentID,
			TraceFlags: 1,
			TraceID:    &traceID,
			TraceState: map[string]string{"k": "v"},
		},
		"target",
		dcg.ToMultiplexPacket(f, int(dcg.MessageTypeFragment)),
	)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := ParseInvocation(body)
	if err != nil {
		t.Fatal(err)
	}
	if inv.MessageType != 1 ||
		inv.Target != "SendMessageAsync" ||
		inv.InvocationID == nil ||
		*inv.InvocationID != "7" ||
		len(inv.Arguments) != 3 {
		t.Fatalf("inv=%#v", inv)
	}
}

func TestParseOnReceiveMessage(t *testing.T) {
	p := packer{}
	p.array(6)
	p.integer(1)
	p.mapLen(0)
	p.b = append(p.b, 0xc0)
	p.str("OnReceiveMessage")
	p.array(3)
	p.str("source")
	p.trace(TraceContextPacket{TraceFlags: 1, TraceState: map[string]string{}})
	packet := dcg.ToMultiplexPacket(
		dcg.Fragment{
			SequenceNumber:       9,
			FragmentNumber:       1,
			FragmentCount:        1,
			MessageID:            5,
			Payload:              []byte{4, 5},
			TransportMessageType: int(dcg.TransportMessageTypePlatform),
			SessionID:            "s",
		},
		int(dcg.MessageTypeFragment),
	)
	if err := p.multiplexPacket(packet); err != nil {
		t.Fatal(err)
	}
	p.array(0)

	inv, err := ParseInvocation(p.b)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := ParseOnReceiveMessage(inv)
	if err != nil {
		t.Fatal(err)
	}
	if msg.SourceDcgClientID != "source" ||
		msg.Packet.Type != "ms-dcg" ||
		string(msg.Packet.Raw) != string([]byte{4, 5}) {
		t.Fatalf("msg=%#v", msg)
	}
}

func TestSplitFrames(t *testing.T) {
	a, _ := Frame([]byte{1, 2})
	b, _ := Frame([]byte{3})
	all := append(a, b...)
	frames, err := SplitFrames(all)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || len(frames[0]) != 2 || frames[1][0] != 3 {
		t.Fatalf("frames=%v", frames)
	}
}

func TestParseOnReceiveSessionBasedMessage(t *testing.T) {
	p := packer{}
	p.array(6)
	p.integer(1)
	p.mapLen(0)
	p.b = append(p.b, 0xc0)
	p.str(TargetOnReceiveSessionBasedMessage)
	p.array(4)
	p.str("source")
	p.trace(TraceContextPacket{})
	packet := dcg.ToMultiplexPacket(
		dcg.Fragment{
			SequenceNumber: 1, FragmentNumber: 1, FragmentCount: 1,
			MessageID: 3, Payload: []byte{8},
			TransportMessageType: int(dcg.TransportMessageTypePlatform),
			SessionID: "session-2",
		},
		int(dcg.MessageTypeFragment),
	)
	if err := p.multiplexPacket(packet); err != nil {
		t.Fatal(err)
	}
	p.str("session-2")
	p.array(0)

	inv, err := ParseInvocation(p.b)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := ParseOnReceiveSessionBasedMessage(inv)
	if err != nil {
		t.Fatal(err)
	}
	if msg.SourceDcgClientID != "source" || msg.ConnectionSessionID != "session-2" {
		t.Fatalf("msg=%#v", msg)
	}
}
