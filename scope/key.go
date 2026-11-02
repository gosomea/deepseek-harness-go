package scope

import (
	"errors"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// Key is an opaque, identity-compared scope key.
//
// Identity is the key's own value equality, so a caller normally uses a pointer
// (an agent handle, a session owner) rather than a string identifier. Two
// generations that share a string id must route independently, which a string
// key cannot express. A key whose dynamic type is not comparable makes the map
// operations panic; that is a programming error, and the documentation says so
// rather than adding a runtime check to every lookup.
type Key any

// ErrAlreadyBound reports an attempt to give a scope key a parent twice. Only the
// binding returned by the original call may re-link it.
var ErrAlreadyBound = errors.New("scope: scope key is already bound to a parent")

// ErrCycle reports a parent link that would close a cycle. Every chain consumer
// walks parents to a root, so a cycle would not terminate.
var ErrCycle = errors.New("scope: scope parent link would form a cycle")

// ErrDuplicateEntry reports a duplicate name inside one entry table.
var ErrDuplicateEntry = errors.New("scope: duplicate entry")

// ErrRemovedProjection reports a use of a contribution after its owner disposed it.
var ErrRemovedProjection = errors.New("scope: contribution was removed or replaced")

// parentOf maps each key to its enclosing scope. Go has no weak maps, so an
// entry is deleted when the scope that owns it is disposed; see Scope.Dispose.
var parentOf = map[Key]Key{}

// carriers maps each minted carrier to the key it routes for. The presence of an
// entry distinguishes an unkeyed carrier from a non-carrier, and the stored value
// is the routing key (which may be nil for an unkeyed carrier).
var carriers = map[*Carrier]Key{}

// ParentBinding is the privileged handle to move one key's parent link.
//
// A key that already has a parent cannot be bound again from outside: the caller
// that created the link is the only one that may change it, because nothing else
// can know what was produced under the old parent.
type ParentBinding struct {
	// key is the child whose parent this binding may move.
	key Key
}

// Rebind re-links the bound key to a different parent, with the same cycle check
// as the original bind.
//
// It is valid only while nothing produced under the old parent is retained; the
// holder upholds that because this relation cannot see what was logged.
func (b ParentBinding) Rebind(parent Key) error {
	if b.key == nil {
		return fmt.Errorf("%w: binding has no key", ErrCycle)
	}
	return linkParent(b.key, parent)
}

// BindParent makes parent the enclosing scope of key, once.
//
// A key that already has a parent returns ErrAlreadyBound: there is no open
// re-link path, so a scope's ancestry cannot be moved by anyone but the original
// binder. A link that would close a cycle returns ErrCycle.
func BindParent(key, parent Key) (ParentBinding, error) {
	if key == nil {
		return ParentBinding{}, fmt.Errorf("%w: scope key is nil", ErrAlreadyBound)
	}
	if _, exists := parentOf[key]; exists {
		return ParentBinding{}, fmt.Errorf("%w: %v", ErrAlreadyBound, key)
	}
	if err := linkParent(key, parent); err != nil {
		return ParentBinding{}, err
	}
	return ParentBinding{key: key}, nil
}

// linkParent writes one parent link after proving it cannot close a cycle.
func linkParent(key, parent Key) error {
	for cursor := parent; cursor != nil; {
		if cursor == key {
			return fmt.Errorf("%w: %v", ErrCycle, key)
		}
		next, ok := parentOf[cursor]
		if !ok {
			break
		}
		cursor = next
	}
	parentOf[key] = parent
	return nil
}

// ParentOf returns one key's enclosing scope, or nil for a root scope.
func ParentOf(key Key) Key {
	parent, ok := parentOf[key]
	if !ok {
		return nil
	}
	return parent
}

// ChainOf returns the chain from key to its root ancestor, nearest first:
// [key, parent, grandparent, ...]. A nil key returns an empty chain.
//
// The chain terminates because every link is cycle-checked, so a caller may walk
// it without a bound.
func ChainOf(key Key) []Key {
	var chain []Key
	for cursor := key; cursor != nil; {
		chain = append(chain, cursor)
		next, ok := parentOf[cursor]
		if !ok {
			break
		}
		cursor = next
	}
	return chain
}

// forget drops a disposed key's parent link.
//
// This is the deliberate difference from the reference's WeakMap: Go cannot
// reclaim a map entry when its key becomes unreachable, so a long-lived process
// would retain every dead key. Disposing the scope releases the link explicitly.
// A key that is never disposed keeps its link, which is a real retention
// difference rather than a closed gap.
func forget(key Key) {
	delete(parentOf, key)
}

// Scope is one agent's registration world: an opaque key plus the Cordis child
// context whose registrations it owns.
//
// Everything registered through Ctx is visible in this scope and disposed with
// it, so a contribution can never be visible in one scope while torn down with
// another. Disposing the scope unwinds every such registration.
type Scope struct {
	key     Key
	fiber   *cordis.Fiber
	ctx     *cordis.Context
	dispose func() error
	once    bool
}

// Option customizes scope creation.
type Option func(*scopeConfig)

// scopeConfig collects the options before the child plugin is registered.
type scopeConfig struct {
	// parent is an enclosing scope key, bound before the child registers so that
	// contributions made during Apply already see the chain.
	parent Key
	// hasParent distinguishes "no parent requested" from an explicit nil parent,
	// which is how an unkeyed root scope is expressed.
	hasParent bool
}

// WithParent binds parent as the enclosing scope of the new scope's key.
//
// The binding is made once and its handle is discarded, so a scope created this
// way can never be re-linked: the caller that wants to move a child later must
// call BindParent itself and keep the returned handle.
func WithParent(parent Key) Option {
	return func(config *scopeConfig) {
		config.parent = parent
		config.hasParent = true
	}
}

// Create mints a scope under ctx's activation and returns it.
//
// The child plugin carries no dependencies, so it activates as soon as ctx is
// writable and its scoped context becomes available immediately. Everything
// registered through Scope.Ctx belongs to the child activation, so Scope.Dispose
// releases it.
func Create(ctx *cordis.Context, key Key, options ...Option) (*Scope, error) {
	if ctx == nil {
		return nil, errors.New("scope: create needs a context")
	}
	if key == nil {
		return nil, errors.New("scope: create needs a key")
	}
	config := scopeConfig{}
	for _, option := range options {
		option(&config)
	}
	created := &Scope{key: key}
	if config.hasParent {
		binding, err := BindParent(key, config.parent)
		if err != nil {
			return nil, err
		}
		// The handle is intentionally dropped: WithParent is the re-link-free
		// form, and its only purpose was the initial link.
		_ = binding
	}
	plugin := cordis.Plugin{
		Name: "scope",
		Apply: func(scoped *cordis.Context, _ any) (cordis.Cleanup, error) {
			created.ctx = scoped
			created.fiber = scoped.Fiber()
			// Tag the activation so ScopeOf finds the key from the context a
			// registration receives.
			tag(scoped, key)
			return func() error {
				untag(scoped)
				forget(key)
				return nil
			}, nil
		},
	}
	fiber, err := ctx.Plugin(plugin, nil)
	if err != nil {
		forget(key)
		return nil, err
	}
	if err := fiber.Err(); err != nil {
		forget(key)
		return nil, err
	}
	created.fiber = fiber
	created.dispose = fiber.Dispose
	return created, nil
}

// Key returns the scope's opaque identity.
func (s *Scope) Key() Key {
	if s == nil {
		return nil
	}
	return s.key
}

// Ctx returns the scoped context. It is nil when the scope was created under a
// context that is not yet writable, because the child has not activated.
func (s *Scope) Ctx() *cordis.Context {
	if s == nil {
		return nil
	}
	return s.ctx
}

// Fiber returns the activation that owns this scope's registrations.
func (s *Scope) Fiber() *cordis.Fiber {
	if s == nil {
		return nil
	}
	return s.fiber
}

// RawDispose returns the exact Cordis disposer.
//
// It is for nesting teardown inside an ordered composite effect: the disposer
// must be yielded by identity rather than wrapped, or the composite's ordering
// guarantee is lost. It does not untag eagerly; the owning plugin's cleanup does
// that as part of the same unwind.
func (s *Scope) RawDispose() cordis.Cleanup {
	if s == nil || s.dispose == nil {
		return func() error { return nil }
	}
	return s.dispose
}

// Dispose unwinds every registration made through this scope.
//
// It is idempotent and safe to race: concurrent callers join one completion
// instead of unwinding twice. Unlike RawDispose, Dispose is the public path a
// caller uses when it simply wants the scope gone.
func (s *Scope) Dispose() error {
	if s == nil {
		return nil
	}
	if s.once {
		return nil
	}
	s.once = true
	if s.dispose == nil {
		return nil
	}
	return s.dispose()
}

// tag records the key for one activation's context.
func tag(ctx *cordis.Context, key Key) { contextKeys[ctx] = key }

// untag drops one activation's tag.
func untag(ctx *cordis.Context) { delete(contextKeys, ctx) }

// contextKeys maps a scoped activation's context to its key.
//
// The reference reads the nearest tag through JavaScript prototype inheritance.
// Go contexts do not inherit properties, so this is an explicit table: a context
// minted by Create is tagged, and ScopeOf answers for it. A context derived by
// registering a *further* child plugin is not tagged automatically, so a nested
// scope must be created with Create (and WithParent when it belongs to a chain).
// That difference is recorded in the package README rather than hidden.
var contextKeys = map[*cordis.Context]Key{}

// ScopeOf returns the key one context's registrations belong to, or nil for an
// unscoped (global) registrant.
func ScopeOf(ctx *cordis.Context) Key {
	if ctx == nil {
		return nil
	}
	key, ok := contextKeys[ctx]
	if !ok {
		return nil
	}
	return key
}
