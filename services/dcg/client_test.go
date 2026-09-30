package dcg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListDevicesWireShape(t *testing.T) {
	linked := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/DeviceAuthProxy/GetDeviceInfoList" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("api-version") != "1.5.0" ||
			r.URL.Query().Get("top") != "20" {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer msa" {
			t.Fatalf("Authorization=%q", got)
		}
		if got := r.Header.Get("Authorization-Type"); got != "MSA" {
			t.Fatalf("Authorization-Type=%q", got)
		}
		_ = json.NewEncoder(w).Encode([]DeviceInfo{{
			ID:           "phone",
			IsLinked:     &linked,
			ClientType:   "LTW",
			Certificates: map[string][]string{"SelfSigned": {"cert"}},
		}})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	devices, err := client.ListDevices(context.Background(), "msa")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].ID != "phone" ||
		devices[0].Certificates["SelfSigned"][0] != "cert" {
		t.Fatalf("devices=%#v", devices)
	}
}

func TestAssignedSignalRShardsWireShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/TransportConfiguration/user/transportconfiguration/SignalR" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if r.URL.Query().Get("api-version") != "1.0.0" {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("{\"assignedShards\":[{\"region\":\"westus\"}]}"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	out, err := client.AssignedSignalRShards(context.Background(), "msa")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.AssignedShards) != 1 || out.AssignedShards[0].Region != "westus" {
		t.Fatalf("out=%#v", out)
	}
}

func TestEnrollDeviceWireShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/DeviceAuthProxy/EnrollDevice" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("api-version") != "1.5.0" {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		if got := r.Header.Get("Dcg-Token"); got != "dcg" {
			t.Fatalf("Dcg-Token=%q", got)
		}
		var body EnrollRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Metadata.ClientType != "WEA" ||
			body.Metadata.Capabilities[0] != "CLIPBOARD" ||
			body.Certificates["SelfSigned"][0] != "cert" {
			t.Fatalf("body=%#v", body)
		}
		_, _ = w.Write([]byte("{\"accountCert\":\"account-cert\"}"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	out, err := client.EnrollDevice(context.Background(), "msa", "dcg", "", EnrollRequest{
		Metadata: MetadataForClipboardPC("1.0.0", "linux", "1.0"),
		Certificates: map[string][]string{
			"SelfSigned": {"cert"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.AccountCert != "account-cert" {
		t.Fatalf("out=%#v", out)
	}
}

func TestLinkedPeersFiltersSelfAndUnlinked(t *testing.T) {
	yes, no := true, false
	got := LinkedPeers([]DeviceInfo{
		{ID: "self", IsLinked: &yes},
		{ID: "phone", IsLinked: &yes},
		{ID: "old", IsLinked: &no},
	}, "SELF")
	if len(got) != 1 || got[0].ID != "phone" {
		t.Fatalf("got=%#v", got)
	}
}
