package scope

import (
	"fmt"
	"slices"
)

// NamedEntries is an insertion-ordered table of uniquely named values.
//
// Order is part of the contract, not an implementation detail: a registry built
// on this table presents its entries in registration order, and a scoped layer
// shadows a global entry without moving it. A plain Go map would lose both, so
// the table keeps an explicit name order alongside the values.
//
// Values are borrowed. The table never copies or freezes what a caller stores;
// materialization belongs to the domain that owns the value's meaning.
type NamedEntries[V any] struct {
	// names is the insertion order, with removed names deleted in place.
	names []string
	// values maps each live name to its value.
	values map[string]V
	// duplicate builds the error for a name already present. It is caller-supplied
	// so each registry owns its own diagnostic vocabulary.
	duplicate func(name string) error
}

// NewNamedEntries creates an empty table whose duplicate diagnostics come from
// duplicate. A nil duplicate function produces a plain ErrDuplicateEntry message.
func NewNamedEntries[V any](duplicate func(name string) error) *NamedEntries[V] {
	if duplicate == nil {
		duplicate = func(name string) error {
			return fmt.Errorf("%w: %q", ErrDuplicateEntry, name)
		}
	}
	return &NamedEntries[V]{values: map[string]V{}, duplicate: duplicate}
}

// Insert stores one value under name and returns an idempotent undo for exactly
// that insertion.
//
// A name already present in this table is rejected with the caller's error: a
// duplicate inside one layer is a registration conflict, whereas the same name in
// a different layer is a deliberate shadow. The undo removes only this insertion,
// so disposing one contribution cannot remove another's.
func (t *NamedEntries[V]) Insert(name string, value V) (func(), error) {
	if _, exists := t.values[name]; exists {
		return nil, t.duplicate(name)
	}
	t.values[name] = value
	t.names = append(t.names, name)
	active := true
	return func() {
		if !active {
			return
		}
		active = false
		if _, exists := t.values[name]; !exists {
			return
		}
		delete(t.values, name)
		for index, candidate := range t.names {
			if candidate == name {
				t.names = append(t.names[:index], t.names[index+1:]...)
				break
			}
		}
	}, nil
}

// Get returns the value stored under name, or the zero value and false.
func (t *NamedEntries[V]) Get(name string) (V, bool) {
	value, ok := t.values[name]
	return value, ok
}

// Has reports whether name is present.
func (t *NamedEntries[V]) Has(name string) bool {
	_, ok := t.values[name]
	return ok
}

// Names returns the live names in insertion order. The result is a copy, so a
// caller cannot reorder the table.
func (t *NamedEntries[V]) Names() []string { return slices.Clone(t.names) }

// Entries returns the live name/value pairs in insertion order.
func (t *NamedEntries[V]) Entries() []NamedEntry[V] {
	result := make([]NamedEntry[V], 0, len(t.names))
	for _, name := range t.names {
		result = append(result, NamedEntry[V]{Name: name, Value: t.values[name]})
	}
	return result
}

// IsEmpty reports whether the table has no live entries.
func (t *NamedEntries[V]) IsEmpty() bool { return t.values != nil && len(t.values) == 0 }

// NamedEntry is one name/value pair read from a NamedEntries table.
type NamedEntry[V any] struct {
	// Name is the entry's registration name.
	Name string
	// Value is the borrowed value stored under that name.
	Value V
}

// AnonymousEntries is an insertion-ordered table of independently identified
// values.
//
// Equal values stay separate registrations, which is what a table of guards or
// restrictions needs: two identical restrictions registered by different owners
// are two contributions, each with its own undo.
type AnonymousEntries[V any] struct {
	// order lists each entry's identity token in insertion order.
	order []*entry[V]
	// next mints identity tokens so equal values remain distinct.
	next func() *entry[V]
}

// entry is one anonymous registration: an identity plus a borrowed value.
type entry[V any] struct {
	value    V
	sequence uint64
}

// NewAnonymousEntries creates an empty table.
func NewAnonymousEntries[V any]() *AnonymousEntries[V] {
	table := &AnonymousEntries[V]{}
	table.next = func() *entry[V] {
		created := &entry[V]{sequence: uint64(len(table.order)) + 1}
		return created
	}
	return table
}

// Append stores one value under a fresh identity and returns an idempotent undo
// for exactly that registration.
func (t *AnonymousEntries[V]) Append(value V) func() {
	created := t.next()
	created.value = value
	t.order = append(t.order, created)
	active := true
	return func() {
		if !active {
			return
		}
		active = false
		for index, candidate := range t.order {
			if candidate == created {
				t.order = append(t.order[:index], t.order[index+1:]...)
				break
			}
		}
	}
}

// Values returns the live values in insertion order.
func (t *AnonymousEntries[V]) Values() []V {
	result := make([]V, 0, len(t.order))
	for _, item := range t.order {
		result = append(result, item.value)
	}
	return result
}

// Len reports the number of live registrations.
func (t *AnonymousEntries[V]) Len() int { return len(t.order) }

// IsEmpty reports whether the table has no live registrations.
func (t *AnonymousEntries[V]) IsEmpty() bool { return len(t.order) == 0 }
