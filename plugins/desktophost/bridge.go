package desktophost

import (
	"errors"
	"fmt"
	"sync"
)

// State is a desktop host's lifecycle state.
type State string

// The states this bridge reports.
const (
	// StateStarting is before readiness or failure.
	StateStarting State = "starting"
	// StateReady is after a successful startup.
	StateReady State = "ready"
	// StateFailed is after a fatal startup error.
	StateFailed State = "failed"
	// StateClosed is after shutdown.
	StateClosed State = "closed"
)

// Errors this package returns.
var (
	// ErrNotReady reports an operation before readiness.
	ErrNotReady = errors.New("desktophost: host is not ready")
	// ErrClosed reports use after shutdown.
	ErrClosed = errors.New("desktophost: host is closed")
)

// Bridge models the host-side desktop lifecycle handshake.
type Bridge struct {
	mu     sync.Mutex
	state  State
	fatal  string
	closes int
}

// New returns a bridge in the starting state.
func New() *Bridge { return &Bridge{state: StateStarting} }

// Ready marks startup successful.
func (b *Bridge) Ready() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateClosed {
		return fmt.Errorf("%w", ErrClosed)
	}
	b.state = StateReady
	return nil
}

// Fail records a fatal startup error.
func (b *Bridge) Fail(reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateClosed {
		return fmt.Errorf("%w", ErrClosed)
	}
	b.state = StateFailed
	b.fatal = reason
	return nil
}

// State reports the current state.
func (b *Bridge) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// FatalReason returns the fatal startup reason, if any.
func (b *Bridge) FatalReason() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.fatal
}

// Inject demonstrates credential injection: it returns the token it was given so
// a test can assert the shell received exactly the injected credential, and it
// refuses before readiness.
func (b *Bridge) Inject(token string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateClosed {
		return "", fmt.Errorf("%w", ErrClosed)
	}
	if b.state != StateReady {
		return "", fmt.Errorf("%w", ErrNotReady)
	}
	return token, nil
}

// Close shuts the host down exactly once.
func (b *Bridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateClosed {
		return nil
	}
	b.state = StateClosed
	b.closes++
	return nil
}

// CloseCount reports how many times Close ran, for tests to prove idempotence.
func (b *Bridge) CloseCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closes
}
