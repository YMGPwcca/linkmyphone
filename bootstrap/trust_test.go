package bootstrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YMGPwcca/phonelink-linux/auth/dcgauth"
	servicedcg "github.com/YMGPwcca/phonelink-linux/services/dcg"
)

func TestSyncTrustBuildsAccountAndLinkedPeerRelationships(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	account, err := dcgauth.NewIdentity(now)
	if err != nil {
		t.Fatal(err)
	}
	phoneTrust, err := dcgauth.NewTrustIdentity("phone", now)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/DeviceAuthProxy/GetDeviceInfoList" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if r.URL.Query().Get("api-version") != servicedcg.DeviceManagementAPIVersion ||
			r.URL.Query().Get("top") != "20" {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		linked := true
		enabled := true
		_ = json.NewEncoder(w).Encode([]servicedcg.DeviceInfo{
			{ID: "self", IsLinked: &linked, IsEnabled: &enabled},
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
	}))
	defer server.Close()

	client := servicedcg.NewClient(server.URL)
	result, err := SyncTrust(
		context.Background(),
		client,
		"msa",
		"self",
		base64.StdEncoding.EncodeToString(account.CertificateDER),
		"cid",
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Account.TrustType != dcgauth.TrustTypeA2D ||
		result.Account.PartnerDcgClientID != account.DeviceID {
		t.Fatalf("account=%#v", result.Account)
	}
	if len(result.Peers) != 1 ||
		result.Peers[0].PartnerDcgClientID != "phone" ||
		result.Peers[0].TrustType != dcgauth.TrustTypeAsyncD2D {
		t.Fatalf("peers=%#v", result.Peers)
	}
	if len(result.Relationships()) != 2 {
		t.Fatalf("relationships=%#v", result.Relationships())
	}
}
