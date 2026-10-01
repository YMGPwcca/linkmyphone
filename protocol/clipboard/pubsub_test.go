package clipboard

import "testing"

func TestPCClipboardChangePublicationUsesData(t *testing.T) {
	p := NewPCClipboardChangePublication("pc-cid")
	if len(p.Data) == 0 {
		t.Fatal("expected Data payload")
	}
	if len(p.Additional) != 0 {
		t.Fatalf("unexpected Additional payload: %x", p.Additional)
	}
	resp, err := UnmarshalResponse(p.Data)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != ResponseClipboardChange || resp.CorrelationID != "pc-cid" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestPhoneClipboardChangePublicationUsesAdditional(t *testing.T) {
	change := MarshalResponse(NewClipboardChange("phone-cid"))
	cid, err := ParsePhoneClipboardChangePublication(PubSubPayload{Additional: change})
	if err != nil {
		t.Fatal(err)
	}
	if cid != "phone-cid" {
		t.Fatalf("cid=%q", cid)
	}
}

func TestPhoneClipboardChangePublicationRejectsDataOnly(t *testing.T) {
	change := MarshalResponse(NewClipboardChange("phone-cid"))
	if _, err := ParsePhoneClipboardChangePublication(PubSubPayload{Data: change}); err == nil {
		t.Fatal("expected missing Additional error")
	}
}
