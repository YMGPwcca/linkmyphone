package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
)

func TestDCGIdentityFromMSA(t *testing.T) {
	var deviceID string
	calls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("Authorization"); got != "Bearer msa-access" {
			t.Fatalf("Authorization=%q", got)
		}
		if got := r.Header.Get(dcgauth.HeaderAuthorizationType); got != dcgauth.UserIdentityTypeMSA {
			t.Fatalf("Authorization-Type=%q", got)
		}

		switch r.URL.Path {
		case "/Auth/GenerateNonce":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			deviceID = body["deviceId"]
			if deviceID == "" {
				t.Fatal("missing deviceId")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"nonce": "nonce",
			})

		case "/Auth/CreateIdentity":
			if deviceID == "" {
				t.Fatal("CreateIdentity arrived before GenerateNonce")
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["certificateJWT"] == "" {
				t.Fatal("missing certificateJWT")
			}
			expires := time.Now().Add(time.Hour).Unix()
			keyDays := int64(180)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":           "dcg-general",
				"deviceId":              deviceID,
				"epochExpirationTime":   expires,
				"keyValidRemainingDays": keyDays,
				"tenantId":              "tenant",
			})

		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	dcg := dcgauth.NewClient(server.URL)
	result, err := DCGIdentityFromMSA(context.Background(), dcg, msa.OAuthToken{
		AccessToken:  "msa-access",
		RefreshToken: "msa-refresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	if result.Identity == nil || result.Identity.DeviceID != deviceID {
		t.Fatalf("identity=%#v deviceID=%q", result.Identity, deviceID)
	}
	if result.ServicesToken.Token != "dcg-general" {
		t.Fatalf("token=%q", result.ServicesToken.Token)
	}
	if result.ServicesToken.Scope != dcgauth.ServicesScopeGeneral {
		t.Fatalf("scope=%q", result.ServicesToken.Scope)
	}
	if result.ServicesToken.DeviceID != deviceID {
		t.Fatalf("token device=%q want=%q", result.ServicesToken.DeviceID, deviceID)
	}
	if result.MSAToken.RefreshToken != "msa-refresh" {
		t.Fatalf("MSA refresh token=%q", result.MSAToken.RefreshToken)
	}
}
