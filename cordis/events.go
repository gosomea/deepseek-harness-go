package cordis

import (
	"errors"
	"fmt"
	"sync"
)

// Event contains the dispatching scope, payload and activation cancellation.
// Args are shared between parallel listeners; treat them as immutable there.
type Event struct {
	// Context is the dispatcher scope, not the registration scope.
	Context *Context
	// Name identifies the event being dispatched.
	Name string
	// Args contains caller-owned payloads; parallel listeners must treat them as immutable.
	Args []any
}

// Next delegates to the rest of a waterfall chain, including its default body.
type Next func() (any, error)

// Listener returns a bail value or error. Next is non-nil only in Waterfall.
type Listener func(Event, Next) (any, error)

// EventOptions controls order, single-shot delivery and dispatch filtering.
// Without Filter, events reach every registered listener in the root runtime,
// including isolated scopes, matching Cordis' unfiltered dispatch behavior.
type EventOptions struct {
	// Prepend places this listener before existing listeners.
	Prepend bool
	// Once claims and removes the listener before its first invocation.
	Once bool
	// Global bypasses the optional dispatcher filter.
	Global bool
}

type hook struct {
	ctx      *Context
	listener Listener
	options  EventOptions
	effect   *effect
	claimed  bool
}

// On registers a lifecycle-owned listener. Only loading/active contexts register.
func (c *Context) On(name string, listener Listener, options EventOptions) (Cleanup, error) {
	if name == "" || listener == nil {
		return nil, fmt.Errorf("cordis: event needs a name and listener")
	}
	r := c.rt
	r.mu.Lock()
	if !c.writableLocked() {
		r.mu.Unlock()
		return nil, ErrInactive
	}
	h := &hook{ctx: c, listener: listener, options: options}
	h.effect = c.addEffectLocked("on:"+name, func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		hooks := r.hooks[name]
		for i, item := range hooks {
			if item == h {
				r.hooks[name] = append(hooks[:i], hooks[i+1:]...)
				break
			}
		}
		if len(r.hooks[name]) == 0 {
			delete(r.hooks, name)
		}
		return nil
	})
	if options.Prepend {
		r.hooks[name] = append([]*hook{h}, r.hooks[name]...)
	} else {
		r.hooks[name] = append(r.hooks[name], h)
	}
	r.mu.Unlock()
	return h.effect.dispose, nil
}

// Dispatcher selects listeners using an optional predicate on their contexts.
// Global listeners bypass this predicate. The predicate runs outside the lock.
type Dispatcher struct {
	ctx    *Context
	filter func(*Context) bool
}

// Events returns a dispatcher without listener scope filtering.
func (c *Context) Events() Dispatcher { return Dispatcher{ctx: c} }

// Filter selects listener scopes with a predicate; Global listeners bypass it.
func (c *Context) Filter(predicate func(*Context) bool) Dispatcher { return Dispatcher{c, predicate} }

// SameScope tests service namespace identity, for explicit event filtering.
func (c *Context) SameScope(other *Context, service string) bool {
	return c.rt == other.rt && c.scopes[service] == other.scopes[service]
}

func (d Dispatcher) snapshot(name string) ([]*hook, error) {
	r := d.ctx.rt
	r.mu.Lock()
	if !d.ctx.writableLocked() {
		r.mu.Unlock()
		return nil, ErrInactive
	}
	hooks := append([]*hook(nil), r.hooks[name]...)
	r.mu.Unlock()
	selected := hooks[:0]
	for _, h := range hooks {
		if h.options.Global || d.filter == nil || d.filter(h.ctx) {
			selected = append(selected, h)
		}
	}
	return selected, nil
}

func (h *hook) call(event Event, next Next) (any, error) {
	r := h.ctx.rt
	r.mu.Lock()
	if h.effect.started || !h.ctx.writableLocked() || (h.options.Once && h.claimed) {
		r.mu.Unlock()
		if next != nil {
			return next()
		}
		return nil, nil
	}
	if h.options.Once {
		h.claimed = true
	}
	r.mu.Unlock()
	if h.options.Once {
		_ = h.effect.dispose()
	}
	return safeValue(func() (any, error) { return h.listener(event, next) })
}

// Emit invokes listeners in order, ignores values and stops on the first error.
func (d Dispatcher) Emit(name string, args ...any) error {
	hooks, err := d.snapshot(name)
	if err != nil {
		return err
	}
	for _, h := range hooks {
		if _, err := h.call(Event{d.ctx, name, args}, nil); err != nil {
			return err
		}
	}
	return nil
}

// Parallel starts every listener in a goroutine and joins all errors.
func (d Dispatcher) Parallel(name string, args ...any) error {
	hooks, err := d.snapshot(name)
	if err != nil {
		return err
	}
	errs := make([]error, len(hooks))
	var wg sync.WaitGroup
	for i, h := range hooks {
		wg.Add(1)
		go func(i int, h *hook) { defer wg.Done(); _, errs[i] = h.call(Event{d.ctx, name, args}, nil) }(i, h)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// IsBailed matches Cordis: nil and false continue; 0 and "" stop the chain.
func IsBailed(value any) bool {
	if nilValue(value) {
		return false
	}
	boolean, ok := value.(bool)
	return !ok || boolean
}

// Serial invokes listeners sequentially until the first bail value or error.
func (d Dispatcher) Serial(name string, args ...any) (any, error) {
	hooks, err := d.snapshot(name)
	if err != nil {
		return nil, err
	}
	for _, h := range hooks {
		value, err := h.call(Event{d.ctx, name, args}, nil)
		if err != nil || IsBailed(value) {
			return value, err
		}
	}
	return nil, nil
}

// Bail shares Serial's synchronous Go implementation; Go has no Promise mode.
func (d Dispatcher) Bail(name string, args ...any) (any, error) { return d.Serial(name, args...) }

// Waterfall wraps a default body in listeners. Returning without calling Next
// vetoes the remaining chain. A skipped stale listener delegates automatically.
func (d Dispatcher) Waterfall(name string, inner Next, args ...any) (any, error) {
	if inner == nil {
		return nil, fmt.Errorf("cordis: waterfall needs a default body")
	}
	hooks, err := d.snapshot(name)
	if err != nil {
		return nil, err
	}
	index := 0
	var next Next
	next = func() (any, error) {
		if index == len(hooks) {
			return safeValue(inner)
		}
		h := hooks[index]
		index++
		return h.call(Event{d.ctx, name, args}, next)
	}
	return next()
}
