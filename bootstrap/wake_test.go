package bootstrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/services/dcg"
)

func TestWakePartnerBuildsSignedDispatcherPayload(t *testing.T) {
	trust, err := dcgauth.NewTrustIdentity("local", time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body dcg.WakeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.DeviceID != "phone" ||
			body.TTL != 30 ||
			body.Data["DCG-Environment"] != "Prod" ||
			body.Data["IgnoreDeviceDisabledStatus"] != "False" ||
			body.Data["DCG-RequestNewSession"] != "True" ||
			body.Data["DCG-HubRegion"] != "westus" {
			t.Fatalf("body=%#v", body)
		}
		parts := strings.Split(body.Data["DCG-CryptoWakeJwt"], ".")
		if len(parts) != 3 {
			t.Fatalf("jwt=%q", body.Data["DCG-CryptoWakeJwt"])
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			t.Fatal(err)
		}
		var claims map[string]any
		if err := json.Unmarshal(payload, &claims); err != nil {
			t.Fatal(err)
		}
		if claims["iss"] != "trust_local" ||
			claims["SourceId"] != "local" ||
			claims["Scope"] != "wake" {
			t.Fatalf("claims=%#v", claims)
		}
		var signedData map[string]string
		if err := json.Unmarshal([]byte(claims["Data"].(string)), &signedData); err != nil {
			t.Fatal(err)
		}
		if _, exists := signedData["DCG-CryptoWakeJwt"]; exists {
			t.Fatal("signed Data must not recursively contain the crypto JWT")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err = WakePartner(
		context.Background(),
		dcg.NewClient(server.URL),
		trust,
		"local",
		"phone",
		"dcg-token",
		WakeOptions{
			HubRegion:         "westus",
			RequestNewSession: true,
			TimeToLive:        30 * time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}
