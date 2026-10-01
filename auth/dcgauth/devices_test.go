package dcgauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDeviceInfoListWireShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/DeviceAuthProxy/GetDeviceInfoList" {
			t.Fatalf("request=%s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("api-version") != "1.5.0" ||
			r.URL.Query().Get("top") != "20" {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer msa" {
			t.Fatalf("Authorization=%q", got)
		}
		if got := r.Header.Get(HeaderAuthorizationType); got != "MSA" {
			t.Fatalf("Authorization-Type=%q", got)
		}
		_ = json.NewEncoder(w).Encode([]DeviceInfoItem{{
			ID:       "phone",
			IsLinked: true,
			OSName:   "Android",
			Certificates: map[string][]string{
				"SelfSigned": {"cert"},
			},
		}})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	items, err := client.GetDeviceInfoList(context.Background(), "msa")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "phone" {
		t.Fatalf("items=%#v", items)
	}
}

func TestLinkedPeerAndTrustHelpers(t *testing.T) {
	items := []DeviceInfoItem{
		{ID: "self", IsLinked: true},
		{ID: "phone", IsLinked: true, OSName: "Android", CustomData: `{"yppCapabilities":32}`, Certificates: map[string][]string{"SelfSigned": {"self"}, "PKI": {"pki"}}},
		{ID: "old", IsLinked: false},
	}
	peers := LinkedPeerDevices(items, "SELF")
	if len(peers) != 1 || peers[0].ID != "phone" {
		t.Fatalf("peers=%#v", peers)
	}
	if got := CryptoTrustClientID("phone"); got != "trust_phone" {
		t.Fatalf("trust id=%q", got)
	}
	cert, ok := PeerTrustCertificate(peers[0])
	if !ok || cert != "pki" {
		t.Fatalf("cert=%q ok=%v", cert, ok)
	}
	if !SupportsAsyncTrust(peers[0]) {
		t.Fatal("expected async trust capability")
	}
}
