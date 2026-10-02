package bootstrap

import (
	"context"
	"testing"
	"time"

	clipproto "github.com/YMGPwcca/linkmyphone/protocol/clipboard"
	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	"github.com/YMGPwcca/linkmyphone/protocol/msaep"
	"github.com/YMGPwcca/linkmyphone/protocol/platform"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
)

func TestProbeClipboardContextPublishContinuesFromStatusToContent(t *testing.T) {
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

	statusPayload := clipproto.MarshalDeviceResourceMessage(
		clipproto.WrapClipboardRequest(
			clipproto.NewStatusRequest(change.CorrelationID),
		),
	)
	statusRequest := platform.NewDeviceResourceRequest(statusPayload, "phone-status")
	statusWire, err := platform.Marshal(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		SessionID:            "peer-session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              statusWire,
	}

	var statusReply relay.Received
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) >= 2 {
			statusReply = fr.sent[1]
		}
		fr.mu.Unlock()
		if statusReply.Payload != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if statusReply.Payload == nil {
		t.Fatal("no STATUS response")
	}
	statusMessage, err := platform.Unmarshal(statusReply.Payload)
	if err != nil {
		t.Fatal(err)
	}
	statusRoute, _ := statusMessage.Header(platform.HeaderRoute)
	statusOriginal, _ := statusMessage.Header(platform.HeaderOriginalRequestID)
	if statusRoute != platform.RouteInternalResponse || statusOriginal != "phone-status" {
		t.Fatalf("route=%q original=%q", statusRoute, statusOriginal)
	}
	statusDRM, err := clipproto.UnmarshalDeviceResourceResponse(statusMessage.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if statusDRM.ResponseType != clipproto.DeviceResourceResponseSuccess {
		t.Fatalf("status response=%#v", statusDRM)
	}
	statusClipboard, err := clipproto.UnmarshalResponse(statusDRM.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if statusClipboard.Status != clipproto.ResponseFeatureOn ||
		statusClipboard.CorrelationID != change.CorrelationID {
		t.Fatalf("status clipboard response=%#v", statusClipboard)
	}

	contentPayload := clipproto.MarshalDeviceResourceMessage(
		clipproto.WrapClipboardRequest(
			clipproto.NewContentRequest(change.CorrelationID),
		),
	)
	contentRequest := platform.NewDeviceResourceRequest(contentPayload, "phone-content")
	contentWire, err := platform.Marshal(contentRequest)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		SessionID:            "peer-session",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              contentWire,
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.value.Route != platform.RouteDeviceResourceManager ||
			!got.value.StatusFeatureOnSent ||
			!got.value.ContentDeclined ||
			len(got.value.ClipboardRequests) != 2 {
			t.Fatalf("result=%#v", got.value)
		}
		if got.value.ClipboardRequests[0].ClipboardRequestType != clipproto.RequestStatus ||
			got.value.ClipboardRequests[1].ClipboardRequestType != clipproto.RequestContent ||
			got.value.ClipboardRequests[0].CorrelationID != change.CorrelationID ||
			got.value.ClipboardRequests[1].CorrelationID != change.CorrelationID {
			t.Fatalf("requests=%#v", got.value.ClipboardRequests)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	var contentReply relay.Received
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) >= 3 {
			contentReply = fr.sent[2]
		}
		fr.mu.Unlock()
		if contentReply.Payload != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if contentReply.Payload == nil {
		t.Fatal("no CONTENT probe response")
	}
	contentMessage, err := platform.Unmarshal(contentReply.Payload)
	if err != nil {
		t.Fatal(err)
	}
	contentRoute, _ := contentMessage.Header(platform.HeaderRoute)
	contentOriginal, _ := contentMessage.Header(platform.HeaderOriginalRequestID)
	if contentRoute != platform.RouteInternalResponse || contentOriginal != "phone-content" {
		t.Fatalf("route=%q original=%q", contentRoute, contentOriginal)
	}
	contentDRM, err := clipproto.UnmarshalDeviceResourceResponse(contentMessage.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if contentDRM.ResponseType != clipproto.DeviceResourceResponseResourceHandlerNotRegistered {
		t.Fatalf("content response=%#v", contentDRM)
	}
}

func TestProbeClipboardContextPublishStatusOnlyTimesOutAfterFeatureOn(t *testing.T) {
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
			30*time.Millisecond,
		)
		done <- result{value: got, err: err}
	}()

	var correlationID string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) != 0 {
			pm, err := platform.Unmarshal(fr.sent[0].Payload)
			if err == nil {
				envelope, err := msaep.Unmarshal(pm.Payload)
				if err == nil {
					pubsub, err := clipproto.UnmarshalPubSubPayload(envelope.Payload)
					if err == nil {
						change, err := clipproto.UnmarshalResponse(pubsub.Data)
						if err == nil {
							correlationID = change.CorrelationID
						}
					}
				}
			}
		}
		fr.mu.Unlock()
		if correlationID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if correlationID == "" {
		t.Fatal("no publication correlation id")
	}

	statusPayload := clipproto.MarshalDeviceResourceMessage(
		clipproto.WrapClipboardRequest(
			clipproto.NewStatusRequest(correlationID),
		),
	)
	statusRequest := platform.NewDeviceResourceRequest(statusPayload, "phone-status")
	statusWire, err := platform.Marshal(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              statusWire,
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if !got.value.StatusFeatureOnSent ||
			got.value.ContentDeclined ||
			len(got.value.ClipboardRequests) != 1 ||
			got.value.ClipboardRequests[0].ClipboardRequestType != clipproto.RequestStatus {
			t.Fatalf("result=%#v", got.value)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
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

func TestProbeClipboardContextPublishSendsExplicitTextContent(t *testing.T) {
	fr := newSessionFakeRelay()
	textValue := "linkmyphone probe text"
	type result struct {
		value ContextProbeResult
		err   error
	}
	done := make(chan result, 1)
	go func() {
		got, err := ProbeClipboardContextPublishWithOptions(
			context.Background(),
			fr,
			"phone",
			"desktop",
			ContextProbeOptions{
				Timeout:     time.Second,
				ContentText: &textValue,
			},
		)
		done <- result{value: got, err: err}
	}()

	var correlationID string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) != 0 {
			pm, err := platform.Unmarshal(fr.sent[0].Payload)
			if err == nil {
				envelope, err := msaep.Unmarshal(pm.Payload)
				if err == nil {
					pubsub, err := clipproto.UnmarshalPubSubPayload(envelope.Payload)
					if err == nil {
						change, err := clipproto.UnmarshalResponse(pubsub.Data)
						if err == nil {
							correlationID = change.CorrelationID
						}
					}
				}
			}
		}
		fr.mu.Unlock()
		if correlationID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if correlationID == "" {
		t.Fatal("no publication correlation id")
	}

	statusPayload := clipproto.MarshalDeviceResourceMessage(
		clipproto.WrapClipboardRequest(
			clipproto.NewStatusRequest(correlationID),
		),
	)
	statusRequest := platform.NewDeviceResourceRequest(statusPayload, "phone-status")
	statusWire, err := platform.Marshal(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              statusWire,
	}

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		n := len(fr.sent)
		fr.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	contentPayload := clipproto.MarshalDeviceResourceMessage(
		clipproto.WrapClipboardRequest(
			clipproto.NewContentRequest(correlationID),
		),
	)
	contentRequest := platform.NewDeviceResourceRequest(contentPayload, "phone-content")
	contentWire, err := platform.Marshal(contentRequest)
	if err != nil {
		t.Fatal(err)
	}
	fr.recv <- relay.Received{
		Source:               "phone",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              contentWire,
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if !got.value.StatusFeatureOnSent ||
			!got.value.ContentSent ||
			got.value.ContentDeclined ||
			got.value.ContentTextBytes != len([]byte(textValue)) {
			t.Fatalf("result=%#v", got.value)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	var contentReply relay.Received
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fr.mu.Lock()
		if len(fr.sent) >= 3 {
			contentReply = fr.sent[2]
		}
		fr.mu.Unlock()
		if contentReply.Payload != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if contentReply.Payload == nil {
		t.Fatal("no CONTENT text response")
	}

	replyMessage, err := platform.Unmarshal(contentReply.Payload)
	if err != nil {
		t.Fatal(err)
	}
	originalRequestID, _ := replyMessage.Header(platform.HeaderOriginalRequestID)
	if originalRequestID != "phone-content" {
		t.Fatalf("original request id=%q", originalRequestID)
	}
	drmResponse, err := clipproto.UnmarshalDeviceResourceResponse(replyMessage.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if drmResponse.ResponseType != clipproto.DeviceResourceResponseSuccess {
		t.Fatalf("DRM response=%#v", drmResponse)
	}
	clipboardResponse, err := clipproto.UnmarshalResponse(drmResponse.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if clipboardResponse.Status != clipproto.ResponseOK ||
		clipboardResponse.CorrelationID != correlationID ||
		len(clipboardResponse.Items) != 1 ||
		clipboardResponse.Items[0].Type != clipproto.ItemTextPlain ||
		clipboardResponse.Items[0].Text == nil ||
		*clipboardResponse.Items[0].Text != textValue {
		t.Fatalf("clipboard response=%#v", clipboardResponse)
	}
}

func TestProbeClipboardContextPublishRejectsOversizedExplicitText(t *testing.T) {
	fr := newSessionFakeRelay()
	textValue := string(make([]byte, MaxContextProbeTextBytes+1))
	_, err := ProbeClipboardContextPublishWithOptions(
		context.Background(),
		fr,
		"phone",
		"desktop",
		ContextProbeOptions{
			Timeout:     time.Second,
			ContentText: &textValue,
		},
	)
	if err == nil {
		t.Fatal("expected oversized text error")
	}
}
