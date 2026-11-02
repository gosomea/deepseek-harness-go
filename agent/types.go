package agent

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidValue reports an Agent value that cannot be used.
var ErrInvalidValue = errors.New("agent: invalid value")

// ErrClosed reports use of an Agent after it was disposed.
var ErrClosed = errors.New("agent: agent is disposed")

// ErrNotOwner reports a disposal attempt from a caller that does not own the
// instance.
//
// It is a distinct error because "you asked to stop something you do not own" is
// a permission fact, not a lifecycle one: a handle that can find an Agent must
// not therefore be able to destroy it.
var ErrNotOwner = errors.New("agent: caller does not own this agent")

// ErrInputRejected reports input refused before it joined the inbox, for example
// because the Agent is stopping.
var ErrInputRejected = errors.New("agent: input rejected")

// ID identifies one Agent instance for its whole life.
//
// It is an opaque string: the reference brands identity at compile time and
// performs no format validation, so an id read from an existing record is
// accepted unchanged.
type ID string

// String returns the raw identifier.
func (id ID) String() string { return string(id) }

// IsZero reports whether the identifier is unset.
func (id ID) IsZero() bool { return id == "" }

// ScopeKey names the Agent scope an instance is visible in.
//
// It is a plain string rather than a scope value so this package does not depend
// on the scope implementation: the scope layer decides what a key means, and an
// Agent only records which one it belongs to.
type ScopeKey string

// String returns the raw key.
func (k ScopeKey) String() string { return string(k) }

// State is an Agent's lifecycle state as its owner observes it.
//
// The set is closed and deliberately small: a consumer needs to tell "not started
// yet" from "working" from "settled" from "gone", and anything finer is a driver
// detail that belongs behind the driver's own observation channel.
type State string

// The states an Agent reports.
const (
	// StatePending is a created Agent whose driver has not begun a turn.
	StatePending State = "pending"
	// StateRunning is an Agent with an active turn.
	StateRunning State = "running"
	// StateIdle is an Agent that finished its work and is waiting for input.
	StateIdle State = "idle"
	// StateStopping is an Agent whose cancellation has been requested and whose
	// driver is still releasing resources.
	StateStopping State = "stopping"
	// StateDisposed is an Agent whose resources have been released. It accepts
	// no further input.
	StateDisposed State = "disposed"
)

// Valid reports whether the state is one this package defines.
func (s State) Valid() bool {
	switch s {
	case StatePending, StateRunning, StateIdle, StateStopping, StateDisposed:
		return true
	default:
		return false
	}
}

// Input is one piece of user-side input delivered to an Agent.
//
// It carries text rather than a message so the caller does not have to mint a
// message id or choose a source: the driver records the user message, and it is
// the driver's job to keep that record consistent with the log.
type Input struct {
	// Text is the user-visible input.
	Text string `json:"text"`
	// Origin names the producer, for example "cli" or "test". It is recorded on
	// the user message's source so a log reader can attribute the input.
	Origin string `json:"origin,omitempty"`
}

// Validate reports whether the input can be delivered.
func (i Input) Validate() error {
	if strings.TrimSpace(i.Text) == "" {
		return fmt.Errorf("%w: input requires text", ErrInvalidValue)
	}
	return nil
}

// Info describes one Agent to a consumer that only reads.
type Info struct {
	// ID is the instance identity.
	ID ID
	// Scope is the scope key the Agent is visible in.
	Scope ScopeKey
	// Provider is the route the Agent's model calls use, when known.
	Provider string
	// Model is the model identifier, when known.
	Model string
}

// Validate reports whether the description is complete.
func (i Info) Validate() error {
	if i.ID.IsZero() {
		return fmt.Errorf("%w: info requires an id", ErrInvalidValue)
	}
	if i.Scope == "" {
		return fmt.Errorf("%w: info requires a scope", ErrInvalidValue)
	}
	return nil
}
