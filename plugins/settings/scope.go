package settings

import (
	"fmt"
	"strings"
)

// ScopePath builds the chain of scopes from the root to target, inclusive.
//
// The chain is passed explicitly to Resolve rather than derived from a parent
// pointer, so this package never has to know how scopes record their parents. It
// also makes the resolution order a value a caller can print, which is what makes
// "why did this scope see that value" answerable.
//
// A parent chain that does not end at target is refused: silently accepting it
// would resolve against a scope the caller did not ask about.
func ScopePath(root string, parents map[string]string, target string) ([]string, error) {
	if target == "" && root == "" {
		return []string{""}, nil
	}
	var reversed []string
	current := target
	seen := make(map[string]bool)
	for {
		if seen[current] {
			return nil, fmt.Errorf("%w: scope chain for %q is cyclic", ErrInvalidSchema, target)
		}
		seen[current] = true
		reversed = append(reversed, current)
		if current == root {
			break
		}
		parent, ok := parents[current]
		if !ok {
			return nil, fmt.Errorf("%w: scope %q does not descend from root %q", ErrInvalidSchema, target, root)
		}
		current = parent
	}
	// Reverse into root-first order.
	path := make([]string, 0, len(reversed))
	for index := len(reversed) - 1; index >= 0; index-- {
		path = append(path, reversed[index])
	}
	return path, nil
}

// Registry owns the layers of a settings tree.
//
// It is a value rather than a package-level table: two registries in one process
// must not see each other's overrides.
type Registry struct {
	schema   *Schema
	layers   map[string]*Layer
	parents  map[string]string
	root     string
	sessions map[string]map[string]Value
}

// NewRegistry returns a registry over a schema, rooted at root.
func NewRegistry(schema *Schema, root string) *Registry {
	return &Registry{
		schema:   schema,
		layers:   make(map[string]*Layer),
		parents:  make(map[string]string),
		root:     root,
		sessions: make(map[string]map[string]Value),
	}
}

// Layer returns the layer for a scope, creating it on first use.
func (r *Registry) Layer(scope string) *Layer {
	if layer, ok := r.layers[scope]; ok {
		return layer
	}
	layer := NewLayer(scope)
	r.layers[scope] = layer
	return layer
}

// SetParent records that scope descends from parent.
//
// It refuses to re-point an existing child: a scope with two parents would make
// the resolution chain ambiguous, and the ambiguity would only show up as a
// surprising value later.
func (r *Registry) SetParent(scope, parent string) error {
	if strings.TrimSpace(scope) == "" {
		return fmt.Errorf("%w: a child scope needs a name", ErrInvalidSchema)
	}
	if existing, ok := r.parents[scope]; ok && existing != parent {
		return fmt.Errorf("%w: scope %q already descends from %q", ErrInvalidSchema, scope, existing)
	}
	r.parents[scope] = parent
	return nil
}

// Session returns the session override map for a session id, creating it on
// first use.
func (r *Registry) Session(id string) map[string]Value {
	if overrides, ok := r.sessions[id]; ok {
		return overrides
	}
	overrides := make(map[string]Value)
	r.sessions[id] = overrides
	return overrides
}

// Resolve computes the effective settings for a scope in a session.
//
// The session override is applied last so an explicit per-session choice always
// beats a scope default, regardless of how deep the scope is.
func (r *Registry) Resolve(scope, session string) ([]Resolved, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: no registry", ErrInvalidSchema)
	}
	path, err := ScopePath(r.root, r.parents, scope)
	if err != nil {
		return nil, err
	}
	var sessionOverrides map[string]Value
	if session != "" {
		sessionOverrides = r.sessions[session]
	}
	return Resolve(r.schema, path, r.layers, sessionOverrides)
}

// Effective returns the resolved value for one key.
func (r *Registry) Effective(scope, session, key string) (Resolved, error) {
	resolved, err := r.Resolve(scope, session)
	if err != nil {
		return Resolved{}, err
	}
	for _, candidate := range resolved {
		if candidate.Key == key {
			return candidate, nil
		}
	}
	return Resolved{}, fmt.Errorf("%w: %q", ErrUnknownKey, key)
}
