package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YMGPwcca/linkmyphone/runtime/controlplane"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
)

func TestFeatureCommandCRUDForClipboard(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "features.json")

	if err := runFeatureCreate([]string{
		"--state", statePath,
		"--enabled",
		"linkmyphone.clipboard",
	}); err != nil {
		t.Fatal(err)
	}

	store, err := kernel.OpenFeatureStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Read("linkmyphone.clipboard")
	if err != nil {
		t.Fatal(err)
	}
	if !record.Enabled {
		t.Fatal("created clipboard feature should be enabled")
	}

	if err := runFeatureUpdate([]string{
		"--state", statePath,
		"--config", `{"poll_interval_ms":250,"request_timeout_ms":5000,"publish_initial":false}`,
		"linkmyphone.clipboard",
	}); err != nil {
		t.Fatal(err)
	}
	if err := runFeatureToggle([]string{
		"--state", statePath,
		"linkmyphone.clipboard",
	}, false); err != nil {
		t.Fatal(err)
	}

	store, err = kernel.OpenFeatureStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	record, err = store.Read("linkmyphone.clipboard")
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
		"linkmyphone.clipboard",
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
		ID:      "linkmyphone.retired",
		Enabled: true,
		Config:  json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}

	if err := runFeatureToggle([]string{
		"--state", statePath,
		"linkmyphone.retired",
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := runFeatureToggle([]string{
		"--state", statePath,
		"linkmyphone.retired",
	}, true); err == nil {
		t.Fatal("unavailable stale feature must not be enable-able")
	}
	if err := runFeatureDelete([]string{
		"--state", statePath,
		"linkmyphone.retired",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureCommandDoesNotFallbackOfflineWhenRuntimeRejectsMutation(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "features.json")
	store, err := kernel.OpenFeatureStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(kernel.FeatureRecord{
		ID:      "linkmyphone.retired",
		Enabled: false,
		Config:  json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}

	socketPath := controlplane.SocketPathForStore(statePath)
	server, err := controlplane.Listen(
		socketPath,
		controlplane.HandlerFunc(func(controlplane.Request) controlplane.Response {
			return controlplane.Failure(errors.New("runtime is starting; retry the feature command"))
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Serve(ctx)
	}()

	err = runFeatureDelete([]string{
		"--state", statePath,
		"linkmyphone.retired",
	})
	if err == nil || !strings.Contains(err.Error(), "runtime is starting") {
		t.Fatalf("delete err=%v", err)
	}

	reopened, err := kernel.OpenFeatureStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Read("linkmyphone.retired"); err != nil {
		t.Fatalf("offline fallback mutated store despite live runtime rejection: %v", err)
	}
}
