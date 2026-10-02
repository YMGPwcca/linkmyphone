package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/services/dcg"
)

func TestEnrollWithMSATokenSequence(t *testing.T) {
	fixed := time.Unix(1_700_000_000, 0).UTC()
	var deviceID string
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		switch step {
		case 1:
			if r.URL.Path != "/Auth/GenerateNonce" {
				t.Fatalf("step1 path=%q", r.URL.Path)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			deviceID = body["deviceId"]
			_, _ = w.Write([]byte("{\"nonce\":\"nonce\"}"))
		case 2:
			if r.URL.Path != "/Auth/CreateIdentity" {
				t.Fatalf("step2 path=%q", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":         "dcg-token",
				"deviceId":            deviceID,
				"epochExpirationTime": fixed.Add(time.Hour).Unix(),
			})
		case 3:
			if r.URL.Path != "/DeviceAuthProxy/EnrollDevice" {
				t.Fatalf("step3 path=%q", r.URL.Path)
			}
			if r.Header.Get("Dcg-Token") != "dcg-token" {
				t.Fatalf("Dcg-Token=%q", r.Header.Get("Dcg-Token"))
			}
			var body dcg.EnrollRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Certificates["SelfSigned"]) != 1 {
				t.Fatalf("certificates=%#v", body.Certificates)
			}
			_, _ = w.Write([]byte("{\"accountCert\":\"account-cert\"}"))
		default:
			t.Fatalf("unexpected request %d", step)
		}
	}))
	defer server.Close()

	authClient := dcgauth.NewClient(server.URL)
	authClient.Now = func() time.Time { return fixed }
	enroller := &Enroller{
		Auth: authClient,
		DCG:  dcg.NewClient(server.URL),
		Now:  func() time.Time { return fixed },
	}
	result, err := enroller.EnrollWithMSAToken(
		context.Background(),
		"msa-token",
		dcg.MetadataForClipboardPC("1.0.0", "linux", "1.0"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if step != 3 || result.Identity.DeviceID != deviceID ||
		result.TrustIdentity.ClientID != "trust_"+deviceID ||
		result.EnrollResponse.AccountCert != "account-cert" {
		t.Fatalf("step=%d result=%#v", step, result)
	}
}
