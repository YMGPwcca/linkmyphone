package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeModule struct {
	manifest Manifest

	mu         sync.Mutex
	startCount int
	stopCount  int
	errors     chan error
}

func (m *fakeModule) Manifest() Manifest { return m.manifest }

func (m *fakeModule) ValidateConfig(config json.RawMessage) error {
	var value map[string]any
	return json.Unmarshal(config, &value)
}

func (m *fakeModule) Start(context.Context, json.RawMessage, Reporter) (Instance, error) {
	m.mu.Lock()
	m.startCount++
	m.mu.Unlock()
	m.errors = make(chan error, 1)
	return &fakeInstance{module: m, errors: m.errors}, nil
}

type fakeInstance struct {
	module *fakeModule
	errors chan error
}

func (i *fakeInstance) Capabilities() []LiveCapability {
	out := make([]LiveCapability, 0, len(i.module.manifest.Capabilities.Potential))
	for _, declared := range i.module.manifest.Capabilities.Potential {
		out = append(out, LiveCapability{
			ID:              declared.ID,
			ContractVersion: declared.ContractVersion,
			ProviderID:      i.module.manifest.ID,
		})
	}
	return out
}

func (i *fakeInstance) Errors() <-chan error { return i.errors }

func (i *fakeInstance) Stop(context.Context) error {
	i.module.mu.Lock()
	i.module.stopCount++
	i.module.mu.Unlock()
	close(i.errors)
	return nil
}

func testManifest(t *testing.T, id, capability string, required ...Dependency) Manifest {
	t.Helper()
	manifest, err := ParseManifestJSON([]byte(validManifestJSON))
	if err != nil {
		t.Fatal(err)
	}
	manifest.ID = id
	manifest.Metadata.DiagnosticLabel = id
	manifest.Capabilities.Potential = nil
	if capability != "" {
		manifest.Capabilities.Potential = []CapabilityDeclaration{{
			ID:              capability,
			ContractVersion: "1.0.0",
		}}
	}
	manifest.Dependencies.Required = append([]Dependency(nil), required...)
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestRegistryStartsDependenciesBeforeDependent(t *testing.T) {
	provider := &fakeModule{manifest: testManifest(t, "phonelink.provider", "test.provider")}
	consumer := &fakeModule{manifest: testManifest(
		t,
		"phonelink.consumer",
		"test.consumer",
		Dependency{ID: "phonelink.provider", MinimumVersion: "1.0.0"},
	)}

	registry := NewRegistry(nil)
	if err := registry.Create(consumer, FeatureRecord{ID: consumer.manifest.ID, Enabled: true, Config: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Create(provider, FeatureRecord{ID: provider.manifest.ID, Enabled: true, Config: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := registry.StartEnabled(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{provider.manifest.ID, consumer.manifest.ID} {
		snapshot, err := registry.Read(id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.State != StateReady {
			t.Fatalf("%s state=%s", id, snapshot.State)
		}
	}
	if _, err := registry.Capabilities().Get("test.provider"); err != nil {
		t.Fatal(err)
	}

	if err := registry.StopAll(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryBlocksMissingRequiredDependency(t *testing.T) {
	module := &fakeModule{manifest: testManifest(
		t,
		"phonelink.consumer",
		"test.consumer",
		Dependency{ID: "phonelink.missing", MinimumVersion: "1.0.0"},
	)}
	registry := NewRegistry(nil)
	if err := registry.Create(module, FeatureRecord{ID: module.manifest.ID, Enabled: true, Config: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := registry.StartEnabled(context.Background()); err == nil {
		t.Fatal("expected dependency failure")
	}
	snapshot, err := registry.Read(module.manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != StateBlocked {
		t.Fatalf("state=%s", snapshot.State)
	}
}

func TestRegistryIgnoresStaleInstanceErrorAfterStop(t *testing.T) {
	module := &fakeModule{manifest: testManifest(t, "phonelink.test", "test.echo")}
	registry := NewRegistry(nil)
	if err := registry.Create(module, FeatureRecord{ID: module.manifest.ID, Enabled: true, Config: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Start(context.Background(), module.manifest.ID); err != nil {
		t.Fatal(err)
	}

	oldErrors := module.errors
	if err := registry.Stop(context.Background(), module.manifest.ID); err != nil {
		t.Fatal(err)
	}
	// Stop closes the old error channel. A replacement instance gets a new epoch.
	if err := registry.Start(context.Background(), module.manifest.ID); err != nil {
		t.Fatal(err)
	}

	// A stale completion has no path to mutate the replacement epoch. The closed
	// old channel must not produce a runtime error either.
	select {
	case err := <-oldErrors:
		if err != nil {
			t.Fatalf("unexpected old instance error=%v", err)
		}
	default:
	}

	snapshot, err := registry.Read(module.manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != StateReady || snapshot.Epoch < 3 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}

func TestRegistrySurfacesCurrentInstanceFailure(t *testing.T) {
	module := &fakeModule{manifest: testManifest(t, "phonelink.test", "test.echo")}
	registry := NewRegistry(nil)
	if err := registry.Create(module, FeatureRecord{ID: module.manifest.ID, Enabled: true, Config: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Start(context.Background(), module.manifest.ID); err != nil {
		t.Fatal(err)
	}

	module.errors <- errors.New("boom")
	select {
	case runtimeErr := <-registry.Errors():
		if runtimeErr.ModuleID != module.manifest.ID {
			t.Fatalf("runtimeErr=%#v", runtimeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime error not surfaced")
	}
	snapshot, err := registry.Read(module.manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != StateFailed || snapshot.LastError == "" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}
