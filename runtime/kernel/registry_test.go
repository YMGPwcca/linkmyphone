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

	mu           sync.Mutex
	startCount   int
	stopCount    int
	errors       chan error
	startEntered chan struct{}
	startRelease chan struct{}
}

func (m *fakeModule) Manifest() Manifest { return m.manifest }

func (m *fakeModule) ValidateConfig(config json.RawMessage) error {
	var value map[string]any
	return json.Unmarshal(config, &value)
}

func (m *fakeModule) Start(ctx context.Context, _ json.RawMessage, _ Reporter) (Instance, error) {
	m.mu.Lock()
	m.startCount++
	errorsCh := make(chan error, 1)
	m.errors = errorsCh
	entered := m.startEntered
	release := m.startRelease
	m.mu.Unlock()

	if entered != nil {
		close(entered)
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &fakeInstance{module: m, errors: errorsCh}, nil
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

func TestRegistryRejectsStoppingRequiredProviderWhileDependentReady(t *testing.T) {
	provider := &fakeModule{manifest: testManifest(t, "phonelink.provider", "test.provider")}
	consumer := &fakeModule{manifest: testManifest(
		t,
		"phonelink.consumer",
		"test.consumer",
		Dependency{ID: "phonelink.provider", MinimumVersion: "1.0.0"},
	)}

	registry := NewRegistry(nil)
	for _, module := range []*fakeModule{consumer, provider} {
		if err := registry.Create(module, FeatureRecord{
			ID:      module.manifest.ID,
			Enabled: true,
			Config:  json.RawMessage(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.StartEnabled(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := registry.Stop(context.Background(), provider.manifest.ID); err == nil {
		t.Fatal("expected provider stop to be blocked by ready dependent")
	}
	if err := registry.Stop(context.Background(), consumer.manifest.ID); err != nil {
		t.Fatal(err)
	}
	if err := registry.Stop(context.Background(), provider.manifest.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryRejectsStartUsingDeletedEntry(t *testing.T) {
	module := &fakeModule{manifest: testManifest(t, "phonelink.deleted", "test.deleted")}
	registry := NewRegistry(nil)
	if err := registry.Create(module, FeatureRecord{
		ID:      module.manifest.ID,
		Enabled: true,
		Config:  json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}

	entry, err := registry.entry(module.manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Delete(context.Background(), module.manifest.ID); err != nil {
		t.Fatal(err)
	}

	if err := registry.startEntry(context.Background(), entry); !errors.Is(err, ErrFeatureNotFound) {
		t.Fatalf("stale start err=%v", err)
	}
}

func TestRegistryBlocksProviderStopWhileDependentStarting(t *testing.T) {
	provider := &fakeModule{manifest: testManifest(t, "phonelink.provider", "test.provider")}
	consumer := &fakeModule{
		manifest: testManifest(
			t,
			"phonelink.consumer",
			"test.consumer",
			Dependency{ID: "phonelink.provider", MinimumVersion: "1.0.0"},
		),
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
	}

	registry := NewRegistry(nil)
	for _, module := range []*fakeModule{provider, consumer} {
		if err := registry.Create(module, FeatureRecord{
			ID:      module.manifest.ID,
			Enabled: true,
			Config:  json.RawMessage(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Start(context.Background(), provider.manifest.ID); err != nil {
		t.Fatal(err)
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- registry.Start(context.Background(), consumer.manifest.ID)
	}()

	select {
	case <-consumer.startEntered:
	case <-time.After(time.Second):
		t.Fatal("dependent did not enter Starting")
	}

	if err := registry.Stop(context.Background(), provider.manifest.ID); err == nil {
		t.Fatal("provider stop must be blocked while required dependent is Starting")
	}

	close(consumer.startRelease)
	select {
	case err := <-startDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dependent start did not finish")
	}

	if err := registry.Stop(context.Background(), consumer.manifest.ID); err != nil {
		t.Fatal(err)
	}
	if err := registry.Stop(context.Background(), provider.manifest.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryDegradesRequiredDependentsTransitivelyOnFailure(t *testing.T) {
	provider := &fakeModule{manifest: testManifest(t, "phonelink.provider", "test.provider")}
	middle := &fakeModule{manifest: testManifest(
		t,
		"phonelink.middle",
		"test.middle",
		Dependency{ID: "phonelink.provider", MinimumVersion: "1.0.0"},
	)}
	leaf := &fakeModule{manifest: testManifest(
		t,
		"phonelink.leaf",
		"test.leaf",
		Dependency{ID: "phonelink.middle", MinimumVersion: "1.0.0"},
	)}

	registry := NewRegistry(nil)
	for _, module := range []*fakeModule{leaf, middle, provider} {
		if err := registry.Create(module, FeatureRecord{
			ID:      module.manifest.ID,
			Enabled: true,
			Config:  json.RawMessage(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.StartEnabled(context.Background()); err != nil {
		t.Fatal(err)
	}

	provider.mu.Lock()
	providerErrors := provider.errors
	provider.mu.Unlock()
	providerErrors <- errors.New("provider boom")

	select {
	case runtimeErr := <-registry.Errors():
		if runtimeErr.ModuleID != provider.manifest.ID {
			t.Fatalf("runtimeErr=%#v", runtimeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("provider failure was not surfaced")
	}

	for _, id := range []string{middle.manifest.ID, leaf.manifest.ID} {
		snapshot, err := registry.Read(id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.State != StateDegraded || snapshot.LastError == "" {
			t.Fatalf("%s snapshot=%#v", id, snapshot)
		}
	}
	for _, capability := range []string{"test.provider", "test.middle", "test.leaf"} {
		if _, err := registry.Capabilities().Get(capability); err == nil {
			t.Fatalf("capability %s remained live after dependency failure", capability)
		}
	}

	if err := registry.StopAll(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryProviderFailureInvalidatesDependentStartCompletion(t *testing.T) {
	provider := &fakeModule{manifest: testManifest(t, "phonelink.provider", "test.provider")}
	consumer := &fakeModule{
		manifest: testManifest(
			t,
			"phonelink.consumer",
			"test.consumer",
			Dependency{ID: "phonelink.provider", MinimumVersion: "1.0.0"},
		),
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
	}

	registry := NewRegistry(nil)
	for _, module := range []*fakeModule{provider, consumer} {
		if err := registry.Create(module, FeatureRecord{
			ID:      module.manifest.ID,
			Enabled: true,
			Config:  json.RawMessage(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Start(context.Background(), provider.manifest.ID); err != nil {
		t.Fatal(err)
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- registry.Start(context.Background(), consumer.manifest.ID)
	}()
	select {
	case <-consumer.startEntered:
	case <-time.After(time.Second):
		t.Fatal("dependent did not enter Start")
	}

	provider.mu.Lock()
	providerErrors := provider.errors
	provider.mu.Unlock()
	providerErrors <- errors.New("provider failed during dependent start")

	select {
	case <-registry.Errors():
	case <-time.After(time.Second):
		t.Fatal("provider failure was not surfaced")
	}

	close(consumer.startRelease)
	select {
	case err := <-startDone:
		if err == nil {
			t.Fatal("stale dependent start unexpectedly reached Ready")
		}
	case <-time.After(time.Second):
		t.Fatal("dependent start completion did not return")
	}

	snapshot, err := registry.Read(consumer.manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != StateDegraded {
		t.Fatalf("consumer snapshot=%#v", snapshot)
	}
	if _, err := registry.Capabilities().Get("test.consumer"); err == nil {
		t.Fatal("consumer capability became live after stale start completion")
	}

	consumer.mu.Lock()
	stops := consumer.stopCount
	consumer.mu.Unlock()
	if stops != 1 {
		t.Fatalf("stale consumer instance rollback stops=%d want=1", stops)
	}

	// The invalidated consumer is Degraded but owns no live instance. It must
	// not hold the failed provider hostage during explicit teardown.
	if err := registry.Stop(context.Background(), provider.manifest.ID); err != nil {
		t.Fatalf("stop failed provider after stale dependent rollback: %v", err)
	}
	if err := registry.Stop(context.Background(), consumer.manifest.ID); err != nil {
		t.Fatalf("stop instance-less degraded consumer: %v", err)
	}
}

func TestRegistryUpdatePreservesStoppedLifecycleState(t *testing.T) {
	module := &fakeModule{manifest: testManifest(t, "phonelink.stopped", "test.stopped")}
	registry := NewRegistry(nil)
	if err := registry.Create(module, FeatureRecord{
		ID:      module.manifest.ID,
		Enabled: true,
		Config:  json.RawMessage(`{"value":1}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Start(context.Background(), module.manifest.ID); err != nil {
		t.Fatal(err)
	}
	if err := registry.Stop(context.Background(), module.manifest.ID); err != nil {
		t.Fatal(err)
	}

	enabled := false
	config := json.RawMessage(`{"value":2}`)
	snapshot, err := registry.Update(module.manifest.ID, &enabled, &config)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Enabled {
		t.Fatalf("snapshot enabled=%t", snapshot.Enabled)
	}
	if snapshot.State != StateStopped {
		t.Fatalf("snapshot state=%s want=%s", snapshot.State, StateStopped)
	}
	if string(snapshot.Config) != `{"value":2}` {
		t.Fatalf("snapshot config=%s", snapshot.Config)
	}
}
