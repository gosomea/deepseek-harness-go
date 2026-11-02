package hooks

import (
	"errors"
	"fmt"
	"sync"
)

// Errors this bridge returns.
var (
	// ErrDuplicateHook reports two handlers registered under one name.
	ErrDuplicateHook = fmt.Errorf("hooks: duplicate hook")
)

// Handler observes one event and may modify a value.
type Handler func(event string, payload map[string]string) error

// Bridge routes named events to registered handlers.
type Bridge struct {
	mu      sync.RWMutex
	byEvent map[string][]namedHandler
	errs    []error
}

type namedHandler struct {
	name string
	fn   Handler
}

// New returns an empty bridge.
func New() *Bridge {
	return &Bridge{byEvent: map[string][]namedHandler{}}
}

// Register adds a handler for an event.
func (b *Bridge) Register(name, event string, handler Handler) error {
	if name == "" || event == "" || handler == nil {
		return fmt.Errorf("hooks: name, event and handler are required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, existing := range b.byEvent[event] {
		if existing.name == name {
			return fmt.Errorf("%w: %q for %q", ErrDuplicateHook, name, event)
		}
	}
	b.byEvent[event] = append(b.byEvent[event], namedHandler{name: name, fn: handler})
	return nil
}

// Emit runs every handler for an event in registration order.
//
// A handler failure is recorded and returned joined, not swallowed: a hook that
// failed must be visible to the caller, because a silently ignored lifecycle hook
// is exactly the "claimed but not done" failure this bridge refuses.
func (b *Bridge) Emit(event string, payload map[string]string) error {
	b.mu.RLock()
	handlers := append([]namedHandler(nil), b.byEvent[event]...)
	b.mu.RUnlock()
	var errs []error
	for _, handler := range handlers {
		if err := handler.fn(event, payload); err != nil {
			errs = append(errs, fmt.Errorf("hook %q: %w", handler.name, err))
		}
	}
	b.mu.Lock()
	b.errs = append(b.errs, errs...)
	b.mu.Unlock()
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Errors returns every error recorded by Emit, in order.
func (b *Bridge) Errors() []error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]error, len(b.errs))
	copy(out, b.errs)
	return out
}

// Events returns the registered event names.
func (b *Bridge) Events() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.byEvent))
	for event := range b.byEvent {
		out = append(out, event)
	}
	return out
}
