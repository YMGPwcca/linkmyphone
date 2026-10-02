package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/YMGPwcca/linkmyphone/features"
	"github.com/YMGPwcca/linkmyphone/runtime/controlplane"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
	"github.com/YMGPwcca/linkmyphone/runtime/phonehost"
)

type fakeLiveModule struct {
	manifest kernel.Manifest

	mu         sync.Mutex
	startCount int
	stopCount  int
}

func (m *fakeLiveModule) Manifest() kernel.Manifest { return m.manifest }

func (m *fakeLiveModule) ValidateConfig(raw json.RawMessage) error {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value == nil {
		return errors.New("config must be object")
	}
	return nil
}

func (m *fakeLiveModule) Start(
	context.Context,
	json.RawMessage,
	kernel.Reporter,
) (kernel.Instance, error) {
	m.mu.Lock()
	m.startCount++
	m.mu.Unlock()
	return &fakeLiveInstance{
		module: m,
		errors: make(chan error),
	}, nil
}

type fakeLiveInstance struct {
	module *fakeLiveModule
	errors chan error
	once   sync.Once
}

func (i *fakeLiveInstance) Capabilities() []kernel.LiveCapability { return nil }
func (i *fakeLiveInstance) Errors() <-chan error                 { return i.errors }

func (i *fakeLiveInstance) Stop(context.Context) error {
	i.once.Do(func() {
		i.module.mu.Lock()
		i.module.stopCount++
		i.module.mu.Unlock()
		close(i.errors)
	})
	return nil
}

func testLiveManifest() kernel.Manifest {
	manifest := kernel.Manifest{
		SchemaVersion: "1.0",
		ID:            "linkmyphone.test",
		Version:       "1.0.0",
		Kind:          "builtin",
	}
	manifest.Runtime.APIVersion = "1.0"
	manifest.Metadata.DisplayName = "Test"
	manifest.Metadata.Description = "Test live feature"
	manifest.Metadata.DiagnosticLabel = "linkmyphone.test"
	return manifest
}

func newTestRuntimeController(t *testing.T) (*runtimeController, *fakeLiveModule) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "features.json")
	store, err := kernel.OpenFeatureStore(path)
	if err != nil {
		t.Fatal(err)
	}
	registry := kernel.NewRegistry(nil)
	module := &fakeLiveModule{manifest: testLiveManifest()}
	if err := module.manifest.Validate(); err != nil {
		t.Fatal(err)
	}

	controller := newRuntimeController(context.Background(), store, registry, (*phonehost.Session)(nil), nil)
	controller.resolve = func(id string) (features.Definition, error) {
		if id != module.manifest.ID {
			return features.Definition{}, errors.New("unknown test feature")
		}
		return features.Definition{
			Manifest:      module.manifest,
			DefaultConfig: json.RawMessage(`{"value":1}`),
			Validate:      module.ValidateConfig,
			Build: func(*phonehost.Session) (kernel.Module, error) {
				return module, nil
			},
		}, nil
	}
	return controller, module
}

func TestRuntimeControllerLiveCRUD(t *testing.T) {
	controller, module := newTestRuntimeController(t)

	create := controller.HandleControl(controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationCreate,
		Record: &kernel.FeatureRecord{
			ID:      "linkmyphone.test",
			Enabled: true,
			Config:  json.RawMessage(`{"value":1}`),
		},
	})
	if !create.OK || create.Record == nil || create.Snapshot == nil {
		t.Fatalf("create=%#v", create)
	}
	if create.Snapshot.State != kernel.StateReady {
		t.Fatalf("create state=%s", create.Snapshot.State)
	}

	config := json.RawMessage(`{"value":2}`)
	update := controller.HandleControl(controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationUpdate,
		ID:        "linkmyphone.test",
		Config:    &config,
	})
	if !update.OK || update.Record == nil || update.Snapshot == nil {
		t.Fatalf("update=%#v", update)
	}
	if update.Snapshot.State != kernel.StateReady {
		t.Fatalf("update state=%s", update.Snapshot.State)
	}

	disabled := false
	disable := controller.HandleControl(controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationUpdate,
		ID:        "linkmyphone.test",
		Enabled:   &disabled,
	})
	if !disable.OK || disable.Record == nil || disable.Snapshot == nil {
		t.Fatalf("disable=%#v", disable)
	}
	if disable.Record.Enabled || disable.Snapshot.State != kernel.StateStopped {
		t.Fatalf("disable record=%#v snapshot=%#v", disable.Record, disable.Snapshot)
	}

	enabled := true
	enable := controller.HandleControl(controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationUpdate,
		ID:        "linkmyphone.test",
		Enabled:   &enabled,
	})
	if !enable.OK || enable.Record == nil || enable.Snapshot == nil {
		t.Fatalf("enable=%#v", enable)
	}
	if !enable.Record.Enabled || enable.Snapshot.State != kernel.StateReady {
		t.Fatalf("enable record=%#v snapshot=%#v", enable.Record, enable.Snapshot)
	}

	list := controller.HandleControl(controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationList,
	})
	if !list.OK || len(list.Records) != 1 || len(list.Snapshots) != 1 {
		t.Fatalf("list=%#v", list)
	}

	get := controller.HandleControl(controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationGet,
		ID:        "linkmyphone.test",
	})
	if !get.OK || get.Record == nil || get.Snapshot == nil {
		t.Fatalf("get=%#v", get)
	}

	deleted := controller.HandleControl(controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationDelete,
		ID:        "linkmyphone.test",
	})
	if !deleted.OK {
		t.Fatalf("delete=%#v", deleted)
	}
	if _, err := controller.store.Read("linkmyphone.test"); !errors.Is(err, kernel.ErrFeatureNotFound) {
		t.Fatalf("store read after delete err=%v", err)
	}
	if _, err := controller.registry.Read("linkmyphone.test"); !errors.Is(err, kernel.ErrFeatureNotFound) {
		t.Fatalf("registry read after delete err=%v", err)
	}

	module.mu.Lock()
	starts := module.startCount
	stops := module.stopCount
	module.mu.Unlock()
	if starts != 3 || stops != 3 {
		t.Fatalf("starts=%d stops=%d want=3/3", starts, stops)
	}
}

func TestRuntimeControllerLoadsDisabledFeatureWithoutStartingIt(t *testing.T) {
	controller, module := newTestRuntimeController(t)
	if err := controller.store.Create(kernel.FeatureRecord{
		ID:      "linkmyphone.test",
		Enabled: false,
		Config:  json.RawMessage(`{"value":1}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := controller.load(context.Background()); err != nil {
		t.Fatal(err)
	}

	snapshot, err := controller.registry.Read("linkmyphone.test")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != kernel.StateValidated || snapshot.Enabled {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	module.mu.Lock()
	starts := module.startCount
	module.mu.Unlock()
	if starts != 0 {
		t.Fatalf("disabled module starts=%d", starts)
	}
}
