package dcgheaders

import "testing"

func TestNewCrossDeviceClientInfo(t *testing.T) {
	info, err := NewCrossDeviceClientInfo("logical", "1.2.3.4", "Public", "10.0.26100")
	if err != nil {
		t.Fatal(err)
	}
	if info.LogicalDeviceID != "logical" ||
		info.AppID != CrossDeviceAppID ||
		info.AppVersion != "1.2.3.4" ||
		info.SessionID == "" ||
		info.RingName != "Public" ||
		info.OS != "Windows" ||
		info.OSVersion != "10.0.26100" {
		t.Fatalf("info=%#v", info)
	}
}
