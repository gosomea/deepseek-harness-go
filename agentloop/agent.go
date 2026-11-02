package agentloop

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/agent"
)

// instance is one running Agent.
//
// It implements agent.Handle (read, send, wait, done), agent.Canceler (cancel
// without disposal) and agent.Owner (dispose). Keeping them on one type is what
// lets the factory hand out only the narrower interfaces: the same value, three
// different capabilities, granted deliberately.
type instance struct {
	// info describes the Agent and never changes.
	info agent.Info
	// config is the driver configuration this instance runs with.
	config Config
	// inbox holds input queued for the next step.
	inbox *inbox
	// state is the lifecycle state, guarded by mu.
	mu sync.Mutex
	// state is the current lifecycle state.
	state agent.State
	// done is closed exactly once, when the instance is disposed.
	done chan struct{}
	// disposed guards single-shot disposal.
	disposed bool
	// cancelled is the cancel function of the active turn, or nil when idle.
	cancelled context.CancelFunc
	// active reports whether a turn is currently running.
	active bool
	// idle is closed and replaced each time the Agent settles, so Wait can block
	// on settlement without polling.
	idle chan struct{}
	// waitErr records why the last turn ended, so Wait can report a failure
	// rather than only its absence.
	waitErr error
	// turnSeq counts turns this instance has run. It is the turn number written
	// into the log.
	turnSeq int
	// stepCount counts steps in the current turn.
	stepCount int
}

// newInstance builds an instance without publishing it.
//
// It performs no I/O and cannot fail for a validated spec, which is why
// initialize is a separate step: the factory calls this first and only then does
// anything that could leave resources behind.
func newInstance(config Config, spec agent.Spec) (*instance, error) {
	id, err := newAgentID()
	if err != nil {
		return nil, err
	}
	maxSteps := config.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	config.MaxSteps = maxSteps
	created := &instance{
		info: agent.Info{
			ID:       id,
			Scope:    spec.Scope,
			Provider: spec.Provider,
			Model:    spec.Model,
		},
		config: config,
		state:  agent.StatePending,
		done:   make(chan struct{}),
		idle:   make(chan struct{}),
	}
	created.inbox = newInbox()
	// An initial input is queued before the Agent is published, so a caller
	// never has to race Send against startup.
	if spec.InitialInput != nil {
		if err := created.inbox.push(*spec.InitialInput); err != nil {
			return nil, err
		}
	}
	return created, nil
}

// initialize completes creation.
//
// Everything that can fail happens here, before the factory records the
// instance. A failure therefore leaves nothing published, which is what makes
// "creation failed" and "a broken Agent exists" distinguishable.
func (a *instance) initialize(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// The turn/start event is written by the loop, not here: a created Agent that
	// has not run yet must not have a turn in the log, or replay would claim work
	// that never happened.
	a.mu.Lock()
	a.state = agent.StateIdle
	a.mu.Unlock()
	return nil
}

// Info describes this Agent.
func (a *instance) Info() agent.Info { return a.info }

// State reports the current lifecycle state.
func (a *instance) State() agent.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// Done returns a channel closed when the instance is disposed.
func (a *instance) Done() <-chan struct{} { return a.done }

// Handle returns the read/send handle, which is the instance itself.
func (a *instance) Handle() agent.Handle { return a }

// Wait blocks until the Agent has no work left, or until ctx is done.
//
// A disposed Agent returns immediately. The wait observes a settling channel
// rather than polling state, so a caller is woken by the driver instead of
// spinning.
func (a *instance) Wait(ctx context.Context) error {
	for {
		a.mu.Lock()
		if a.disposed {
			err := a.waitErr
			a.mu.Unlock()
			return err
		}
		if !a.active {
			err := a.waitErr
			a.mu.Unlock()
			return err
		}
		idle := a.idle
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idle:
		}
	}
}

// settle marks the current turn as finished and wakes waiters.
//
// The idle channel is replaced rather than closed twice, so a later turn starts
// a fresh wait instead of returning immediately on a stale signal.
func (a *instance) settle(err error) {
	a.mu.Lock()
	a.active = false
	a.cancelled = nil
	a.waitErr = err
	if a.state != agent.StateDisposed && a.state != agent.StateStopping {
		a.state = agent.StateIdle
	}
	previous := a.idle
	a.idle = make(chan struct{})
	a.mu.Unlock()
	close(previous)
}

// begin marks a turn as active and returns the context its work runs under.
//
// The returned cancel function is retained by the instance, so a later Cancel
// call reaches the same context the work is using.
func (a *instance) begin(parent context.Context) (context.Context, context.CancelFunc, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.disposed {
		return nil, nil, agent.ErrClosed
	}
	if a.active {
		return nil, nil, fmt.Errorf("%w: a turn is already running", agent.ErrInvalidValue)
	}
	ctx, cancel := context.WithCancel(parent)
	a.active = true
	a.state = agent.StateRunning
	a.cancelled = cancel
	a.turnSeq++
	return ctx, cancel, nil
}

// dispose stops the instance and releases its resources.
//
// It is idempotent, waits for an active turn to finish rather than abandoning
// it, and refuses further input afterwards. Waiting matters: a caller that
// disposes an Agent and immediately inspects shared state must not observe the
// driver still writing to it.
func (a *instance) dispose(ctx context.Context) error {
	a.mu.Lock()
	if a.disposed {
		a.mu.Unlock()
		return nil
	}
	a.disposed = true
	a.state = agent.StateStopping
	cancel := a.cancelled
	active := a.active
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if active {
		// Wait for the driver to release the log and the provider stream, using
		// the settling channel the driver closes when a turn ends. It must NOT
		// wait on a.done: that channel is closed at the end of this function, so
		// waiting on it here would deadlock dispose against itself. A
		// caller-supplied deadline still bounds the wait.
		if err := a.waitForSettlement(ctx); err != nil {
			// The turn did not settle in time. The instance is already marked
			// stopping and refuses input, so returning the deadline error is
			// honest: the caller knows the resources may still be releasing.
			return err
		}
	}

	a.mu.Lock()
	a.state = agent.StateDisposed
	a.mu.Unlock()
	closeOnce(a.done)
	return nil
}

// waitForSettlement waits until no turn is active.
func (a *instance) waitForSettlement(ctx context.Context) error {
	for {
		a.mu.Lock()
		if !a.active {
			a.mu.Unlock()
			return nil
		}
		idle := a.idle
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idle:
		}
	}
}

// closeOnce closes a channel at most once, tolerating a nil channel.
func closeOnce(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
}
