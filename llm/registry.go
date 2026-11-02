package llm

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// ProviderRegistry holds the routes a process can address.
//
// It is safe for concurrent use. Registration and lookup are separately locked
// from each other, and a closed registry refuses further registration so a
// shutdown cannot be raced by a late plugin.
type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	closed    atomic.Bool
}

// NewProviderRegistry returns an empty registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{providers: make(map[string]Provider)}
}

// Register adds one provider under its route key.
//
// A nil provider, an empty route key or a duplicate route key is refused: the
// reference treats a duplicate provider as a configuration error rather than
// letting the last registration win silently.
func (r *ProviderRegistry) Register(provider Provider) error {
	if r.closed.Load() {
		return ErrProviderClosed
	}
	if provider == nil {
		return fmt.Errorf("%w: provider is nil", ErrInvalidValue)
	}
	info := provider.Info()
	if info.ID == "" {
		return fmt.Errorf("%w: provider requires an id", ErrInvalidValue)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[info.ID]; exists {
		return fmt.Errorf("%w: provider %q is already registered", ErrInvalidValue, info.ID)
	}
	r.providers[info.ID] = provider
	return nil
}

// Unregister removes one route, reporting whether it was present.
func (r *ProviderRegistry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[id]; !exists {
		return false
	}
	delete(r.providers, id)
	return true
}

// Lookup returns the provider for a route. The boolean is false when no
// provider owns the route; the error is reserved for a closed registry.
func (r *ProviderRegistry) Lookup(id string) (Provider, bool, error) {
	if r.closed.Load() {
		return nil, false, ErrProviderClosed
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, exists := r.providers[id]
	return provider, exists, nil
}

// List returns the registered provider descriptions sorted by route key, so a
// diagnostic listing is deterministic rather than map-order dependent.
func (r *ProviderRegistry) List() []ProviderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]ProviderInfo, 0, len(r.providers))
	for _, provider := range r.providers {
		infos = append(infos, provider.Info())
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}

// Generate resolves the request's route and starts an attempt.
//
// It refuses an unknown route and a request that needs an undeclared capability
// before any attempt begins, so a caller learns about a routing mistake without
// paying for a request.
func (r *ProviderRegistry) Generate(ctx context.Context, request Request) (Stream, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	provider, exists, err := r.Lookup(request.Provider)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, request.Provider)
	}
	info := provider.Info()
	if len(request.Messages) > 0 && !info.Supports(CapabilityStreaming) {
		return nil, &UnsupportedCapabilityError{Provider: info.ID, Capability: CapabilityStreaming}
	}
	return provider.Generate(ctx, request)
}

// Close marks the registry closed and releases every provider that implements
// Closer. Errors are joined rather than dropped, so one failing release does
// not hide the others.
func (r *ProviderRegistry) Close() error {
	if !r.closed.CompareAndSwap(false, true) {
		return nil
	}
	r.mu.Lock()
	providers := make([]Provider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.providers = make(map[string]Provider)
	r.mu.Unlock()
	var errs []error
	for _, provider := range providers {
		if closer, ok := provider.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
