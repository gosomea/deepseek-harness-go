package loader

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

var (
	// ErrUnknownPlugin reports a configuration entry naming a plugin the catalog
	// does not hold. It is distinct from ErrDuplicateID and ErrInvalidConfig.
	ErrUnknownPlugin = errors.New("loader: unknown plugin")
	// ErrInvalidConfig reports a configuration document or entry field that the
	// codec rejected before any plugin was constructed.
	ErrInvalidConfig = errors.New("loader: invalid config")
)

// Factory builds a plugin for one configured entry. The catalog stores factories
// under the name that configuration documents use, keeping the plugin value and
// the entry that configures it separate.
type Factory func() cordis.Plugin

// Catalog maps plugin names to factories. Explicit registration replaces dynamic
// module resolution: an entry can only name a plugin the host compiled in.
//
// Catalog is safe for concurrent lookup after the host finished registering.
// Register and Lookup share a mutex, so a host may also register while other
// goroutines resolve.
type Catalog struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewCatalog creates an empty catalog. Register every plugin the host compiled
// in before loading a document that names it.
func NewCatalog() *Catalog {
	return &Catalog{factories: map[string]Factory{}}
}

// Register adds a factory under name. An empty name or nil factory is rejected;
// re-registering an existing name replaces the factory, which is how a host
// swaps a provider implementation without changing the configuration document.
func (c *Catalog) Register(name string, factory Factory) error {
	if name == "" {
		return fmt.Errorf("%w: plugin name is empty", ErrInvalidConfig)
	}
	if factory == nil {
		return fmt.Errorf("%w: factory for %q is nil", ErrInvalidConfig, name)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.factories[name] = factory
	return nil
}

// Lookup returns the factory registered under name. The error wraps
// ErrUnknownPlugin so callers can report the missing name as a configuration
// failure rather than a wiring failure.
func (c *Catalog) Lookup(name string) (Factory, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	factory, ok := c.factories[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownPlugin, name)
	}
	return factory, nil
}

// Has reports whether name is registered.
func (c *Catalog) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.factories[name]
	return ok
}

// Names lists registered plugin names in sorted order for deterministic
// diagnostics and configuration dumps.
func (c *Catalog) Names() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.factories))
	for name := range c.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
