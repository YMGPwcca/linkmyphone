package dcgheaders

import (
	"net/http"
	"strconv"
)

const (
	PortalLegacyMSM  = "MSM"
	PortalFirstParty = "FirstPartyAppsRepo"
)

type ClientInfo struct {
	LogicalDeviceID string
	AppVersion      string
	AppID           string
	SessionID       string
	RingName        string
	OS              string
	OSVersion       string
}

func (i ClientInfo) ApplyHTTP(h http.Header, authorizationPortal string) {
	if h == nil {
		return
	}
	setIfNonEmpty(h, "DCG-LogicalDeviceId", i.LogicalDeviceID)
	setIfNonEmpty(h, "DCG-AppVersion", i.AppVersion)
	setIfNonEmpty(h, "DCG-AppId", i.AppID)
	setIfNonEmpty(h, "DCG-SessionId", i.SessionID)
	setIfNonEmpty(h, "DCG-RingName", i.RingName)
	setIfNonEmpty(h, "DCG-OS", i.OS)
	setIfNonEmpty(h, "DCG-OSVersion", i.OSVersion)
	setIfNonEmpty(h, "AuthorizationPortal", authorizationPortal)
}

type SignalRInfo struct {
	ClientInfo
	PartnerID        string
	HubRegion        string
	TraceParent      string
	TraceState       string
	HeartbeatSeconds int
}

func (i SignalRInfo) Headers() http.Header {
	h := make(http.Header)
	i.ClientInfo.ApplyHTTP(h, "")
	setIfNonEmpty(h, "DCG-PartnerId", i.PartnerID)
	setIfNonEmpty(h, "DCG-HubRegion", i.HubRegion)
	setIfNonEmpty(h, "traceparent", i.TraceParent)
	setIfNonEmpty(h, "tracestate", i.TraceState)
	setIfNonEmpty(h, "DCG-TraceState", i.TraceState)
	if i.HeartbeatSeconds > 0 {
		h.Set("DCG-HeartBeatFrequency", strconv.Itoa(i.HeartbeatSeconds))
	}
	return h
}

func setIfNonEmpty(h http.Header, key, value string) {
	if value != "" && h.Get(key) == "" {
		h.Set(key, value)
	}
}
