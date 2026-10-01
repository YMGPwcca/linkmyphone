package bootstrap

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	sessionproto "github.com/YMGPwcca/phonelink-linux/protocol/sessionvalidation"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

type sessionFakeRelay struct {
	recv chan relay.Received

	mu   sync.Mutex
	sent []relay.Received
}

func newSessionFakeRelay() *sessionFakeRelay {
	return &sessionFakeRelay{recv: make(chan relay.Received, 8)}
}

func (f *sessionFakeRelay) Received() <-chan relay.Received { return f.recv }

func (f *sessionFakeRelay) Send(
	_ context.Context,
	target string,
	session string,
	transportType dcg.TransportMessageType,
	payload []byte,
) error {
	f.mu.Lock()
	f.sent = append(f.sent, relay.Received{
		Source:               target,
		SessionID:            session,
		TransportMessageType: transportType,
		Payload:              append([]byte(nil), payload...),
	})
	f.mu.Unlock()
	return nil
}

func TestValidatePlatformSessionWireAndResponse(t *testing.T) {
	fr := newSessionFakeRelay()
	type result struct {
		value SessionValidationResult
		err   error
	}
	done := make(chan result, 1)
	go func() {
		got, err := ValidatePlatformSession(context.Background(), fr, "phone", time.Second)
		done <- result{value: got, err: err}
	}()

	var sent relay.Received
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) != 0 {
			sent = fr.sent[0]
		}
		fr.mu.Unlock()
		if sent.Payload != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if sent.Payload == nil {
		t.Fatal("no SessionValidation request")
	}
	if sent.TransportMessageType != dcg.TransportMessageTypePlatform || sent.Source != "phone" {
		t.Fatalf("sent=%#v", sent)
	}

	request, err := platform.Unmarshal(sent.Payload)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := request.Header(platform.HeaderRoute)
	requestID, _ := request.Header(platform.HeaderRequestID)
	if route != platform.RouteSessionValidation || requestID == "" {
		t.Fatalf("route=%q requestID=%q", route, requestID)
	}
	if got := request.Payload; len(got) != 3 || got[0] != 0x0a || got[1] != 0x01 || got[2] != 0x01 {
		t.Fatalf("request payload=%x", got)
	}

	responsePayload := sessionproto.MarshalResponse(sessionproto.Response{
		Capabilities: []sessionproto.Capability{
			sessionproto.CapabilitySessionValidation,
			sessionproto.CapabilityPersistentMessageChannel,
		},
		PersistentMessagingChannelVersion: 14,
	})
	response := platform.NewInternalResponse(responsePayload, requestID)
	wire, err := platform.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		SessionID:            "dcg-session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              wire,
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.value.SessionID != "dcg-session" ||
			!got.value.Response.Has(sessionproto.CapabilitySessionValidation) ||
			!got.value.Response.Has(sessionproto.CapabilityPersistentMessageChannel) ||
			got.value.Response.PersistentMessagingChannelVersion != 14 {
			t.Fatalf("got=%#v", got.value)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestValidatePlatformSessionReportsPeerRejection(t *testing.T) {
	fr := newSessionFakeRelay()
	done := make(chan error, 1)
	go func() {
		_, err := ValidatePlatformSession(context.Background(), fr, "phone", time.Second)
		done <- err
	}()

	var requestID string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) != 0 {
			request, err := platform.Unmarshal(fr.sent[0].Payload)
			if err != nil {
				fr.mu.Unlock()
				t.Fatal(err)
			}
			requestID, _ = request.Header(platform.HeaderRequestID)
		}
		fr.mu.Unlock()
		if requestID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if requestID == "" {
		t.Fatal("no request id")
	}

	response := platform.NewInternalResponse([]byte{1}, requestID)
	response.Headers = append(response.Headers, platform.Header{
		Key:   platform.HeaderRejectedReason,
		Value: "NotSubscribedToRoute",
	})
	wire, err := platform.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              wire,
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected rejection error")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}
