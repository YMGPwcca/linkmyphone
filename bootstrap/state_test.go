package bootstrap

import (
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
)

func TestBuildStateSnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	identity, err := dcgauth.NewIdentity(now)
	if err != nil {
		t.Fatal(err)
	}
	trustIdentity, err := dcgauth.NewTrustIdentity(identity.DeviceID, now)
	if err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Hour).Unix()
	result := &EnrollResult{
		Identity:      identity,
		TrustIdentity: trustIdentity,
		Token: dcgauth.TokenResponse{
			AccessToken:         "general",
			DeviceID:            identity.DeviceID,
			EpochExpirationTime: &expires,
		},
		EnrollResponse: servicedcg.EnrollResponse{
			AccountCert: "account-cert",
			AccountInfo: &servicedcg.AccountInfo{AccountKey: "account"},
			RootCertificateChain: []string{"root"},
		},
	}
	trustSync := TrustSyncResult{
		Peers: []dcgauth.TrustRelationship{{
			PartnerDcgClientID: "phone",
			PartnerClientID:    "trust_phone",
			TrustType:          dcgauth.TrustTypeAsyncD2D,
		}},
	}
	snapshot, err := BuildStateSnapshot(
		msa.OAuthToken{RefreshToken: "refresh"},
		result,
		trustSync,
		"logical",
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LogicalDeviceID != "logical" ||
		snapshot.MSARefreshToken != "refresh" ||
		snapshot.ServicesToken.Token != "general" ||
		snapshot.Enrollment.AccountInfo.AccountKey != "account" ||
		len(snapshot.TrustRelationships) != 1 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if _, err := snapshot.Identity.Identity(); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.TrustIdentity.Trust(); err != nil {
		t.Fatal(err)
	}
}
