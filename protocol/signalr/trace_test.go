package signalr

import "testing"

func TestNewTraceContextPacketShape(t *testing.T) {
	trace, err := NewTraceContextPacket()
	if err != nil {
		t.Fatal(err)
	}
	if trace.TraceID == nil || len(*trace.TraceID) != 32 {
		t.Fatalf("TraceID=%v", trace.TraceID)
	}
	if trace.ParentID == nil || len(*trace.ParentID) != 16 {
		t.Fatalf("ParentID=%v", trace.ParentID)
	}
	if trace.TraceState == nil || len(trace.TraceState) != 0 {
		t.Fatalf("TraceState=%#v", trace.TraceState)
	}
}

func TestNormalizeTraceContextPacketFillsRequiredFields(t *testing.T) {
	trace, err := NormalizeTraceContextPacket(TraceContextPacket{})
	if err != nil {
		t.Fatal(err)
	}
	if trace.TraceID == nil || len(*trace.TraceID) != 32 ||
		trace.ParentID == nil || len(*trace.ParentID) != 16 ||
		trace.TraceState == nil {
		t.Fatalf("trace=%#v", trace)
	}
}
