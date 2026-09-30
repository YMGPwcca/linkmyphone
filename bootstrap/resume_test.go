package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YMGPwcca/phonelink-linux/auth/dcgauth"
	"github.com/YMGPwcca/phonelink-linux/auth/msa"
	authstate "github.com/YMGPwcca/phonelink-linux/auth/state"
)

func TestResumeAuthReusesPersistedDCGIdentity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	identity, err := dcgauth.NewIdentity(now)
	if err != nil {
		t.Fatal(err)
	}
	trustIdentity, err := dcgauth.NewTrustIdentity(identity.DeviceID, now)
	if err != nil {
		t.Fatal(err)
	}
	identityPair, err := authstate.KeyPairFromIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	trustPair, err := authstate.KeyPairFromTrustIdentity(trustIdentity)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := authstate.Snapshot{
		Version:         authstate.CurrentVersion,
		MSARefreshToken: "old-refresh",
		Identity:        identityPair,
		TrustIdentity:   trustPair,
	}

	msaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Fatalf("MSA path=%q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" ||
			r.Form.Get("refresh_token") != "old-refresh" {
			t.Fatalf("MSA form=%v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "msa-new",
			"refresh_token": "refresh-new",
			"expires_in":    3600,
		})
	}))
	defer msaServer.Close()

	step := 0
	dcgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		switch step {
		case 1:
			if r.URL.Path != "/Auth/GenerateNonce" {
				t.Fatalf("path=%q", r.URL.Path)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["deviceId"] != identity.DeviceID {
				t.Fatalf("deviceId=%q want=%q", body["deviceId"], identity.DeviceID)
			}
			_, _ = w.Write([]byte(`{"nonce":"nonce"}`))
		case 2:
			if r.URL.Path != "/Auth/SignIn" {
				t.Fatalf("path=%q", r.URL.Path)
			}
			expires := now.Add(time.Hour).Unix()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":         "general-new",
				"deviceId":            identity.DeviceID,
				"epochExpirationTime": expires,
			})
		default:
			t.Fatalf("unexpected DCG request %d", step)
		}
	}))
	defer dcgServer.Close()

	msaClient := msa.NewDeviceCodeClient()
	msaClient.Authority = msaServer.URL
	dcgClient := dcgauth.NewClient(dcgServer.URL)
	dcgClient.Now = func() time.Time { return now }

	result, err := ResumeAuth(context.Background(), msaClient, dcgClient, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if result.Identity.DeviceID != identity.DeviceID ||
		result.ServicesToken.Token != "general-new" ||
		result.State.MSARefreshToken != "refresh-new" ||
		result.State.ServicesToken.Token != "general-new" ||
		step != 2 {
		t.Fatalf("result=%#v step=%d", result, step)
	}
}
