package dcgheaders

import (
	"net/http"
	"testing"
)

func TestHTTPHeaders(t *testing.T) {
	info := ClientInfo{
		LogicalDeviceID: "logical",
		AppVersion:      "1.2.3.4",
		AppID:           "app",
		SessionID:       "session",
		RingName:        "retail",
		OS:              "Windows",
		OSVersion:       "10.0.26100",
	}
	h := make(http.Header)
	info.ApplyHTTP(h, PortalLegacyMSM)
	if h.Get("DCG-LogicalDeviceId") != "logical" ||
		h.Get("DCG-AppVersion") != "1.2.3.4" ||
		h.Get("AuthorizationPortal") != "MSM" {
		t.Fatalf("headers=%v", h)
	}
}

func TestSignalRHeaders(t *testing.T) {
	h := (SignalRInfo{
		ClientInfo: ClientInfo{
			SessionID: "app-session",
		},
		PartnerID:        "phone",
		HubRegion:        "westus",
		TraceParent:      "trace",
		TraceState:       "state",
		HeartbeatSeconds: 15,
	}).Headers()
	if h.Get("DCG-PartnerId") != "phone" ||
		h.Get("DCG-HubRegion") != "westus" ||
		h.Get("DCG-HeartBeatFrequency") != "15" ||
		h.Get("DCG-TraceState") != "state" {
		t.Fatalf("headers=%v", h)
	}
}
