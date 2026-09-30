package signalr

import "testing"

func TestParseHubPing(t *testing.T) {
	body := []byte{0x91, 0x06}
	mt, a, err := ParseHubMessage(body)
	if err != nil || mt != HubMessageTypePing || len(a) != 1 {
		t.Fatalf("mt=%d a=%v err=%v", mt, a, err)
	}
}
