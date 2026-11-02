package agent

import (
	"context"
	"fmt"
	"sync"
)

// Spec describes the Agent a caller wants created.
//
// It carries facts, not implementation choices: the driver, provider and tool
// set are resolved by whoever implements Factory. That keeps a caller from
// naming a concrete driver while still letting it state what the Agent is for.
type Spec struct {
	// Scope is the scope key the Agent becomes visible in. It is required: an
	// Agent without a scope could see the wrong tools.
	Scope ScopeKey
	// SessionID is the log this Agent records into. A driver decides what that
	// means; the contract only requires it to be named.
	SessionID string
	// Provider is the model route to use, when the caller wants to pin it.
	Provider string
	// Model is the model identifier, when the caller wants to pin it.
	Model string
	// InitialInput, when set, is delivered before the first turn starts. It
	// exists so a caller does not have to race Send against driver startup.
	InitialInput *Input
}

// Validate reports whether the spec is complete enough to create an Agent.
func (s Spec) Validate() error {
	if s.Scope == "" {
		return fmt.Errorf("%w: spec requires a scope", ErrInvalidValue)
	}
	if s.SessionID == "" {
		return fmt.Errorf("%w: spec requires a session id", ErrInvalidValue)
	}
	if s.InitialInput != nil {
		if err := s.InitialInput.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Factory creates and owns Agent instances.
//
// It is the seam a caller uses without knowing which driver is installed: the
// default driver in agentloop implements it, and so may an extension. A Factory
// owns every instance it created, which is what makes disposal a permission
// question with a definite answer.
type Factory interface {
	// Create builds one Agent. A non-nil error means no Agent was published:
	// creation is transactional, so a failure leaves nothing half-created
	// behind.
	Create(ctx context.Context, spec Spec) (Owner, error)
	// Active returns the handles this factory currently owns.
	Active() []Handle
	// DisposeAll stops every instance this factory created, joining errors.
	DisposeAll(ctx context.Context) error
}

// Registry resolves the Agent factory a component should use.
//
// It holds at most one factory at a time: two drivers competing to create
// Agents in one scope would make "which driver ran this turn" unattributable.
type Registry struct {
	mu      sync.RWMutex
	factory Factory
	closed  bool
}

// NewRegistry returns a registry with no factory installed.
func NewRegistry() *Registry { return &Registry{} }

// Install makes one factory the active driver.
func (r *Registry) Install(factory Factory) error {
	if factory == nil {
		return fmt.Errorf("%w: factory is nil", ErrInvalidValue)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("%w: registry is closed", ErrClosed)
	}
	if r.factory != nil {
		return fmt.Errorf("%w: a factory is already installed", ErrInvalidValue)
	}
	r.factory = factory
	return nil
}

// Factory returns the installed factory, reporting whether one is present.
func (r *Registry) Factory() (Factory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.factory, r.factory != nil
}

// Create delegates to the installed factory.
//
// An absent factory is refused explicitly rather than treated as an empty
// success, because "no driver is installed" is a configuration error the caller
// must see.
func (r *Registry) Create(ctx context.Context, spec Spec) (Owner, error) {
	r.mu.RLock()
	factory := r.factory
	closed := r.closed
	r.mu.RUnlock()
	if closed {
		return nil, ErrClosed
	}
	if factory == nil {
		return nil, fmt.Errorf("%w: no factory installed", ErrInvalidValue)
	}
	return factory.Create(ctx, spec)
}

// Close disposes every instance the installed factory owns and forgets it.
//
// It is idempotent. The factory is cleared only after its instances are
// released, so a concurrent Create cannot slip into a factory that is already
// being torn down.
func (r *Registry) Close(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	factory := r.factory
	r.mu.Unlock()
	if factory == nil {
		return nil
	}
	err := factory.DisposeAll(ctx)
	r.mu.Lock()
	r.factory = nil
	r.mu.Unlock()
	return err
}

// Closed reports whether the registry has been closed.
func (r *Registry) Closed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.closed
}
