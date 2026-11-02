package agentloop

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// ErrInvalidConfig reports a driver configuration that cannot be used.
var ErrInvalidConfig = errors.New("agentloop: invalid configuration")

// ErrStepLimit reports a turn that hit the driver's step bound.
//
// It is a failure rather than a silent truncation: a model that keeps requesting
// tools must be visible to the caller, not quietly stopped.
var ErrStepLimit = errors.New("agentloop: step limit reached")

// Config describes the collaborators one driver uses.
//
// Every field is injected rather than constructed here, so the same driver works
// with a fake provider in a test and a real one in production, and so a test can
// supply a controllable clock through the log.
type Config struct {
	// Provider generates model responses. Required.
	Provider llm.Provider
	// Log is the authoritative session log. Required.
	Log *session.Log
	// Pipeline executes tool calls. Required when the model may request tools.
	Pipeline *tools.Pipeline
	// SystemFragments is the prompt fragment set rendered for each request.
	SystemFragments *fragmentSource
	// MaxSteps bounds the steps of one turn, so a model that keeps requesting
	// tools cannot loop forever. Zero means the default bound.
	MaxSteps int
}

// Validate reports whether the configuration can drive an Agent.
func (c Config) Validate() error {
	if c.Provider == nil {
		return fmt.Errorf("%w: a model provider is required", ErrInvalidConfig)
	}
	if c.Log == nil {
		return fmt.Errorf("%w: a session log is required", ErrInvalidConfig)
	}
	return nil
}

// Factory implements agent.Factory for the default driver.
//
// It owns the instances it created and is the only holder of their Owner
// capability, which is what makes "only the factory can dispose what it made" a
// property of the code rather than a convention.
type Factory struct {
	mu sync.Mutex
	// config is the driver configuration shared by every instance.
	config Config
	// instances maps each created Agent's id to its owner handle.
	instances map[agent.ID]*instance
	// order preserves creation order so Active and DisposeAll are deterministic.
	order []agent.ID
	// closed refuses new creation after shutdown.
	closed bool
}

// NewFactory returns a factory for the given configuration.
func NewFactory(config Config) (*Factory, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Factory{config: config, instances: map[agent.ID]*instance{}}, nil
}

// Config returns the driver configuration.
func (f *Factory) Config() Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.config
}

// Create builds, initializes and publishes one Agent.
//
// Creation is transactional: the instance is only recorded in the factory after
// its initialization succeeded, so a failure leaves no half-created Agent
// reachable through Active, and no Agent id is leaked to the caller. This is the
// observable difference between "creation failed" and "a broken Agent exists".
func (f *Factory) Create(ctx context.Context, spec agent.Spec) (agent.Owner, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil, fmt.Errorf("%w: factory is closed", agent.ErrClosed)
	}
	config := f.config
	f.mu.Unlock()

	instance, err := newInstance(config, spec)
	if err != nil {
		return nil, err
	}
	if err := instance.initialize(ctx); err != nil {
		// The failed instance still owns released-or-releasable resources, so it
		// is torn down here rather than dropped for the garbage collector to
		// find. A test asserts nothing remains reachable afterwards.
		_ = instance.dispose(ctx)
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.instances[instance.info.ID] = instance
	f.order = append(f.order, instance.info.ID)
	return instance, nil
}

// Active returns the handles of every instance this factory created.
func (f *Factory) Active() []agent.Handle {
	f.mu.Lock()
	defer f.mu.Unlock()
	handles := make([]agent.Handle, 0, len(f.order))
	for _, id := range f.order {
		if instance, ok := f.instances[id]; ok {
			handles = append(handles, instance)
		}
	}
	return handles
}

// Lookup returns the handle for one Agent id, reporting whether it exists.
//
// A lookup result is a Handle, never an Owner: finding an Agent by id is a
// read-side operation and must not confer the ability to destroy it. Returning
// the instance directly would defeat that, because the instance implements Owner
// as well; the returned value is therefore the narrower wrapper below.
func (f *Factory) Lookup(id agent.ID) (agent.Handle, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	instance, ok := f.instances[id]
	if !ok {
		return nil, false
	}
	return readOnlyHandle{instance: instance}, true
}

// DisposeAll stops every instance this factory owns, joining their errors.
//
// Instances are disposed in reverse creation order so that a child created last
// is released first, matching the ownership rule the rest of the project uses.
func (f *Factory) DisposeAll(ctx context.Context) error {
	f.mu.Lock()
	if f.closed && len(f.order) == 0 {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	ids := append([]agent.ID(nil), f.order...)
	f.mu.Unlock()

	var errs []error
	for index := len(ids) - 1; index >= 0; index-- {
		f.mu.Lock()
		instance, ok := f.instances[ids[index]]
		f.mu.Unlock()
		if !ok {
			continue
		}
		if err := instance.dispose(ctx); err != nil {
			errs = append(errs, err)
		}
		f.mu.Lock()
		delete(f.instances, ids[index])
		f.mu.Unlock()
	}
	f.mu.Lock()
	f.order = nil
	f.mu.Unlock()
	return errors.Join(errs...)
}

// Closed reports whether the factory refuses new creation.
func (f *Factory) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// Install registers this factory as the active driver in a registry.
func (f *Factory) Install(registry *agent.Registry) error {
	return registry.Install(f)
}

// readOnlyHandle is the narrow view a lookup returns.
//
// It forwards exactly the read/send/wait operations of agent.Handle and nothing
// else. The indirection is the mechanism, not decoration: without it a caller
// could type-assert the returned handle to agent.Owner and dispose an Agent it
// does not own.
type readOnlyHandle struct {
	// instance is the Agent this view refers to.
	instance *instance
}

// Info describes the Agent.
func (h readOnlyHandle) Info() agent.Info { return h.instance.Info() }

// State reports the lifecycle state.
func (h readOnlyHandle) State() agent.State { return h.instance.State() }

// Send delivers input to the Agent's inbox.
func (h readOnlyHandle) Send(ctx context.Context, input agent.Input) error {
	return h.instance.Send(ctx, input)
}

// Wait blocks until the Agent settles.
func (h readOnlyHandle) Wait(ctx context.Context) error { return h.instance.Wait(ctx) }

// Done reports disposal.
func (h readOnlyHandle) Done() <-chan struct{} { return h.instance.Done() }

// RunTurn drives one turn through the narrow view.
//
// Driving is forwarded but disposal is not: the contract restricts *destruction*
// of an instance a caller does not own, and a scheduler that found an Agent must
// still be able to pump it. Only Dispose and Cancel stay behind the factory's
// capability grant.
func (h readOnlyHandle) RunTurn(ctx context.Context) error { return h.instance.RunTurn(ctx) }

var (
	_ agent.Handle = readOnlyHandle{}
	_ Runner       = readOnlyHandle{}
)
