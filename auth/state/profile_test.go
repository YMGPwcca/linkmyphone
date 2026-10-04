package state

import (
	"github.com/YMGPwcca/linkmyphone/dcgheaders"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyStateKeepsCrossDeviceAndPhoneLinkPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"logicalDeviceId":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ClientProfile != dcgheaders.ProfileCrossDevice || legacy.LogicalDeviceID != "old" {
		t.Fatalf("legacy identity changed: %#v", legacy)
	}
	legacy.ClientProfile = dcgheaders.ProfilePhoneLink
	if err := Save(path, legacy); err != nil {
		t.Fatal(err)
	}
	resumed, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ClientProfile != dcgheaders.ProfilePhoneLink || resumed.LogicalDeviceID != "old" {
		t.Fatalf("resume changed enrollment: %#v", resumed)
	}
}

func TestUnknownProfileCannotOverwriteWorkingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	original := []byte(`{"version":1,"logicalDeviceId":"working"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Snapshot{ClientProfile: "invalid"}); err == nil {
		t.Fatal("invalid profile saved")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("working state was modified by rejected profile")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"clientProfile":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("invalid persisted profile accepted")
	}
}
