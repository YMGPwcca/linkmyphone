package relay

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	psignalr "github.com/YMGPwcca/phonelink-linux/protocol/signalr"
)

type fakeHub struct {
	mu     sync.Mutex
	sent   [][]byte
	reads  chan []byte
	closed chan struct{}
}

func newFakeHub() *fakeHub {
	return &fakeHub{reads: make(chan []byte, 8), closed: make(chan struct{})}
}

func (h *fakeHub) SendBinary(b []byte) error {
	h.mu.Lock()
	h.sent = append(h.sent, append([]byte(nil), b...))
	h.mu.Unlock()
	return nil
}

func (h *fakeHub) ReadBinary() ([]byte, error) {
	select {
	case b := <-h.reads:
		return b, nil
	case <-h.closed:
		return nil, errors.New("closed")
	}
}

func (h *fakeHub) Close() error {
	close(h.closed)
	return nil
}

func TestReceiveFragmentAcksAndReassembles(t *testing.T) {
	hub := newFakeHub()
	c := New(hub, Config{FragmentSize: 1024, AckTimeout: time.Second, AckRetries: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	packet := dcg.ToMultiplexPacket(dcg.Fragment{
		SequenceNumber: 3, FragmentNumber: 1, FragmentCount: 1,
		MessageID: 7, Payload: []byte("platform"), TransportMessageType: int(dcg.TransportMessageTypePlatform), SessionID: "s",
	}, int(dcg.MessageTypeFragment))
	hub.reads <- onReceiveFrame(t, "phone", packet)

	select {
	case got := <-c.Received():
		if got.Source != "phone" || got.SessionID != "s" || string(got.Payload) != "platform" {
			t.Fatalf("got=%#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reassembled message")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		n := len(hub.sent)
		hub.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no ACK sent")
}

func TestSendCompletesOnAck(t *testing.T) {
	hub := newFakeHub()
	c := New(hub, Config{FragmentSize: 1024, AckTimeout: time.Second, AckRetries: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	done := make(chan error, 1)
	go func() {
		done <- c.Send(ctx, "phone", "session", dcg.TransportMessageTypePlatform, []byte("hello"))
	}()

	var sent []byte
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		if len(hub.sent) > 0 {
			sent = append([]byte(nil), hub.sent[0]...)
		}
		hub.mu.Unlock()
		if sent != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if sent == nil {
		t.Fatal("no fragment sent")
	}

	frames, err := psignalr.SplitFrames(sent)
	if err != nil || len(frames) != 1 {
		t.Fatalf("frames=%d err=%v", len(frames), err)
	}
	inv, err := psignalr.ParseInvocation(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if inv.Target != psignalr.TargetSendMessageAsync || len(inv.Arguments) != 3 {
		t.Fatalf("unexpected invocation: %#v", inv)
	}
	assertValidHubTrace(t, inv.Arguments[0])
	packetMap, ok := inv.Arguments[2].(map[string]any)
	if !ok {
		t.Fatal("missing packet map")
	}
	packet, err := packetFromMapForTest(packetMap)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := dcg.ParseFragmentPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if fragment.TransportMessageType != int(dcg.TransportMessageTypePlatform) {
		t.Fatalf("Hub Relay MessageType=%d want Platform=%d", fragment.TransportMessageType, dcg.TransportMessageTypePlatform)
	}
	raw, ok := packet.Properties[dcg.PropertyMessageType]
	if !ok {
		t.Fatal("wire MessageType missing")
	}
	wireMessageType, ok := raw.(int64)
	if !ok || wireMessageType != int64(dcg.TransportMessageTypePlatform) {
		t.Fatalf("wire MessageType=%#v (%T) want=%d", raw, raw, dcg.TransportMessageTypePlatform)
	}
	hub.reads <- onReceiveFrame(t, "phone", dcg.SuccessAckPacket(fragment))

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("send did not complete")
	}
}

func onReceiveFrame(t *testing.T, source string, packet dcg.MultiplexPacket) []byte {
	t.Helper()
	body := buildOnReceive(t, source, packet)
	framed, err := psignalr.Frame(body)
	if err != nil {
		t.Fatal(err)
	}
	return framed
}

func buildOnReceive(t *testing.T, source string, packet dcg.MultiplexPacket) []byte {
	t.Helper()
	enc := &tinyPacker{}
	enc.array(6)
	enc.integer(1)
	enc.mapLen(0)
	enc.nilValue()
	enc.str("OnReceiveMessage")
	enc.array(3)
	enc.str(source)
	enc.mapLen(4)
	enc.str("ParentId")
	enc.nilValue()
	enc.str("TraceFlags")
	enc.integer(0)
	enc.str("TraceId")
	enc.nilValue()
	enc.str("TraceState")
	enc.nilValue()
	enc.packet(packet)
	enc.array(0)
	return enc.b
}

func packetFromMapForTest(m map[string]any) (dcg.MultiplexPacket, error) {
	p := dcg.MultiplexPacket{}
	p.Type, _ = m["Type"].(string)
	p.Properties, _ = m["Properties"].(map[string]any)
	if raw, ok := m["Raw"].([]byte); ok {
		p.Raw = raw
	}
	if p.Type == "" || p.Properties == nil {
		return p, errors.New("bad packet")
	}
	return p, nil
}

type tinyPacker struct{ b []byte }

func (p *tinyPacker) array(n int)  { p.b = append(p.b, 0x90|byte(n)) }
func (p *tinyPacker) mapLen(n int) { p.b = append(p.b, 0x80|byte(n)) }
func (p *tinyPacker) nilValue()    { p.b = append(p.b, 0xc0) }

func (p *tinyPacker) integer(n int) {
	if n >= 0 && n < 128 {
		p.b = append(p.b, byte(n))
		return
	}
	p.b = append(p.b, 0xd2, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func (p *tinyPacker) str(s string) {
	if len(s) < 32 {
		p.b = append(p.b, 0xa0|byte(len(s)))
	} else {
		p.b = append(p.b, 0xd9, byte(len(s)))
	}
	p.b = append(p.b, s...)
}

func (p *tinyPacker) bin(b []byte) {
	p.b = append(p.b, 0xc4, byte(len(b)))
	p.b = append(p.b, b...)
}

func (p *tinyPacker) float64one() {
	p.b = append(p.b, 0xcb, 0x3f, 0xf0, 0, 0, 0, 0, 0, 0)
}

func (p *tinyPacker) packet(v dcg.MultiplexPacket) {
	p.mapLen(3)
	p.str("Properties")
	p.mapLen(len(v.Properties))
	keys := []string{
		dcg.PropertyVersion, dcg.PropertyType, dcg.PropertySessionID,
		dcg.PropertySequenceNumber, dcg.PropertySuccess, dcg.PropertyErrorNumber,
		dcg.PropertyMessageID, dcg.PropertyFragmentID, dcg.PropertyFragmentCount,
		dcg.PropertyMessageType,
	}
	seen := map[string]bool{}
	for _, k := range keys {
		x, ok := v.Properties[k]
		if !ok {
			continue
		}
		seen[k] = true
		p.str(k)
		switch y := x.(type) {
		case string:
			p.str(y)
		case int:
			p.integer(y)
		case float64:
			if y == 1 {
				p.float64one()
			} else {
				p.b = append(p.b, 0xcb, 0, 0, 0, 0, 0, 0, 0, 0)
			}
		case bool:
			if y {
				p.b = append(p.b, 0xc3)
			} else {
				p.b = append(p.b, 0xc2)
			}
		}
	}
	for k, x := range v.Properties {
		if seen[k] {
			continue
		}
		p.str(k)
		switch y := x.(type) {
		case string:
			p.str(y)
		case int:
			p.integer(y)
		}
	}
	p.str("Raw")
	if v.Raw == nil {
		p.nilValue()
	} else {
		p.bin(v.Raw)
	}
	p.str("Type")
	p.str(v.Type)
}

func TestSessionIDForTargetIsStableAndPerPeer(t *testing.T) {
	c := New(newFakeHub(), Config{})

	first, err := c.sessionIDForTarget("phone-a")
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.sessionIDForTarget("phone-a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := c.sessionIDForTarget("phone-b")
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != again {
		t.Fatalf("unstable session id: first=%q again=%q", first, again)
	}
	if other == "" || other == first {
		t.Fatalf("expected distinct peer session id: first=%q other=%q", first, other)
	}
}

func TestSendConnectedAndPartnerPresence(t *testing.T) {
	hub := newFakeHub()
	c := New(hub, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	if err := c.SendConnected("phone", psignalr.TraceContextPacket{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	var first []byte
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		if len(hub.sent) != 0 {
			first = append([]byte(nil), hub.sent[0]...)
		}
		hub.mu.Unlock()
		if first != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if first == nil {
		t.Fatal("no SendConnectedAsync invocation")
	}
	frames, err := psignalr.SplitFrames(first)
	if err != nil || len(frames) != 1 {
		t.Fatalf("frames=%d err=%v", len(frames), err)
	}
	inv, err := psignalr.ParseInvocation(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if inv.Target != psignalr.TargetSendConnectedAsync {
		t.Fatalf("target=%q", inv.Target)
	}
	assertValidHubTrace(t, inv.Arguments[0])

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- c.WaitPartnerConnected(ctx, "phone")
	}()
	hub.reads <- onPartnerConnectedFrame(t, "phone", "westus")
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("partner presence did not unblock waiter")
	}
	if !c.PartnerConnected("phone") {
		t.Fatal("partner should be connected")
	}
}

func onPartnerConnectedFrame(t *testing.T, source, region string) []byte {
	t.Helper()
	p := &tinyPacker{}
	p.array(6)
	p.integer(1)
	p.mapLen(0)
	p.nilValue()
	p.str(psignalr.TargetOnPartnerConnected)
	p.array(3)
	p.str(source)
	p.mapLen(4)
	p.str("ParentId")
	p.nilValue()
	p.str("TraceFlags")
	p.integer(0)
	p.str("TraceId")
	p.nilValue()
	p.str("TraceState")
	p.nilValue()
	p.str(region)
	p.array(0)
	frame, err := psignalr.Frame(p.b)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func TestWaitHubConnectedReturnsRegionAndPartners(t *testing.T) {
	hub := newFakeHub()
	c := New(hub, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	waitDone := make(chan psignalr.OnConnectedPayload, 1)
	errDone := make(chan error, 1)
	go func() {
		payload, err := c.WaitHubConnected(ctx)
		if err != nil {
			errDone <- err
			return
		}
		waitDone <- payload
	}()

	hub.reads <- onConnectedFrame(t, "westus", []string{"phone-a", "phone-b"})

	select {
	case err := <-errDone:
		t.Fatal(err)
	case payload := <-waitDone:
		if payload.RegionName != "westus" ||
			len(payload.Partners) != 2 ||
			payload.Partners[0] != "phone-a" ||
			payload.Partners[1] != "phone-b" {
			t.Fatalf("payload=%#v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnConnected")
	}

	cached, err := c.WaitHubConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cached.RegionName != "westus" || len(cached.Partners) != 2 {
		t.Fatalf("cached=%#v", cached)
	}
}

func onConnectedFrame(t *testing.T, region string, partners []string) []byte {
	t.Helper()
	p := &tinyPacker{}
	p.array(6)
	p.integer(1)
	p.mapLen(0)
	p.nilValue()
	p.str(psignalr.TargetOnConnected)
	p.array(1)
	p.mapLen(2)
	p.str("RegionName")
	p.str(region)
	p.str("Partners")
	p.array(len(partners))
	for _, partner := range partners {
		p.str(partner)
	}
	p.array(0)
	frame, err := psignalr.Frame(p.b)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func TestSendReportsHubCompletionError(t *testing.T) {
	hub := newFakeHub()
	c := New(hub, Config{FragmentSize: 1024, AckTimeout: time.Second, AckRetries: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	done := make(chan error, 1)
	go func() {
		done <- c.Send(ctx, "phone", "session", dcg.TransportMessageTypePlatform, []byte("hello"))
	}()

	var sent []byte
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		if len(hub.sent) > 0 {
			sent = append([]byte(nil), hub.sent[0]...)
		}
		hub.mu.Unlock()
		if sent != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if sent == nil {
		t.Fatal("no fragment sent")
	}

	frames, err := psignalr.SplitFrames(sent)
	if err != nil || len(frames) != 1 {
		t.Fatalf("frames=%d err=%v", len(frames), err)
	}
	inv, err := psignalr.ParseInvocation(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if inv.InvocationID == nil || *inv.InvocationID == "" {
		t.Fatal("missing invocation id")
	}

	hub.reads <- completionErrorFrame(t, *inv.InvocationID, "target unavailable")

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "Hub Relay rejected SendMessageAsync: target unavailable") {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("send did not report Hub completion error")
	}
}

func completionErrorFrame(t *testing.T, invocationID, message string) []byte {
	t.Helper()
	p := &tinyPacker{}
	p.array(5)
	p.integer(psignalr.HubMessageTypeCompletion)
	p.mapLen(0)
	p.str(invocationID)
	p.integer(psignalr.CompletionResultError)
	p.str(message)
	frame, err := psignalr.Frame(p.b)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func TestFlushPartnerWaitsForHubCompletion(t *testing.T) {
	hub := newFakeHub()
	c := New(hub, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	done := make(chan error, 1)
	go func() {
		flushCtx, flushCancel := context.WithTimeout(ctx, time.Second)
		defer flushCancel()
		done <- c.FlushPartner(flushCtx, "phone", psignalr.TraceContextPacket{})
	}()

	var sent []byte
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		if len(hub.sent) > 0 {
			sent = append([]byte(nil), hub.sent[0]...)
		}
		hub.mu.Unlock()
		if sent != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if sent == nil {
		t.Fatal("no SendConnectedAsync invocation")
	}
	frames, err := psignalr.SplitFrames(sent)
	if err != nil || len(frames) != 1 {
		t.Fatalf("frames=%d err=%v", len(frames), err)
	}
	inv, err := psignalr.ParseInvocation(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if inv.Target != psignalr.TargetSendConnectedAsync || inv.InvocationID == nil {
		t.Fatalf("invocation=%#v", inv)
	}
	hub.reads <- completionVoidFrame(t, *inv.InvocationID)

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("FlushPartner did not complete")
	}
}

func completionVoidFrame(t *testing.T, invocationID string) []byte {
	t.Helper()
	p := &tinyPacker{}
	p.array(4)
	p.integer(psignalr.HubMessageTypeCompletion)
	p.mapLen(0)
	p.str(invocationID)
	p.integer(psignalr.CompletionResultVoid)
	frame, err := psignalr.Frame(p.b)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func assertValidHubTrace(t *testing.T, raw any) {
	t.Helper()
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("trace=%#v (%T)", raw, raw)
	}
	traceID, ok := m["TraceId"].(string)
	if !ok || len(traceID) != 32 {
		t.Fatalf("TraceId=%#v", m["TraceId"])
	}
	parentID, ok := m["ParentId"].(string)
	if !ok || len(parentID) != 16 {
		t.Fatalf("ParentId=%#v", m["ParentId"])
	}
	if _, ok := m["TraceState"].(map[string]any); !ok {
		t.Fatalf("TraceState=%#v (%T)", m["TraceState"], m["TraceState"])
	}
}
