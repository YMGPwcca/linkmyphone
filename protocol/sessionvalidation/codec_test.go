package sessionvalidation

import (
	"bytes"
	"testing"
)

func TestBaselineRequestWire(t *testing.T) {
	got := MarshalRequest(Request{Capabilities: []Capability{CapabilitySessionValidation}})
	want := []byte{0x0a, 0x01, 0x01}
	if !bytes.Equal(got, want) {
		t.Fatalf("wire=%x want=%x", got, want)
	}
}

func TestResponseRoundTrip(t *testing.T) {
	want := Response{
		Capabilities: []Capability{
			CapabilitySessionValidation,
			CapabilityPersistentMessageChannel,
			CapabilityNanoTransportPreference,
		},
		PersistentMessagingChannelVersion: 14,
		NanoTransportPreferenceVersion:    3,
	}
	wire := MarshalResponse(want)
	got, err := UnmarshalResponse(wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Capabilities) != 3 ||
		!got.Has(CapabilitySessionValidation) ||
		!got.Has(CapabilityPersistentMessageChannel) ||
		!got.Has(CapabilityNanoTransportPreference) ||
		got.PersistentMessagingChannelVersion != 14 ||
		got.NanoTransportPreferenceVersion != 3 {
		t.Fatalf("got=%#v wire=%x", got, wire)
	}
}

func TestResponseAcceptsUnpackedCapability(t *testing.T) {
	got, err := UnmarshalResponse([]byte{0x08, 0x01})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Has(CapabilitySessionValidation) {
		t.Fatalf("got=%#v", got)
	}
}
