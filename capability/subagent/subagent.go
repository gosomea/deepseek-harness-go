package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gosomea/deepseek-harness-go/agent"
)

// Errors this package returns.
var (
	// ErrInvalidSpec reports a spawn request that cannot be honoured.
	ErrInvalidSpec = errors.New("subagent: invalid spec")
	// ErrUnsupported reports a capability a provider does not declare.
	ErrUnsupported = errors.New("subagent: capability not supported")
	// ErrProviderNotFound reports a route no registered provider owns.
	ErrProviderNotFound = errors.New("subagent: provider not found")
	// ErrRegistryClosed reports use of a registry after it was closed.
	ErrRegistryClosed = errors.New("subagent: registry closed")
	// ErrDepthExceeded reports a spawn that would exceed the depth limit.
	ErrDepthExceeded = errors.New("subagent: depth limit exceeded")
	// ErrDuplicateProvider reports two providers registered under one id.
	ErrDuplicateProvider = errors.New("subagent: duplicate provider id")
)

// Capability names one feature a subagent provider may declare.
//
// It is declared rather than probed because a caller must be able to decide
// before spawning whether the provider can do what is asked. A provider that
// cannot fork must not be sent a fork request and then fail somewhere deep.
type Capability string

// The subagent capabilities this package understands.
const (
	// CapabilitySpawn means the provider can start a fresh child Agent.
	CapabilitySpawn Capability = "spawn"
	// CapabilityFork means the provider can start a child that inherits a prefix
	// of the parent's history as an inheritance fact, not as new child facts.
	CapabilityFork Capability = "fork"
	// CapabilityContinue means the provider can continue an existing child
	// across a turn boundary.
	CapabilityContinue Capability = "continue"
)

// Mode is how a child is created relative to its parent.
type Mode string

// The creation modes this package names.
const (
	// ModeSpawn creates a fresh child with no inherited history.
	ModeSpawn Mode = "spawn"
	// ModeFork creates a child that inherits a prefix of the parent's history.
	ModeFork Mode = "fork"
)

// Spec describes one child the caller wants created.
//
// It carries facts, not implementation choices: the parent, the mode, and the
// cut point for a fork. A caller that asks to fork must say where the prefix
// ends, so the child's inherited facts are exact rather than "whatever the
// parent had at some moment".
type Spec struct {
	// Parent is the parent Agent's id. Required.
	Parent agent.ID
	// Mode is spawn or fork.
	Mode Mode
	// InheritThrough names the parent event sequence the child inherits, for a
	// fork. It is required for a fork and ignored for a spawn.
	InheritThrough int
	// Depth is the child's depth below the root (root is 0). It is derived by
	// the caller, not guessed here.
	Depth int
	// ToolScope names the scope the child sees. Empty inherits the parent's.
	ToolScope agent.ScopeKey
}

// Validate reports whether the spec can be honoured.
func (s Spec) Validate() error {
	if s.Parent.IsZero() {
		return fmt.Errorf("%w: spec requires a parent", ErrInvalidSpec)
	}
	switch s.Mode {
	case ModeSpawn:
		if s.InheritThrough != 0 {
			return fmt.Errorf("%w: a spawn must not name an inheritance cut", ErrInvalidSpec)
		}
	case ModeFork:
		if s.InheritThrough <= 0 {
			return fmt.Errorf("%w: a fork requires a positive inheritance cut", ErrInvalidSpec)
		}
	default:
		return fmt.Errorf("%w: unknown mode %q", ErrInvalidSpec, s.Mode)
	}
	if s.Depth < 0 {
		return fmt.Errorf("%w: depth must not be negative", ErrInvalidSpec)
	}
	return nil
}

// ProviderInfo describes one registered subagent route.
type ProviderInfo struct {
	// ID is the route key callers select with Spec-provider resolution.
	ID string
	// Name is the human-readable provider name for diagnostics.
	Name string
	// Capabilities is the declared feature set, in unspecified order.
	Capabilities []Capability
}

// Supports reports whether the route declares one capability.
func (i ProviderInfo) Supports(capability Capability) bool {
	for _, declared := range i.Capabilities {
		if declared == capability {
			return true
		}
	}
	return false
}

// Provider creates child Agents.
//
// The interface is deliberately narrow: a provider that also wants to persist,
// stream or report usage adds that through separate small interfaces, so a
// consumer that only spawns does not depend on them.
type Provider interface {
	// Spawn creates one child for the spec. A non-nil error means no child was
	// published: creation is transactional.
	Spawn(ctx context.Context, spec Spec) (agent.Owner, error)
	// Info describes the route and its declared capabilities.
	Info() ProviderInfo
}

// maxDepth is the default limit on how deep children may nest, so a runaway
// spawn cannot build an unbounded tree.
const maxDepth = 4

// Registry holds the named subagent providers a caller may resolve.
//
// It is a registry rather than a single provider because a product may offer
// several ways to run a child — an in-process one for tests, an external one for
// production — and the caller picks by name. Holding them under named routes
// keeps "which provider ran this child" attributable.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	order     []string
	closed    bool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

// Register adds one provider under its id.
func (r *Registry) Register(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("%w: provider is nil", ErrInvalidSpec)
	}
	info := provider.Info()
	if strings.TrimSpace(info.ID) == "" {
		return fmt.Errorf("%w: provider has no id", ErrInvalidSpec)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("%w", ErrRegistryClosed)
	}
	if _, exists := r.providers[info.ID]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateProvider, info.ID)
	}
	r.providers[info.ID] = provider
	r.order = append(r.order, info.ID)
	return nil
}

// Resolve returns the provider with an id, reporting whether it exists.
func (r *Registry) Resolve(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[id]
	return provider, ok
}

// Infos returns the registered routes in registration order.
func (r *Registry) Infos() []ProviderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderInfo, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.providers[id].Info())
	}
	return out
}

// Spawn resolves a provider and creates a child, enforcing the depth limit and
// the declared capability.
//
// The checks are here, before the provider runs, so a provider cannot be asked
// to do something it declared it cannot do: a fork request to a spawn-only
// provider is refused with ErrUnsupported rather than failing deeper.
func (r *Registry) Spawn(ctx context.Context, providerID string, spec Spec) (agent.Owner, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.Depth > maxDepth {
		return nil, fmt.Errorf("%w: depth %d exceeds limit %d", ErrDepthExceeded, spec.Depth, maxDepth)
	}
	provider, ok := r.Resolve(providerID)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, providerID)
	}
	info := provider.Info()
	var needed Capability
	switch spec.Mode {
	case ModeSpawn:
		needed = CapabilitySpawn
	case ModeFork:
		needed = CapabilityFork
	}
	if !info.Supports(needed) {
		return nil, fmt.Errorf("%w: provider %q does not support %s", ErrUnsupported, info.ID, needed)
	}
	return provider.Spawn(ctx, spec)
}

// Close marks the registry closed. It does not dispose children the providers
// already created: their owners are responsible for them.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// Closed reports whether Close was called.
func (r *Registry) Closed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.closed
}
