package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type State string

const (
	DefaultRollbackStopTimeout = 10 * time.Second

	StateValidated State = "validated"
	StateResolved  State = "resolved"
	StateBlocked   State = "blocked"
	StateStarting  State = "starting"
	StateReady     State = "ready"
	StateDegraded  State = "degraded"
	StateFailed    State = "failed"
	StateStopping  State = "stopping"
	StateStopped   State = "stopped"
)

type Module interface {
	Manifest() Manifest
	ValidateConfig(json.RawMessage) error
	Start(context.Context, json.RawMessage, Reporter) (Instance, error)
}

type Instance interface {
	Capabilities() []LiveCapability
	Errors() <-chan error
	Stop(context.Context) error
}

type Snapshot struct {
	ID        string          `json:"id"`
	Version   string          `json:"version"`
	Enabled   bool            `json:"enabled"`
	State     State           `json:"state"`
	Config    json.RawMessage `json:"config"`
	Epoch     uint64          `json:"epoch"`
	LastError string          `json:"last_error,omitempty"`
}

type RuntimeError struct {
	ModuleID string
	Err      error
}

func (e RuntimeError) Error() string {
	return fmt.Sprintf("module %s: %v", e.ModuleID, e.Err)
}

type registryEntry struct {
	opMu sync.Mutex

	module   Module
	manifest Manifest
	config   json.RawMessage
	enabled  bool
	state    State
	epoch    uint64
	lastErr  string
	instance Instance
}

type Registry struct {
	mu           sync.RWMutex
	entries      map[string]*registryEntry
	capabilities *CapabilityRegistry
	reporter     Reporter
	errors       chan RuntimeError
	startOrder   []string
}

func NewRegistry(reporter Reporter) *Registry {
	return &Registry{
		entries:      make(map[string]*registryEntry),
		capabilities: NewCapabilityRegistry(),
		reporter:     reporter,
		errors:       make(chan RuntimeError, 32),
	}
}

func (r *Registry) Errors() <-chan RuntimeError {
	return r.errors
}

func (r *Registry) Capabilities() *CapabilityRegistry {
	return r.capabilities
}

func (r *Registry) Create(module Module, record FeatureRecord) error {
	if module == nil {
		return errors.New("kernel: module is required")
	}
	manifest := module.Manifest()
	if err := manifest.Validate(); err != nil {
		return err
	}
	if record.ID != manifest.ID {
		return fmt.Errorf("kernel: feature record %s does not match module %s", record.ID, manifest.ID)
	}
	record.Config = normalizeFeatureConfig(record.Config)
	if err := module.ValidateConfig(record.Config); err != nil {
		return fmt.Errorf("kernel: validate %s config: %w", manifest.ID, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[manifest.ID]; exists {
		return fmt.Errorf("%w: %s", ErrFeatureExists, manifest.ID)
	}
	r.entries[manifest.ID] = &registryEntry{
		module:   module,
		manifest: manifest,
		config:   cloneRawJSON(record.Config),
		enabled:  record.Enabled,
		state:    StateValidated,
	}
	return nil
}

func (r *Registry) Read(id string) (Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry := r.entries[id]
	if entry == nil {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrFeatureNotFound, id)
	}
	return snapshotEntry(entry), nil
}

func (r *Registry) List() []Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Snapshot, 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, snapshotEntry(entry))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *Registry) Update(id string, enabled *bool, config *json.RawMessage) (Snapshot, error) {
	entry, err := r.entry(id)
	if err != nil {
		return Snapshot{}, err
	}
	entry.opMu.Lock()
	defer entry.opMu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[id] != entry {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrFeatureNotFound, id)
	}
	if entry.instance != nil || entry.state == StateStarting || entry.state == StateStopping {
		return Snapshot{}, fmt.Errorf("kernel: module %s must be stopped before update", id)
	}

	nextConfig := cloneRawJSON(entry.config)
	if config != nil {
		nextConfig = normalizeFeatureConfig(*config)
	}
	if err := entry.module.ValidateConfig(nextConfig); err != nil {
		return Snapshot{}, fmt.Errorf("kernel: validate %s config: %w", id, err)
	}
	if enabled != nil {
		entry.enabled = *enabled
	}
	entry.config = nextConfig
	// Updating desired state/config for an already-stopped module must not
	// erase the lifecycle fact that it is stopped. Start() does not require
	// StateValidated and will transition through Resolved/Starting itself.
	// Other non-live terminal/error states are reset to Validated so an update
	// can clear stale failure/blocking diagnostics before the next start.
	if entry.state != StateStopped {
		entry.state = StateValidated
	}
	entry.lastErr = ""
	return snapshotEntry(entry), nil
}

func (r *Registry) Delete(ctx context.Context, id string) error {
	entry, err := r.entry(id)
	if err != nil {
		return err
	}
	entry.opMu.Lock()
	defer entry.opMu.Unlock()

	r.mu.RLock()
	current := r.entries[id]
	r.mu.RUnlock()
	if current != entry {
		return fmt.Errorf("%w: %s", ErrFeatureNotFound, id)
	}
	if err := r.stopEntryLocked(ctx, entry, true); err != nil {
		return err
	}

	r.mu.Lock()
	if current := r.entries[id]; current != entry {
		r.mu.Unlock()
		return fmt.Errorf("kernel: module %s changed during delete", id)
	}
	delete(r.entries, id)
	r.removeStartOrderLocked(id)
	r.mu.Unlock()
	r.capabilities.RemoveProvider(id)
	return nil
}

func (r *Registry) Start(ctx context.Context, id string) error {
	entry, err := r.entry(id)
	if err != nil {
		return err
	}
	return r.startEntry(ctx, entry)
}

func (r *Registry) Stop(ctx context.Context, id string) error {
	entry, err := r.entry(id)
	if err != nil {
		return err
	}
	return r.stopEntry(ctx, entry, true)
}

func (r *Registry) StartEnabled(ctx context.Context) error {
	pending := make(map[string]struct{})
	for _, snapshot := range r.List() {
		if snapshot.Enabled {
			pending[snapshot.ID] = struct{}{}
		}
	}

	for len(pending) != 0 {
		progress := false
		ids := make([]string, 0, len(pending))
		for id := range pending {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		for _, id := range ids {
			entry, err := r.entry(id)
			if err != nil {
				return err
			}
			ready, blockedReason := r.dependenciesReady(entry)
			if !ready {
				if blockedReason != "" {
					r.setBlocked(entry, blockedReason)
				}
				continue
			}
			if err := r.startEntry(ctx, entry); err != nil {
				return err
			}
			delete(pending, id)
			progress = true
		}
		if progress {
			continue
		}

		blocked := make([]string, 0, len(pending))
		for id := range pending {
			blocked = append(blocked, id)
		}
		sort.Strings(blocked)
		return fmt.Errorf("kernel: enabled modules cannot resolve dependencies: %s", strings.Join(blocked, ", "))
	}
	return nil
}

func (r *Registry) StopAll(ctx context.Context) error {
	r.mu.RLock()
	order := append([]string(nil), r.startOrder...)
	r.mu.RUnlock()

	var errs []error
	for i := len(order) - 1; i >= 0; i-- {
		entry, err := r.entry(order[i])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := r.stopEntry(ctx, entry, false); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Registry) startEntry(ctx context.Context, entry *registryEntry) error {
	entry.opMu.Lock()
	defer entry.opMu.Unlock()

	r.mu.Lock()
	if r.entries[entry.manifest.ID] != entry {
		r.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrFeatureNotFound, entry.manifest.ID)
	}
	if !entry.enabled {
		r.mu.Unlock()
		return fmt.Errorf("kernel: module %s is disabled", entry.manifest.ID)
	}
	if entry.instance != nil {
		if entry.state == StateReady {
			r.mu.Unlock()
			return nil
		}
		state := entry.state
		r.mu.Unlock()
		return fmt.Errorf("kernel: module %s has a live instance in state %s; stop it before restart", entry.manifest.ID, state)
	}
	if entry.state == StateStarting || entry.state == StateStopping {
		state := entry.state
		r.mu.Unlock()
		return fmt.Errorf("kernel: module %s is busy in state %s", entry.manifest.ID, state)
	}
	ready, reason := r.dependenciesReadyLocked(entry)
	if !ready {
		entry.state = StateBlocked
		entry.lastErr = reason
		r.mu.Unlock()
		if reason == "" {
			reason = "required dependency is not ready"
		}
		return fmt.Errorf("kernel: module %s blocked: %s", entry.manifest.ID, reason)
	}

	entry.state = StateResolved
	entry.epoch++
	epoch := entry.epoch
	config := cloneRawJSON(entry.config)
	entry.state = StateStarting
	entry.lastErr = ""
	r.mu.Unlock()

	instance, err := entry.module.Start(ctx, config, r.reporter)
	if err != nil {
		r.mu.Lock()
		if entry.epoch == epoch {
			entry.state = StateFailed
			entry.lastErr = err.Error()
		}
		r.mu.Unlock()
		return fmt.Errorf("kernel: start module %s: %w", entry.manifest.ID, err)
	}
	if instance == nil {
		err := errors.New("module returned nil instance")
		r.mu.Lock()
		if entry.epoch == epoch {
			entry.state = StateFailed
			entry.lastErr = err.Error()
		}
		r.mu.Unlock()
		return fmt.Errorf("kernel: start module %s: %w", entry.manifest.ID, err)
	}

	// A dependency can fail while Module.Start is executing. Reject the stale
	// completion before publishing any live capability; otherwise there is a
	// short window where a degraded instance becomes discoverable as Ready.
	r.mu.RLock()
	staleBeforeCapabilities := entry.epoch != epoch
	r.mu.RUnlock()
	if staleBeforeCapabilities {
		stopInstanceForRollback(instance)
		return fmt.Errorf("kernel: stale start completion for module %s", entry.manifest.ID)
	}

	if err := r.capabilities.RegisterAll(entry.manifest, instance.Capabilities()); err != nil {
		stopInstanceForRollback(instance)
		r.mu.Lock()
		if entry.epoch == epoch {
			entry.state = StateFailed
			entry.lastErr = err.Error()
		}
		r.mu.Unlock()
		return err
	}

	r.mu.Lock()
	if entry.epoch != epoch {
		r.mu.Unlock()
		r.capabilities.RemoveProvider(entry.manifest.ID)
		stopInstanceForRollback(instance)
		return fmt.Errorf("kernel: stale start completion for module %s", entry.manifest.ID)
	}
	entry.instance = instance
	entry.state = StateReady
	r.removeStartOrderLocked(entry.manifest.ID)
	r.startOrder = append(r.startOrder, entry.manifest.ID)
	r.mu.Unlock()

	go r.monitorInstance(entry, epoch, instance)
	Report(r.reporter, Event{
		ModuleID: entry.manifest.ID,
		Level:    "info",
		Message:  "module ready",
	})
	return nil
}

func (r *Registry) stopEntry(
	ctx context.Context,
	entry *registryEntry,
	enforceDependents bool,
) error {
	entry.opMu.Lock()
	defer entry.opMu.Unlock()
	r.mu.RLock()
	current := r.entries[entry.manifest.ID]
	r.mu.RUnlock()
	if current != entry {
		return fmt.Errorf("%w: %s", ErrFeatureNotFound, entry.manifest.ID)
	}
	return r.stopEntryLocked(ctx, entry, enforceDependents)
}

func (r *Registry) stopEntryLocked(
	ctx context.Context,
	entry *registryEntry,
	enforceDependents bool,
) error {
	r.mu.Lock()
	if enforceDependents {
		dependents := r.activeRequiredDependentsLocked(entry.manifest.ID)
		if len(dependents) != 0 {
			r.mu.Unlock()
			return fmt.Errorf(
				"kernel: module %s is required by active modules: %s",
				entry.manifest.ID,
				strings.Join(dependents, ", "),
			)
		}
	}
	instance := entry.instance
	if instance == nil {
		if entry.state != StateFailed {
			entry.state = StateStopped
		}
		r.mu.Unlock()
		r.capabilities.RemoveProvider(entry.manifest.ID)
		return nil
	}
	entry.epoch++
	entry.state = StateStopping
	entry.instance = nil
	r.removeStartOrderLocked(entry.manifest.ID)
	r.mu.Unlock()

	r.capabilities.RemoveProvider(entry.manifest.ID)
	err := instance.Stop(ctx)

	r.mu.Lock()
	if err != nil {
		entry.state = StateFailed
		entry.lastErr = err.Error()
	} else {
		entry.state = StateStopped
		entry.lastErr = ""
	}
	r.mu.Unlock()

	if err != nil {
		return fmt.Errorf("kernel: stop module %s: %w", entry.manifest.ID, err)
	}
	Report(r.reporter, Event{
		ModuleID: entry.manifest.ID,
		Level:    "info",
		Message:  "module stopped",
	})
	return nil
}

func (r *Registry) monitorInstance(entry *registryEntry, epoch uint64, instance Instance) {
	errorsCh := instance.Errors()
	if errorsCh == nil {
		return
	}
	err, ok := <-errorsCh
	if !ok || err == nil {
		return
	}

	r.mu.Lock()
	if entry.epoch != epoch {
		r.mu.Unlock()
		return
	}
	entry.state = StateFailed
	entry.lastErr = err.Error()
	degraded := r.degradeRequiredDependentsLocked(entry.manifest.ID, err)
	r.mu.Unlock()

	r.capabilities.RemoveProvider(entry.manifest.ID)
	for _, id := range degraded {
		r.capabilities.RemoveProvider(id)
		Report(r.reporter, Event{
			ModuleID: id,
			Level:    "warning",
			Message:  "module degraded after required dependency failure",
			Fields: map[string]string{
				"dependency": entry.manifest.ID,
			},
		})
	}

	runtimeErr := RuntimeError{ModuleID: entry.manifest.ID, Err: err}
	select {
	case r.errors <- runtimeErr:
	default:
	}
}

func (r *Registry) degradeRequiredDependentsLocked(
	failedProviderID string,
	cause error,
) []string {
	affected := map[string]struct{}{failedProviderID: {}}
	degraded := make([]string, 0)

	for {
		changed := false
		for id, candidate := range r.entries {
			if _, seen := affected[id]; seen {
				continue
			}
			active := candidate.instance != nil ||
				candidate.state == StateStarting ||
				candidate.state == StateReady ||
				candidate.state == StateDegraded
			if !active {
				continue
			}
			var brokenDependency string
			for _, dependency := range candidate.manifest.Dependencies.Required {
				if _, broken := affected[dependency.ID]; broken {
					brokenDependency = dependency.ID
					break
				}
			}
			if brokenDependency == "" {
				continue
			}

			affected[id] = struct{}{}
			changed = true
			if candidate.state == StateReady || candidate.state == StateStarting {
				if candidate.state == StateStarting {
					// Invalidate the in-flight Start completion. startEntry will
					// rollback the returned instance when its epoch no longer
					// matches instead of resurrecting this module as Ready.
					candidate.epoch++
				}
				candidate.state = StateDegraded
				candidate.lastErr = fmt.Sprintf(
					"required dependency %s became unavailable after %s failed: %v",
					brokenDependency,
					failedProviderID,
					cause,
				)
				degraded = append(degraded, id)
			}
		}
		if !changed {
			break
		}
	}
	sort.Strings(degraded)
	return degraded
}

func stopInstanceForRollback(instance Instance) {
	if instance == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultRollbackStopTimeout)
	defer cancel()
	_ = instance.Stop(ctx)
}

func (r *Registry) activeRequiredDependentsLocked(providerID string) []string {
	dependents := make([]string, 0)
	for id, candidate := range r.entries {
		if id == providerID {
			continue
		}
		// A dependent blocks provider teardown while it owns a live
		// instance, is still constructing one, or is in the middle of stopping
		// one. A degraded entry with no instance (for example an invalidated
		// Start completion) owns no runtime resource and must not block cleanup.
		active := candidate.instance != nil ||
			candidate.state == StateStarting ||
			candidate.state == StateStopping
		if !active {
			continue
		}
		for _, dependency := range candidate.manifest.Dependencies.Required {
			if dependency.ID == providerID {
				dependents = append(dependents, id)
				break
			}
		}
	}
	sort.Strings(dependents)
	return dependents
}

func (r *Registry) removeStartOrderLocked(id string) {
	for index, candidate := range r.startOrder {
		if candidate != id {
			continue
		}
		r.startOrder = append(r.startOrder[:index], r.startOrder[index+1:]...)
		return
	}
}

func (r *Registry) dependenciesReady(entry *registryEntry) (bool, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dependenciesReadyLocked(entry)
}

func (r *Registry) dependenciesReadyLocked(entry *registryEntry) (bool, string) {
	for _, dependency := range entry.manifest.Dependencies.Required {
		dep := r.entries[dependency.ID]
		if dep == nil {
			return false, "missing required dependency " + dependency.ID
		}
		if !versionCompatible(dep.manifest.Version, dependency.MinimumVersion) {
			return false, "incompatible required dependency " + dependency.ID
		}
		if dep.state != StateReady {
			return false, "required dependency " + dependency.ID + " is not ready"
		}
	}
	return true, ""
}

func (r *Registry) setBlocked(entry *registryEntry, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[entry.manifest.ID] != entry {
		return
	}
	entry.state = StateBlocked
	entry.lastErr = reason
}

func (r *Registry) entry(id string) (*registryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry := r.entries[id]
	if entry == nil {
		return nil, fmt.Errorf("%w: %s", ErrFeatureNotFound, id)
	}
	return entry, nil
}

func snapshotEntry(entry *registryEntry) Snapshot {
	return Snapshot{
		ID:        entry.manifest.ID,
		Version:   entry.manifest.Version,
		Enabled:   entry.enabled,
		State:     entry.state,
		Config:    cloneRawJSON(entry.config),
		Epoch:     entry.epoch,
		LastError: entry.lastErr,
	}
}

func versionCompatible(offered, minimum string) bool {
	offeredVersion, ok := parseSemverCore(offered)
	if !ok {
		return false
	}
	minimumVersion, ok := parseSemverCore(minimum)
	if !ok || offeredVersion[0] != minimumVersion[0] {
		return false
	}
	for i := 1; i < len(offeredVersion); i++ {
		if offeredVersion[i] > minimumVersion[i] {
			return true
		}
		if offeredVersion[i] < minimumVersion[i] {
			return false
		}
	}
	return true
}

func parseSemverCore(value string) ([3]int, bool) {
	var out [3]int
	core := strings.SplitN(strings.SplitN(value, "+", 2)[0], "-", 2)[0]
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return out, false
		}
		out[i] = number
	}
	return out, true
}
