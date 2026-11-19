package provider

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// Loader rebuilds and registers the provider for a location whose earlier
// restore attempt failed.
type Loader func(ctx context.Context, locationID string) (StorageProvider, error)

// Registry maps locationID → StorageProvider. Providers are registered at
// startup (one per connected location) and looked up by the bridge services.
// A Loader repairs a missing entry on first use, so a failed startup restore
// does not disable the location until the next launch.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]StorageProvider // key = locationID
	loader    Loader
	loadMu    sync.Mutex // serializes lazy rebuilds
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]StorageProvider)}
}

// SetLoader installs the fallback Get uses when a location has no provider.
func (r *Registry) SetLoader(loader Loader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loader = loader
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

// Get returns the provider for locationID. On a miss it asks the Loader to
// rebuild the provider, registering the result for later lookups.
func (r *Registry) Get(locationID string) (StorageProvider, error) {
	r.mu.RLock()
	p, ok := r.providers[locationID]
	loader := r.loader
	r.mu.RUnlock()
	if ok {
		return p, nil
	}
	if loader == nil {
		return nil, fmt.Errorf("no provider registered for location %q", locationID)
	}

	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	r.mu.RLock()
	p, ok = r.providers[locationID]
	r.mu.RUnlock()
	if ok {
		return p, nil
	}
	p, err := loader(context.Background(), locationID)
	if err != nil {
		return nil, err
	}
	r.Register(locationID, p)
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
