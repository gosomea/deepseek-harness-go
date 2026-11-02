package settings

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Errors this package returns.
var (
	// ErrUnknownKey reports a key the schema does not declare.
	ErrUnknownKey = errors.New("settings: unknown key")
	// ErrInvalidValue reports a value the schema refuses.
	ErrInvalidValue = errors.New("settings: invalid value")
	// ErrInvalidSchema reports a schema that cannot be used.
	ErrInvalidSchema = errors.New("settings: invalid schema")
)

// Kind is a setting's value type. It is a closed set so an unknown type is a
// schema error at registration time rather than a surprise at read time.
type Kind string

// The kinds this package supports.
const (
	// KindString is a text value.
	KindString Kind = "string"
	// KindBool is a boolean value.
	KindBool Kind = "bool"
	// KindInt is an integer value.
	KindInt Kind = "int"
)

// Value is one setting value.
//
// The fields are mutually exclusive and which one is populated is decided by the
// declaring Kind. A tagged union rather than `any` keeps a type error visible at
// the boundary instead of surfacing as a failed type assertion deep in a caller.
type Value struct {
	// String holds a KindString value.
	String string
	// Bool holds a KindBool value.
	Bool bool
	// Int holds a KindInt value.
	Int int
}

// Display renders the value for diagnostics.
//
// It is named Display rather than String because Value has a String field, and a
// String method on the same type would shadow it.
func (v Value) Display() string {
	return fmt.Sprintf("string=%q bool=%t int=%d", v.String, v.Bool, v.Int)
}

// Declaration is one key's schema entry.
type Declaration struct {
	// Key is the setting's stable name. Required and unique.
	Key string
	// Kind is the value type.
	Kind Kind
	// Default is the value used when no layer overrides it.
	Default Value
	// Description explains what the key controls.
	Description string
}

// Schema is the set of declared keys.
//
// It is a value with an explicit owner rather than a package-level registry: two
// schemas in one process must not see each other's keys.
type Schema struct {
	declarations map[string]Declaration
	order        []string
}

// NewSchema returns an empty schema.
func NewSchema() *Schema {
	return &Schema{declarations: make(map[string]Declaration)}
}

// Declare adds one key.
//
// It refuses a duplicate key rather than replacing, and refuses a default that
// does not match the declared kind: a schema whose own default violates its kind
// would make every later validation message suspect.
func (s *Schema) Declare(declaration Declaration) error {
	if strings.TrimSpace(declaration.Key) == "" {
		return fmt.Errorf("%w: a key is required", ErrInvalidSchema)
	}
	switch declaration.Kind {
	case KindString, KindBool, KindInt:
	default:
		return fmt.Errorf("%w: key %q has unknown kind %q", ErrInvalidSchema, declaration.Key, declaration.Kind)
	}
	if _, exists := s.declarations[declaration.Key]; exists {
		return fmt.Errorf("%w: key %q is already declared", ErrInvalidSchema, declaration.Key)
	}
	// A non-zero default must be representable in the declared kind. A zero
	// value is always allowed, because that is how an unset default is spelled.
	if declaration.Default != (Value{}) {
		if err := validateValue(declaration.Kind, declaration.Default); err != nil {
			return fmt.Errorf("%w: key %q default: %v", ErrInvalidSchema, declaration.Key, err)
		}
	}
	s.declarations[declaration.Key] = declaration
	s.order = append(s.order, declaration.Key)
	return nil
}

// Keys returns the declared keys in declaration order.
func (s *Schema) Keys() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Lookup returns a declaration by key.
func (s *Schema) Lookup(key string) (Declaration, bool) {
	declaration, ok := s.declarations[key]
	return declaration, ok
}

// validateValue reports whether a value is usable for a kind.
func validateValue(kind Kind, value Value) error {
	switch kind {
	case KindString:
		if value.Bool || value.Int != 0 {
			return fmt.Errorf("%w: a string key carries string content", ErrInvalidValue)
		}
	case KindBool:
		if value.String != "" || value.Int != 0 {
			return fmt.Errorf("%w: a bool key carries only bool content", ErrInvalidValue)
		}
	case KindInt:
		if value.String != "" || value.Bool {
			return fmt.Errorf("%w: an int key carries only int content", ErrInvalidValue)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidValue, kind)
	}
	return nil
}

// Source names the layer a resolved value came from.
type Source string

// The layers, from weakest to strongest.
const (
	// SourceDefault is the schema default.
	SourceDefault Source = "default"
	// SourceScope is an override attached to a scope.
	SourceScope Source = "scope"
	// SourceSession is an override attached to the session.
	SourceSession Source = "session"
)

// Override is one layer's value for a key.
type Override struct {
	// Key is the overridden key.
	Key string
	// Value is the overriding value.
	Value Value
}

// Layer is one scope's overrides.
type Layer struct {
	// Scope is the scope the overrides belong to. The empty string is the root.
	Scope string
	// Overrides are this scope's values, keyed by setting key.
	Overrides map[string]Value
}

// NewLayer returns an empty layer for a scope.
func NewLayer(scope string) *Layer {
	return &Layer{Scope: scope, Overrides: make(map[string]Value)}
}

// Set records one override on the layer.
func (l *Layer) Set(key string, value Value) { l.Overrides[key] = value }

// Resolved is one key's effective value plus its provenance.
//
// The source is reported rather than inferred because "which layer won" is the
// first question asked when a value looks wrong, and an answer that required
// re-reading the merge would arrive too late.
type Resolved struct {
	// Key is the setting key.
	Key string
	// Value is the effective value.
	Value Value
	// Source is the layer that supplied Value.
	Source Source
	// Scope is the scope whose layer supplied the value, when Source is
	// SourceScope. Empty otherwise.
	Scope string
}

// Resolve computes the effective settings for a scope.
//
// Precedence is default, then scope layers from the root down to scope, then the
// session override. The order matters: a nearer scope must win over a farther
// one, and the session must win over both, or "override" would depend on map
// iteration.
//
// path is the chain of scopes from the root to the target, inclusive. Passing the
// chain explicitly rather than walking a parent field keeps this package free of
// the scope package's ownership rules while still resolving deterministically.
func Resolve(schema *Schema, path []string, layers map[string]*Layer, session map[string]Value) ([]Resolved, error) {
	if schema == nil {
		return nil, fmt.Errorf("%w: no schema", ErrInvalidSchema)
	}
	// Reject unknown keys before resolving anything, so a typo cannot silently
	// produce a default.
	for scope, layer := range layers {
		if layer == nil {
			continue
		}
		for key := range layer.Overrides {
			if _, ok := schema.Lookup(key); !ok {
				return nil, fmt.Errorf("%w: %q in scope %q", ErrUnknownKey, key, scope)
			}
		}
	}
	for key := range session {
		if _, ok := schema.Lookup(key); !ok {
			return nil, fmt.Errorf("%w: %q in session", ErrUnknownKey, key)
		}
	}

	resolved := make([]Resolved, 0, len(schema.order))
	for _, key := range schema.order {
		declaration, _ := schema.Lookup(key)
		current := Resolved{Key: key, Value: declaration.Default, Source: SourceDefault}
		for _, scope := range path {
			layer, ok := layers[scope]
			if !ok || layer == nil {
				continue
			}
			value, ok := layer.Overrides[key]
			if !ok {
				continue
			}
			if err := validateValue(declaration.Kind, value); err != nil {
				return nil, fmt.Errorf("%w: %q in scope %q: %v", ErrInvalidValue, key, scope, err)
			}
			current = Resolved{Key: key, Value: value, Source: SourceScope, Scope: scope}
		}
		if value, ok := session[key]; ok {
			if err := validateValue(declaration.Kind, value); err != nil {
				return nil, fmt.Errorf("%w: %q in session: %v", ErrInvalidValue, key, err)
			}
			current = Resolved{Key: key, Value: value, Source: SourceSession}
		}
		resolved = append(resolved, current)
	}
	sort.SliceStable(resolved, func(i, j int) bool { return resolved[i].Key < resolved[j].Key })
	return resolved, nil
}
