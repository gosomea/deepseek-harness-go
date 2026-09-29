package cordis

import (
	"errors"
	"fmt"
)

var (
	// ErrInactive rejects work outside a live activation.
	ErrInactive = errors.New("cordis: inactive context")
	// ErrDuplicateService reports multiple providers in one service namespace.
	ErrDuplicateService = errors.New("cordis: service already provided")
	// ErrServiceNotFound reports an unavailable service in the requested scope.
	ErrServiceNotFound = errors.New("cordis: service not found")
	// ErrServiceType reports a mismatch between a Key and a provided value.
	ErrServiceType = errors.New("cordis: service type mismatch")
)

// Cleanup releases a resource. All remaining cleanups run even if one fails.
type Cleanup func() error

// Plugin describes an entrypoint; each Plugin call creates an independent Fiber.
// Config and service objects are caller-owned and must not be mutated concurrently.
type Plugin struct {
	// Name labels logs and diagnostics; registrations have independent IDs.
	Name string
	// Inject lists services required before Apply runs.
	Inject []string
	// Apply acquires resources and may return a final cleanup even on failure.
	Apply func(*Context, any) (Cleanup, error)
	// Validate normalizes configuration before registration or update; nil accepts it.
	Validate func(any) (any, error)
}

func (p Plugin) validate(config any) (any, error) {
	if p.Apply == nil || p.Name == "" {
		return nil, errors.New("cordis: plugin needs a name and Apply")
	}
	for _, name := range p.Inject {
		if name == "" {
			return nil, errors.New("cordis: empty dependency name")
		}
	}
	if p.Validate == nil {
		return config, nil
	}
	value, err := safeValue(func() (any, error) { return p.Validate(config) })
	if err != nil {
		return nil, fmt.Errorf("validate %s: %w", p.Name, err)
	}
	return value, nil
}

func safeValue(fn func() (any, error)) (value any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("cordis: callback panic: %v", p)
		}
	}()
	return fn()
}

func safeCleanup(fn Cleanup) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("cordis: cleanup panic: %v", p)
		}
	}()
	return fn()
}
