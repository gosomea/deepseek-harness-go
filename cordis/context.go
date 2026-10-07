package cordis

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// Scope is an opaque service namespace. Reuse a Scope to join isolated views.
type Scope struct{ token byte }

// NewScope creates a unique namespace token.
func NewScope() *Scope { return &Scope{} }

// Context is an immutable scope view bound to one plugin activation.
// It is distinct from context.Context; GoContext carries activation cancellation.
type Context struct {
	rt     *runtime
	fiber  *Fiber
	epoch  uint64
	scopes map[string]*Scope
	signal context.Context
}

type runtime struct {
	mu            sync.Mutex
	root          *Context
	fibers        []*Fiber
	services      map[serviceKey]*binding
	hooks         map[string][]*hook
	counter       uint64
	pumping       bool
	busy          bool
	callbacks     int
	idle          chan struct{}
	cleanupErrors []error
	logger        *slog.Logger
}

// Option customizes root construction through the supplied With functions.
type Option func(*runtime)

// WithLogger sets the root logger; nil retains slog.Default.
func WithLogger(logger *slog.Logger) Option {
	return func(r *runtime) {
		if logger != nil {
			r.logger = logger
		}
	}
}

// New creates an active root container with no global registration side effects.
func New(options ...Option) *Context {
	r := &runtime{services: make(map[serviceKey]*binding), hooks: make(map[string][]*hook), logger: slog.Default()}
	for _, option := range options {
		option(r)
	}
	r.idle = make(chan struct{})
	close(r.idle)
	signal, cancel := context.WithCancel(context.Background())
	f := &Fiber{rt: r, state: Active, epoch: 1, cancel: cancel, plugin: Plugin{Name: "root"}}
	c := &Context{rt: r, fiber: f, epoch: 1, scopes: make(map[string]*Scope), signal: signal}
	f.ctx = c
	r.root = c
	return c
}

// Root returns the container shared by all scopes and plugins in this tree.
func (c *Context) Root() *Context { return c.rt.root }

// Fiber returns the plugin instance owning this activation.
func (c *Context) Fiber() *Fiber { return c.fiber }

// GoContext is canceled before this activation unloads.
func (c *Context) GoContext() context.Context { return c.signal }

// Logger returns a slog logger with the owning plugin name attached.
func (c *Context) Logger() *slog.Logger { return c.rt.logger.With("plugin", c.fiber.plugin.Name) }

// Isolate replaces only this service's namespace. Other names retain their scopes.
func (c *Context) Isolate(name string, scope *Scope) *Context {
	if scope == nil {
		scope = NewScope()
	}
	scopes := make(map[string]*Scope, len(c.scopes)+1)
	for key, value := range c.scopes {
		scopes[key] = value
	}
	scopes[name] = scope
	return &Context{rt: c.rt, fiber: c.fiber, epoch: c.epoch, scopes: scopes, signal: c.signal}
}

func (c *Context) writableLocked() bool {
	f := c.fiber
	return c.epoch == f.epoch && !f.disposed && (f.state == Active || f.state == Loading)
}

// Plugin validates config and registers a child. Missing dependencies leave it
// Pending. A startup error returns both the failed Fiber and the error.
func (c *Context) Plugin(p Plugin, config any) (*Fiber, error) {
	value, err := p.validate(config)
	if err != nil {
		return nil, err
	}
	p.Inject = append([]string(nil), p.Inject...)
	r := c.rt
	r.mu.Lock()
	if !c.writableLocked() {
		r.mu.Unlock()
		return nil, ErrInactive
	}
	r.counter++
	f := &Fiber{rt: r, id: r.counter, plugin: p, parent: c, state: Pending, config: value, revision: 1}
	r.fibers = append(r.fibers, f)
	r.beginBusyLocked()
	r.mu.Unlock()
	r.reconcile()
	return f, f.Err()
}

// Inject registers a dependency-controlled callback named "inject".
func (c *Context) Inject(names []string, apply func(*Context, any) (Cleanup, error)) (*Fiber, error) {
	return c.Plugin(Plugin{Name: "inject", Inject: names, Apply: apply}, nil)
}

// Wait waits for lifecycle reconciliation, then reports current startup failures
// and accumulated teardown errors. Pending plugins do not block Wait.
// Do not call Wait from Apply or Cleanup: they drive the work being awaited.
func (c *Context) Wait(ctx context.Context) error {
	r := c.rt
	for {
		r.mu.Lock()
		if !r.busy {
			errs := append([]error(nil), r.cleanupErrors...)
			for _, f := range r.fibers {
				if f.state == Failed {
					errs = append(errs, f.err)
				}
			}
			r.mu.Unlock()
			return errors.Join(errs...)
		}
		idle := r.idle
		r.mu.Unlock()
		select {
		case <-idle:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Refresh re-evaluates every conditional service and reconciles the tree.
// Call it after external state read by a ProvideWhen predicate changes;
// consumers whose dependency became unavailable unload, and those whose
// dependency returned become Pending and reactivate. It is a no-op when the
// tree is already settled, so it is safe to call from any goroutine.
func (c *Context) Refresh() {
	c.rt.reconcile()
}

// Close permanently disposes the root tree and waits for all lifecycle cleanup.
// Call it from outside lifecycle callbacks. Cancellation bounds the wait only;
// plugin code must cooperate with GoContext cancellation to stop its own work.
func (c *Context) Close(ctx context.Context) error {
	r := c.rt
	r.mu.Lock()
	r.disposeTreeLocked(c.Root().fiber)
	r.beginBusyLocked()
	r.mu.Unlock()
	go r.reconcile()
	return c.Wait(ctx)
}

// Snapshot is a stable copy suitable for diagnostics.
type Snapshot struct {
	// ID identifies the plugin instance in this runtime.
	ID uint64
	// ParentID is zero when the parent is root.
	ParentID uint64
	// Name is the plugin diagnostic label.
	Name string
	// State is the lifecycle state at snapshot creation.
	State State
	// Error is the latest startup or final disposal failure.
	Error error
}

// Fibers returns live registry entries in registration order, excluding root.
// Disposed fibers leave the registry but remain readable through their handles.
func (c *Context) Fibers() []Snapshot {
	r := c.rt
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Snapshot, 0, len(r.fibers))
	for _, f := range r.fibers {
		result = append(result, Snapshot{f.id, f.parent.fiber.id, f.plugin.Name, f.state, f.err})
	}
	return result
}
