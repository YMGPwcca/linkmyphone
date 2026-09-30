package dcgauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnrollDeviceWireShape(t *testing.T) {
	fixed := time.Unix(1700000000, 0).UTC()
	trust, err := NewTrustIdentity("local-dcg", fixed)
	if err != nil {
		t.Fatal(err)
	}
	body, err := EnrollmentRequestWithTrustIdentity(
		WindowsCompatibleEnrollmentMetadata("1.0.0", "linux-box", "10.0", nil),
		trust,
	)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/DeviceAuthProxy/EnrollDevice" {
			t.Fatalf("request=%s %s", r.Method, r.URL.String())
		}
		if got := r.URL.Query().Get("api-version"); got != DeviceManagementAPIVersion {
			t.Fatalf("api-version=%q", got)
		}
		if _, ok := r.URL.Query()["pop-device-key"]; ok {
			t.Fatalf("unexpected pop-device-key: %q", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer msa-token" {
			t.Fatalf("Authorization=%q", got)
		}
		if got := r.Header.Get(HeaderAuthorizationType); got != UserIdentityTypeMSA {
			t.Fatalf("Authorization-Type=%q", got)
		}
		if got := r.Header.Get("Dcg-Token"); got != "dcg-general" {
			t.Fatalf("Dcg-Token=%q", got)
		}

		var got EnrollmentRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		certs := got.Certificates[CertificateSelfSigned]
		if len(certs) != 1 || certs[0] != trust.CertificateBase64() {
			t.Fatalf("certificates=%#v", got.Certificates)
		}
		if got.Metadata.ClientType != ClientTypeWEA ||
			got.Metadata.OSName != "Windows" ||
			!got.Metadata.IsEnabled ||
			got.Metadata.DisplayName != "linux-box" {
			t.Fatalf("metadata=%#v", got.Metadata)
		}

		_ = json.NewEncoder(w).Encode(EnrollmentResponse{
			AccountCert: "account-cert",
			AccountInfo: EnrollmentAccountInfo{AccountKey: "account"},
			RootCertificateChain: []string{"root"},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	out, err := client.EnrollDevice(context.Background(), "msa-token", "dcg-general", "", body)
	if err != nil {
		t.Fatal(err)
	}
	if out.AccountCert != "account-cert" || out.AccountInfo.AccountKey != "account" {
		t.Fatalf("out=%#v", out)
	}
}

func TestEnrollDeviceIncludesOptionalPartnerKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("pop-device-key"); got != "PartnerDeviceKey~abc" {
			t.Fatalf("pop-device-key=%q", got)
		}
		_, _ = w.Write([]byte("{\"accountCert\":\"account-cert\"}"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.EnrollDevice(context.Background(), "msa", "dcg", "PartnerDeviceKey~abc", EnrollmentRequest{
		Certificates: map[string][]string{CertificateSelfSigned: {"cert"}},
		Metadata: WindowsCompatibleEnrollmentMetadata("1", "host", "10", []string{}),
	})
	if err != nil {
		t.Fatal(err)
	}
}
