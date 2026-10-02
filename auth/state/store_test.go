package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	identity, err := dcgauth.NewIdentity(now)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := dcgauth.NewTrustIdentity(identity.DeviceID, now)
	if err != nil {
		t.Fatal(err)
	}
	identityPair, err := KeyPairFromIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	trustPair, err := KeyPairFromTrustIdentity(trust)
	if err != nil {
		t.Fatal(err)
	}
	logicalID, err := NewLogicalDeviceID()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private", "state.json")
	want := Snapshot{
		LogicalDeviceID: logicalID,
		MSARefreshToken: "refresh",
		Identity:        identityPair,
		TrustIdentity:   trustPair,
		ServicesToken: ServicesToken{
			Token:     "general",
			Scope:     dcgauth.ServicesScopeGeneral,
			DeviceID:  identity.DeviceID,
			ExpiresAt: now.Add(time.Hour),
		},
		Enrollment: Enrollment{
			AccountCert:          "account-cert",
			RootCertificateChain: []string{"root"},
		},
	}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode=%#o", got)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	loadedIdentity, err := got.Identity.Identity()
	if err != nil {
		t.Fatal(err)
	}
	loadedTrust, err := got.TrustIdentity.Trust()
	if err != nil {
		t.Fatal(err)
	}
	if got.MSARefreshToken != "refresh" ||
		got.LogicalDeviceID != logicalID ||
		loadedIdentity.DeviceID != identity.DeviceID ||
		loadedTrust.ClientID != trust.ClientID ||
		got.ServicesToken.Token != "general" {
		t.Fatalf("got=%#v", got)
	}
}

func TestKeyPairRejectsMismatchedID(t *testing.T) {
	identity, err := dcgauth.NewIdentity(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	pair, err := KeyPairFromIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	pair.ID = "wrong"
	if _, err := pair.Identity(); err == nil {
		t.Fatal("expected certificate CN mismatch")
	}
}
