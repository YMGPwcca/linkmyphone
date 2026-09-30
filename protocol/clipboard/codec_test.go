package clipboard

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	want := Request{Type: RequestContent, CorrelationID: "abc"}
	b := MarshalRequest(want)
	if gotHex := hex.EncodeToString(b); gotHex != "08011203616263" {
		t.Fatalf("wire = %s", gotHex)
	}
	got, err := UnmarshalRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestClipboardChangePubSub(t *testing.T) {
	change := MarshalResponse(NewClipboardChange("cid-1"))
	payload := MarshalPubSubPayload(PubSubPayload{Additional: change})

	decoded, err := UnmarshalPubSubPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data) != 0 {
		t.Fatalf("unexpected data: %x", decoded.Data)
	}

	resp, err := UnmarshalResponse(decoded.Additional)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != ResponseClipboardChange || resp.CorrelationID != "cid-1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestStatusRequestDRMWrapper(t *testing.T) {
	wrapped := WrapClipboardRequest(NewStatusRequest("status-cid"))
	wire := MarshalDeviceResourceMessage(wrapped)
	got, err := UnmarshalDeviceResourceMessage(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResourceType != DeviceResourceTypeUnknown || got.RequestType != DeviceResourceRequestGET || got.ResourcePath != ResourcePath {
		t.Fatalf("bad wrapper: %#v", got)
	}
	req, err := UnmarshalRequest(got.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if req.Type != RequestStatus || req.CorrelationID != "status-cid" {
		t.Fatalf("bad request: %#v", req)
	}
}

func TestTextResponseRoundTrip(t *testing.T) {
	want := NewTextResponse("text-cid", "hello", &Timestamp{Seconds: 1_700_000_000, Nanos: 123})
	b := MarshalResponse(want)
	got, err := UnmarshalResponse(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v\nwire=%x", got, want, b)
	}
}

func TestUnknownFieldsAreSkipped(t *testing.T) {
	b := MarshalRequest(NewContentRequest("x"))
	b = append(b, 0x98, 0x06, 0x01) // field 99, varint 1
	got, err := UnmarshalRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != RequestContent || got.CorrelationID != "x" {
		t.Fatalf("got %#v", got)
	}
}

func TestMalformedLength(t *testing.T) {
	_, err := UnmarshalRequest([]byte{0x12, 0x05, 'a'})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestImageItem(t *testing.T) {
	want := Response{Items: []Item{{Type: ItemImage, ImageBytes: []byte{1, 2, 3}}}, Status: ResponseOK, CorrelationID: "img"}
	b := MarshalResponse(want)
	got, err := UnmarshalResponse(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || !bytes.Equal(got.Items[0].ImageBytes, []byte{1, 2, 3}) {
		t.Fatalf("got %#v", got)
	}
}


func TestDeviceResourceResponseRoundTrip(t *testing.T) {
	want := DeviceResourceResponse{Payload: []byte{1, 2, 3}, ResponseType: DeviceResourceResponseSuccess}
	wire := MarshalDeviceResourceResponse(want)
	got, err := UnmarshalDeviceResourceResponse(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResponseType != want.ResponseType || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}
