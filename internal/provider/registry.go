package provider

import (
	"fmt"
	"maps"
	"sync"
)

// Registry maps locationID → StorageProvider. Providers are registered at
// startup (one per connected location) and looked up by the bridge services.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]StorageProvider // key = locationID
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]StorageProvider)}
}

// Register adds or replaces the provider for a location.
func (r *Registry) Register(locationID string, p StorageProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[locationID] = p
}

// Unregister removes the provider for a location.
func (r *Registry) Unregister(locationID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, locationID)
}

// Get returns the provider for locationID or an error if not found.
func (r *Registry) Get(locationID string) (StorageProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[locationID]
	if !ok {
		return nil, fmt.Errorf("no provider registered for location %q", locationID)
	}
	return p, nil
}

// All returns a snapshot of all registered providers.
func (r *Registry) All() map[string]StorageProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]StorageProvider, len(r.providers))
	maps.Copy(out, r.providers)
	return out
}
