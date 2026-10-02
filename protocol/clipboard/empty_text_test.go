package clipboard

import (
	"bytes"
	"testing"
)

func TestExplicitEmptyTextPresenceOnWire(t *testing.T) {
	// Independent wire fixture: item_type=TEXT_PLAIN, present text="";
	// response status=OK. This is the protobuf optional-field distinction.
	wire := []byte{0x0a, 0x04, 0x08, 0x02, 0x12, 0x00, 0x10, 0x01}
	response, err := UnmarshalResponse(wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Items[0].Text == nil || *response.Items[0].Text != "" {
		t.Fatal("empty selection lost its presence")
	}
	if !bytes.Equal(MarshalResponse(response), wire) {
		t.Fatal("encoding omitted optional empty field")
	}
}
