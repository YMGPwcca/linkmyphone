package kernel

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

type LiveCapability struct {
	ID              string `json:"id"`
	ContractVersion string `json:"contract_version"`
	ProviderID      string `json:"provider_id"`
}

type CapabilityRegistry struct {
	mu      sync.RWMutex
	entries map[string]LiveCapability
}

func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{entries: make(map[string]LiveCapability)}
}

func (r *CapabilityRegistry) RegisterAll(manifest Manifest, capabilities []LiveCapability) error {
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if capability.ProviderID != manifest.ID {
			return fmt.Errorf("kernel: capability %s provider does not match module %s", capability.ID, manifest.ID)
		}
		if !manifest.DeclaresCapability(capability.ID, capability.ContractVersion) {
			return fmt.Errorf("kernel: module %s did not declare live capability %s@%s", manifest.ID, capability.ID, capability.ContractVersion)
		}
		if _, duplicate := seen[capability.ID]; duplicate {
			return fmt.Errorf("kernel: duplicate live capability %s from %s", capability.ID, manifest.ID)
		}
		seen[capability.ID] = struct{}{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, capability := range capabilities {
		if existing, conflict := r.entries[capability.ID]; conflict && existing.ProviderID != manifest.ID {
			return fmt.Errorf("kernel: capability %s already provided by %s", capability.ID, existing.ProviderID)
		}
	}
	for _, capability := range capabilities {
		r.entries[capability.ID] = capability
	}
	return nil
}

func (r *CapabilityRegistry) RemoveProvider(providerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, capability := range r.entries {
		if capability.ProviderID == providerID {
			delete(r.entries, id)
		}
	}
}

func (r *CapabilityRegistry) Get(id string) (LiveCapability, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	capability, exists := r.entries[id]
	if !exists {
		return LiveCapability{}, errors.New("kernel: live capability not found")
	}
	return capability, nil
}

func (r *CapabilityRegistry) Snapshot() []LiveCapability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]LiveCapability, 0, len(r.entries))
	for _, capability := range r.entries {
		out = append(out, capability)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}
