package cordis

import (
	"fmt"
	"reflect"
)

type serviceKey struct {
	name  string
	scope *Scope
}
type binding struct {
	key   serviceKey
	value any
	owner *Fiber
	epoch uint64
}

func (c *Context) key(name string) serviceKey { return serviceKey{name, c.scopes[name]} }

func available(b *binding) bool {
	return b != nil && !b.owner.disposed && b.owner.state == Active && b.owner.err == nil &&
		b.owner.revision == b.owner.loadedRevision
}

// Provide publishes a service when its owner becomes Active. A service is
// automatically removed on unload. Duplicate providers in one scope are errors.
func (c *Context) Provide(name string, value any) (Cleanup, error) {
	if name == "" || nilValue(value) {
		return nil, fmt.Errorf("cordis: service needs a name and non-nil value")
	}
	r := c.rt
	r.mu.Lock()
	if !c.writableLocked() {
		r.mu.Unlock()
		return nil, ErrInactive
	}
	key := c.key(name)
	if r.services[key] != nil {
		r.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrDuplicateService, name)
	}
	b := &binding{key, value, c.fiber, c.epoch}
	r.services[key] = b
	e := c.addEffectLocked("provide:"+name, func() error {
		r.mu.Lock()
		if r.services[key] == b {
			delete(r.services, key)
		}
		r.mu.Unlock()
		return nil
	})
	r.beginBusyLocked()
	r.mu.Unlock()
	r.reconcile()
	return e.dispose, nil
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// Get resolves a named service. Injected bindings remain available to the
// consumer's cleanup even after the provider has begun unloading.
func (c *Context) Get(name string) (any, error) {
	r := c.rt
	r.mu.Lock()
	defer r.mu.Unlock()
	f := c.fiber
	if c.epoch != f.epoch || (f.state != Active && f.state != Loading && f.state != Unloading) {
		return nil, ErrInactive
	}
	key := c.key(name)
	for _, b := range f.dependencies {
		if b.key == key {
			return b.value, nil
		}
	}
	b := r.services[key]
	if available(b) || (b != nil && b.owner == f && b.epoch == c.epoch) {
		return b.value, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrServiceNotFound, name)
}

// Key carries the consumer's expected service type. Use interface types to
// depend on small capabilities rather than concrete provider implementations.
type Key[T any] struct{ name string }

// NewKey creates a typed lookup key; service names are shared across key types.
func NewKey[T any](name string) Key[T] { return Key[T]{name} }

// Name exposes the key name for dependency declarations and isolation.
func (k Key[T]) Name() string { return k.name }

// Provide registers a typed service with Context.Provide ownership rules.
func Provide[T any](c *Context, key Key[T], value T) (Cleanup, error) {
	return c.Provide(key.name, value)
}

// Resolve returns the expected type or ErrServiceType, and the zero value on failure.
// Use interface keys to permit interchangeable providers.
func Resolve[T any](c *Context, key Key[T]) (T, error) {
	var zero T
	value, err := c.Get(key.name)
	if err != nil {
		return zero, err
	}
	typed, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s has type %T", ErrServiceType, key.name, value)
	}
	return typed, nil
}
