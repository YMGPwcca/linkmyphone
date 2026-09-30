package signalr

import (
	"bytes"
	"testing"

	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
)

func TestMessagePackPrimitives(t *testing.T) {
	p := packer{}
	p.str("abc")
	p.bin([]byte{1, 2})
	p.integer(1)
	p.float64(1)
	wantPrefix := []byte{0xa3, 'a', 'b', 'c', 0xc4, 0x02, 0x01, 0x02, 0x01, 0xcb}
	if !bytes.Equal(p.b[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("wire=%x", p.b)
	}
}

func TestSendMessageAsyncShape(t *testing.T) {
	invocationID := "42"
	traceID := "trace"
	parentID := "parent"
	fragment := dcg.Fragment{
		SequenceNumber:       1,
		FragmentNumber:       1,
		FragmentCount:        1,
		MessageID:            5,
		Payload:              []byte{1, 2, 3},
		TransportMessageType: int(dcg.TransportMessageTypePlatform),
		SessionID:            "s",
	}
	packet := dcg.ToMultiplexPacket(fragment, int(dcg.MessageTypeFragment))
	body, err := MarshalSendMessageAsync(
		&invocationID,
		TraceContextPacket{
			ParentID:   &parentID,
			TraceFlags: 1,
			TraceID:    &traceID,
			TraceState: map[string]string{},
		},
		"target",
		packet,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 5 || body[0] != 0x96 || body[1] != 0x01 || body[2] != 0x80 {
		t.Fatalf("bad invocation prefix: %x", body[:min(8, len(body))])
	}
	if !bytes.Contains(body, []byte("SendMessageAsync")) ||
		!bytes.Contains(body, []byte("Properties")) ||
		!bytes.Contains(body, []byte("ms-dcg")) {
		t.Fatalf("missing invocation fields: %x", body)
	}
	framed, err := FrameSendMessageAsync(
		&invocationID,
		TraceContextPacket{
			ParentID:   &parentID,
			TraceFlags: 1,
			TraceID:    &traceID,
			TraceState: map[string]string{},
		},
		"target",
		packet,
	)
	if err != nil {
		t.Fatal(err)
	}
	n, used, err := DecodeLength(framed)
	if err != nil || n != len(body) || used+len(body) != len(framed) {
		t.Fatalf("frame n=%d used=%d err=%v", n, used, err)
	}
}
