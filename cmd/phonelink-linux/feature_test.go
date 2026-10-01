package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
)

func TestFeatureCommandCRUDForClipboard(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "features.json")

	if err := runFeatureCreate([]string{
		"--state", statePath,
		"--enabled",
		"phonelink.clipboard",
	}); err != nil {
		t.Fatal(err)
	}

	store, err := kernel.OpenFeatureStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Read("phonelink.clipboard")
	if err != nil {
		t.Fatal(err)
	}
	if !record.Enabled {
		t.Fatal("created clipboard feature should be enabled")
	}

	if err := runFeatureUpdate([]string{
		"--state", statePath,
		"--config", `{"poll_interval_ms":250,"request_timeout_ms":5000,"publish_initial":false}`,
		"phonelink.clipboard",
	}); err != nil {
		t.Fatal(err)
	}
	if err := runFeatureToggle([]string{
		"--state", statePath,
		"phonelink.clipboard",
	}, false); err != nil {
		t.Fatal(err)
	}

	store, err = kernel.OpenFeatureStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	record, err = store.Read("phonelink.clipboard")
	if err != nil {
		t.Fatal(err)
	}
	if record.Enabled {
		t.Fatal("clipboard feature should be disabled")
	}
	var cfg map[string]any
	if err := json.Unmarshal(record.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["poll_interval_ms"] != float64(250) {
		t.Fatalf("config=%v", cfg)
	}

	if err := runFeatureDelete([]string{
		"--state", statePath,
		"phonelink.clipboard",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureCommandCanDisableAndDeleteStaleRecord(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "features.json")
	store, err := kernel.OpenFeatureStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(kernel.FeatureRecord{
		ID:      "phonelink.retired",
		Enabled: true,
		Config:  json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}

	if err := runFeatureToggle([]string{
		"--state", statePath,
		"phonelink.retired",
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := runFeatureToggle([]string{
		"--state", statePath,
		"phonelink.retired",
	}, true); err == nil {
		t.Fatal("unavailable stale feature must not be enable-able")
	}
	if err := runFeatureDelete([]string{
		"--state", statePath,
		"phonelink.retired",
	}); err != nil {
		t.Fatal(err)
	}
}
