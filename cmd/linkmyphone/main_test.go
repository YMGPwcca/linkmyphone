package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortID(t *testing.T) {
	if got := shortID("12345678-aaaa-bbbb-cccc-123456789012"); got != "12345678...9012" {
		t.Fatalf("shortID=%q", got)
	}
	if got := shortID("short"); got != "short" {
		t.Fatalf("shortID=%q", got)
	}
}

func TestExistingCrossDeviceEnrollmentRejectsProfileSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	original := []byte(`{"version":1,"logicalDeviceId":"existing"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	err := runBootstrapProbe(context.Background(), []string{"--state", path, "--profile", "phonelink"})
	if err == nil || !strings.Contains(err.Error(), "different client profile") {
		t.Fatalf("existing enrollment must refuse reclassification: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("existing WEA enrollment was overwritten")
	}
}
