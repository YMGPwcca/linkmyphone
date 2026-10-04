package dcgheaders

import "testing"

func TestInvalidEnrollmentProfileCannotAcquireClientHeaders(t *testing.T) {
	if _, err := NewClientInfo("unknown", "logical", "1.2.3.4", "Public", "10.0.26100"); err == nil {
		t.Fatal("unknown profile could acquire usable client headers")
	}
	if _, err := NewClientInfo(ProfilePhoneLink, "", "1.2.3.4", "Public", "10.0.26100"); err == nil {
		t.Fatal("missing logical identity could acquire usable client headers")
	}
}
