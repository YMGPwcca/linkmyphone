package main

import (
	"context"
	"encoding/json"
	"flag"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/runtime/controlplane"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
)

func TestRecoveryGenerationLoadsCommittedFeatureState(t *testing.T) {
	previous, module := newTestRuntimeController(t)
	ctx := context.Background()
	record := kernel.FeatureRecord{ID: module.manifest.ID, Enabled: true, Config: json.RawMessage("{\"value\":1}")}
	if _, _, err := previous.create(ctx, record); err != nil {
		t.Fatal(err)
	}
	disabled := false
	config := json.RawMessage("{\"value\":2}")
	if _, _, err := previous.update(ctx, record.ID, &disabled, &config); err != nil {
		t.Fatal(err)
	}
	if err := stopFeatureGeneration(previous.registry); err != nil {
		t.Fatal(err)
	}
	replacement := newRuntimeController(ctx, previous.store, kernel.NewRegistry(nil), nil, nil)
	replacement.resolve = previous.resolve
	if err := replacement.load(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, err := replacement.registry.Read(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Enabled || string(snapshot.Config) != string(config) || module.startCount != 1 {
		t.Fatal("recovery lost desired state or started disabled feature")
	}
	enabled := true
	if _, _, err := replacement.update(ctx, record.ID, &enabled, nil); err != nil {
		t.Fatal(err)
	}
	if module.startCount != 2 {
		t.Fatal("CRUD could not enable module after recovery")
	}
	if err := stopFeatureGeneration(replacement.registry); err != nil {
		t.Fatal(err)
	}
	if module.stopCount != 2 {
		t.Fatal("replacement module was not stopped")
	}
}

func TestRuntimeRecoveryFlagsAreValidatedAndPassedToHost(t *testing.T) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var opts runtimeHostOptions
	if err := addRuntimeHostFlags(fs, &opts); err != nil {
		t.Fatal(err)
	}
	if err := fs.Parse([]string{"--reconnect-min-delay", "2s", "--reconnect-max-delay", "30s", "--session-open-timeout", "1m", "--refresh-margin", "40s"}); err != nil {
		t.Fatal(err)
	}
	if err := opts.validate(); err != nil {
		t.Fatal(err)
	}
	cfg := opts.config()
	if cfg.ReconnectMinDelay != 2*time.Second || cfg.ReconnectMaxDelay != 30*time.Second || cfg.SessionOpenTimeout != time.Minute || cfg.RefreshMargin != 40*time.Second {
		t.Fatal("recovery flags were not propagated")
	}
	opts.reconnectMaxDelay = time.Second
	if err := opts.validate(); err == nil {
		t.Fatal("accepted decreasing reconnect range")
	}
}

func TestCancelledRuntimeControllerRejectsOldGenerationMutation(t *testing.T) {
	store, err := kernel.OpenFeatureStore(t.TempDir() + "/features.json")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	controller := newRuntimeController(ctx, store, kernel.NewRegistry(nil), nil, nil)
	cancel()
	response := controller.HandleControl(controlplane.Request{Operation: controlplane.OperationCreate, Record: &kernel.FeatureRecord{ID: "linkmyphone.clipboard", Config: []byte("{}")}})
	if response.OK || len(store.List()) != 0 {
		t.Fatal("cancelled generation mutated desired state")
	}
}
