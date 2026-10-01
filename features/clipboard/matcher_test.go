package clipboard

import (
	"testing"

	clipproto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/msaep"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

func TestMatcherScopesClipboardTraffic(t *testing.T) {
	internal, err := platform.Marshal(platform.NewInternalResponse(nil, "request"))
	if err != nil {
		t.Fatal(err)
	}
	if !matchesMessage(relay.Received{
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              internal,
	}) {
		t.Fatal("clipboard matcher rejected internal response")
	}

	clipboardRequest := platform.NewDeviceResourceRequest(
		clipproto.MarshalDeviceResourceMessage(
			clipproto.WrapClipboardRequest(clipproto.NewStatusRequest("cid")),
		),
		"request",
	)
	clipboardWire, err := platform.Marshal(clipboardRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !matchesMessage(relay.Received{
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              clipboardWire,
	}) {
		t.Fatal("clipboard matcher rejected /clipboard request")
	}

	otherDRM := clipproto.DeviceResourceMessage{
		ResourcePath: "/other",
		RequestType:  clipproto.DeviceResourceRequestGET,
	}
	otherMessage := platform.NewDeviceResourceRequest(
		clipproto.MarshalDeviceResourceMessage(otherDRM),
		"request",
	)
	otherWire, err := platform.Marshal(otherMessage)
	if err != nil {
		t.Fatal(err)
	}
	if matchesMessage(relay.Received{
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              otherWire,
	}) {
		t.Fatal("clipboard matcher accepted another resource")
	}

	envelope := msaep.New("id", "phone", int32(clipproto.ClipboardMessageTag), []byte{1})
	contextWire, err := platform.Marshal(platform.NewContextPublish(msaep.Marshal(envelope), "pub"))
	if err != nil {
		t.Fatal(err)
	}
	if !matchesMessage(relay.Received{
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              contextWire,
	}) {
		t.Fatal("clipboard matcher rejected tag 9 publication")
	}
}

func TestMatcherRejectsClipboardTrafficFromOtherPeer(t *testing.T) {
	request := platform.NewDeviceResourceRequest(
		clipproto.MarshalDeviceResourceMessage(
			clipproto.WrapClipboardRequest(clipproto.NewStatusRequest("cid")),
		),
		"request",
	)
	wire, err := platform.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	matcher := matcherForTarget("phone")
	if !matcher(relay.Received{
		Source:               "phone",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              wire,
	}) {
		t.Fatal("target clipboard traffic was rejected")
	}
	if matcher(relay.Received{
		Source:               "other-phone",
		TransportMessageType: dcg.TransportMessageTypePlatform,
		Payload:              wire,
	}) {
		t.Fatal("clipboard traffic from another peer reached target-scoped subscription")
	}
}
