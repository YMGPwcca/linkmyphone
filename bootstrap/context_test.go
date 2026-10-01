package bootstrap

import (
	"context"
	"testing"
	"time"

	clipproto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/msaep"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

func TestProbeClipboardContextPublishObservesClipboardDRMRequest(t *testing.T) {
	fr := newSessionFakeRelay()
	type result struct {
		value ContextProbeResult
		err   error
	}
	done := make(chan result, 1)
	go func() {
		got, err := ProbeClipboardContextPublish(
			context.Background(),
			fr,
			"phone",
			"desktop",
			time.Second,
		)
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
		t.Fatal("no Context/Publish probe")
	}
	if sent.Source != "phone" || sent.TransportMessageType != dcg.TransportMessageTypePlatform {
		t.Fatalf("sent=%#v", sent)
	}

	pm, err := platform.Unmarshal(sent.Payload)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := pm.Header(platform.HeaderRoute)
	if route != platform.RouteContextPublish {
		t.Fatalf("route=%q", route)
	}
	envelope, err := msaep.Unmarshal(pm.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.DcgClientID != "desktop" ||
		envelope.MessageTag != int32(clipproto.ClipboardMessageTag) {
		t.Fatalf("envelope=%#v", envelope)
	}
	pubsub, err := clipproto.UnmarshalPubSubPayload(envelope.Payload)
	if err != nil {
		t.Fatal(err)
	}
	change, err := clipproto.UnmarshalResponse(pubsub.Data)
	if err != nil {
		t.Fatal(err)
	}
	if change.Status != clipproto.ResponseClipboardChange || change.CorrelationID == "" {
		t.Fatalf("change=%#v", change)
	}

	drmPayload := clipproto.MarshalDeviceResourceMessage(
		clipproto.WrapClipboardRequest(
			clipproto.NewContentRequest(change.CorrelationID),
		),
	)
	request := platform.NewDeviceResourceRequest(drmPayload, "phone-request")
	requestWire, err := platform.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		SessionID:            "peer-session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              requestWire,
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.value.Route != platform.RouteDeviceResourceManager ||
			got.value.ResourcePath != clipproto.ResourcePath ||
			got.value.DeviceResourceRequestType != clipproto.DeviceResourceRequestGET ||
			got.value.ClipboardRequestType != clipproto.RequestContent ||
			got.value.ClipboardCorrelationID != change.CorrelationID {
			t.Fatalf("result=%#v", got.value)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	var reply relay.Received
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) >= 2 {
			reply = fr.sent[1]
		}
		fr.mu.Unlock()
		if reply.Payload != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if reply.Payload == nil {
		t.Fatal("no DRM probe response")
	}
	replyMessage, err := platform.Unmarshal(reply.Payload)
	if err != nil {
		t.Fatal(err)
	}
	replyRoute, _ := replyMessage.Header(platform.HeaderRoute)
	originalRequestID, _ := replyMessage.Header(platform.HeaderOriginalRequestID)
	if replyRoute != platform.RouteInternalResponse || originalRequestID != "phone-request" {
		t.Fatalf("route=%q original=%q", replyRoute, originalRequestID)
	}
	drmResponse, err := clipproto.UnmarshalDeviceResourceResponse(replyMessage.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if drmResponse.ResponseType != clipproto.DeviceResourceResponseResourceHandlerNotRegistered {
		t.Fatalf("response=%#v", drmResponse)
	}
}

func TestProbeClipboardContextPublishReportsPeerRejection(t *testing.T) {
	fr := newSessionFakeRelay()
	done := make(chan ContextProbeResult, 1)
	errDone := make(chan error, 1)
	go func() {
		got, err := ProbeClipboardContextPublish(
			context.Background(),
			fr,
			"phone",
			"desktop",
			time.Second,
		)
		if err != nil {
			errDone <- err
			return
		}
		done <- got
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
		t.Fatal("no Context/Publish request id")
	}

	response := platform.NewInternalResponse(nil, requestID)
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
	case err := <-errDone:
		t.Fatal(err)
	case got := <-done:
		if got.Route != platform.RouteInternalResponse ||
			got.RejectedReason != "NotSubscribedToRoute" {
			t.Fatalf("result=%#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestProbeClipboardContextPublishTimeoutIsObservation(t *testing.T) {
	fr := newSessionFakeRelay()
	got, err := ProbeClipboardContextPublish(
		context.Background(),
		fr,
		"phone",
		"desktop",
		5*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestID == "" || got.CorrelationID == "" || got.Route != "" {
		t.Fatalf("result=%#v", got)
	}
}
