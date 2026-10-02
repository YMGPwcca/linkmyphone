package bootstrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
	authstate "github.com/YMGPwcca/linkmyphone/auth/state"
	"github.com/YMGPwcca/linkmyphone/dcgheaders"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
	signalrtransport "github.com/YMGPwcca/linkmyphone/transport/signalr"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
)

func TestBootstrapFirstRunStagesOneThroughFour(t *testing.T) {
	now := time.Now().UTC()
	account, err := dcgauth.NewIdentity(now)
	if err != nil {
		t.Fatal(err)
	}
	phoneTrust, err := dcgauth.NewTrustIdentity("phone", now)
	if err != nil {
		t.Fatal(err)
	}

	var selfID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("AuthorizationPortal"); got != dcgheaders.PortalFirstParty {
			t.Fatalf("AuthorizationPortal=%q path=%q", got, r.URL.Path)
		}
		switch r.URL.Path {
		case "/Auth/GenerateNonce":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			selfID = body["deviceId"]
			_, _ = w.Write([]byte(`{"nonce":"nonce"}`))

		case "/Auth/CreateIdentity":
			expires := now.Add(time.Hour).Unix()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":         "dcg-general",
				"deviceId":            selfID,
				"epochExpirationTime": expires,
			})

		case "/DeviceAuthProxy/EnrollDevice":
			if r.Header.Get("Dcg-Token") != "dcg-general" {
				t.Fatalf("Dcg-Token=%q", r.Header.Get("Dcg-Token"))
			}
			_ = json.NewEncoder(w).Encode(servicedcg.EnrollResponse{
				AccountCert: base64.StdEncoding.EncodeToString(account.CertificateDER),
				AccountInfo: &servicedcg.AccountInfo{AccountKey: "account"},
			})

		case "/DeviceAuthProxy/GetDeviceInfoList":
			linked := true
			enabled := true
			_ = json.NewEncoder(w).Encode([]servicedcg.DeviceInfo{
				{ID: selfID, IsLinked: &linked, IsEnabled: &enabled},
				{
					ID:        "phone",
					IsLinked:  &linked,
					IsEnabled: &enabled,
					OSName:    "Android",
					Certificates: map[string][]string{
						dcgauth.CertificateSelfSigned: {phoneTrust.CertificateBase64()},
					},
				},
			})

		case "/TransportConfiguration/user/transportconfiguration/SignalR":
			_, _ = w.Write([]byte(`{"assignedShards":[{"region":"westus"}]}`))

		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	dial := func(ctx context.Context, cfg signalrtransport.Config) (relay.Hub, error) {
		if cfg.AccessToken != "dcg-general" {
			t.Fatalf("access token=%q", cfg.AccessToken)
		}
		if cfg.Headers.Get("DCG-HubRegion") != "westus" ||
			cfg.Headers.Get("DCG-PartnerId") != "" {
			t.Fatalf("headers=%#v", cfg.Headers)
		}
		return newCloudFakeHub(hubOnConnectedFrame(t, "westus", []string{"phone"})), nil
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	authClient := dcgauth.NewClient(server.URL)
	serviceClient := servicedcg.NewClient(server.URL)
	result, err := BootstrapFirstRun(context.Background(), FirstRunConfig{
		Auth:        authClient,
		Services:    serviceClient,
		MSAToken:    msa.OAuthToken{AccessToken: "msa-access", RefreshToken: "msa-refresh"},
		AppVersion:  "1.26072.116.0",
		DisplayName: "linux-box",
		RingName:    "Public",
		OSVersion:   "10.0.26100",
		MSACID:      "cid",
		StatePath:   statePath,
		DialHub:     dial,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Cloud.Close()

	if result.Enrollment == nil ||
		result.Enrollment.Identity.DeviceID != selfID ||
		len(result.Trust.Peers) != 1 ||
		result.Trust.Peers[0].PartnerDcgClientID != "phone" ||
		result.Cloud.Region != "westus" ||
		len(result.Cloud.Partners) != 1 ||
		result.Cloud.Partners[0] != "phone" {
		t.Fatalf("result=%#v", result)
	}

	saved, err := authstate.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.MSARefreshToken != "msa-refresh" ||
		saved.ServicesToken.Token != "dcg-general" ||
		len(saved.TrustRelationships) != 2 ||
		saved.LogicalDeviceID == "" {
		t.Fatalf("saved=%#v", saved)
	}
}
