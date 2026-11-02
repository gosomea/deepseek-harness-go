package scope

import (
	"fmt"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// Layer is one scope's aggregate contribution to a registry.
//
// Reclaim asks whether the *whole aggregate* is empty, not whether one table is:
// a layer that still holds an anonymous entry must stay alive even after its
// named entries are gone, or the surviving entry would be silently dropped.
type Layer interface {
	// IsEmpty reports whether every table in this layer is empty.
	IsEmpty() bool
}

// Layers holds one eager global layer plus lazily created per-scope layers.
//
// Reads never create a layer: a lookup for a scope with no contributions sees the
// global layer and its ancestors and nothing else, and creating a layer on read
// would make an empty registration appear to exist. The global layer is eager
// because it is context-global, not scope-owned.
//
// L is the caller's own layer type; this package owns the chain and the
// lifecycle, and the caller owns what a layer contains.
type Layers[L Layer] struct {
	// global is the context-global layer, always present.
	global L
	// scoped maps each scope key to its layer, created on first write.
	scoped map[Key]L
	// create builds an empty layer. It receives nil for the global layer and the
	// scope key for a scoped one, so a caller can distinguish the two.
	create func(Key) L
	// onChange notifies a caller that a layer's contents changed, so a derived
	// view can be invalidated. It runs after the effect is registered.
	onChange func(Key)
}

// NewLayers creates a store with an eager global layer built by create(nil).
//
// create must return a layer for every key, including nil. onChange may be nil
// when no derived view needs invalidation.
func NewLayers[L Layer](create func(Key) L, onChange func(Key)) *Layers[L] {
	if create == nil {
		panic("scope: NewLayers needs a layer factory")
	}
	return &Layers[L]{global: create(nil), scoped: map[Key]L{}, create: create, onChange: onChange}
}

// Global returns the context-global layer. It always exists.
func (s *Layers[L]) Global() L { return s.global }

// Peek returns the layer registered for exactly scope, or the zero value and false.
//
// This read is deliberately chain-blind: a caller addressing a scope's *own*
// contributions (its restrictions, its guards) must not accidentally inherit an
// ancestor's. Use Chain for the visibility view. A nil scope always reports
// absent, because the global layer is reached through Global, not Peek.
func (s *Layers[L]) Peek(scope Key) (L, bool) {
	if scope == nil {
		var zero L
		return zero, false
	}
	layer, ok := s.scoped[scope]
	return layer, ok
}

// Chain returns the layers visible to scope, farthest ancestor first and the
// scope's own layer last, so that a later layer shadows an earlier one.
//
// Absent layers are skipped. The result never includes the global layer, which is
// already implicit in every view; use Global for it. Reads never create layers.
func (s *Layers[L]) Chain(scope Key) []L {
	var chain []L
	for _, key := range ChainOf(scope) {
		if layer, ok := s.scoped[key]; ok {
			chain = append(chain, layer)
		}
	}
	// ChainOf is nearest-first; a view applies the farthest ancestor first.
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	return chain
}

// Merge folds the entries visible to scope into one ordered view.
//
// pick extracts a table from a layer. The global layer seeds the view, then each
// layer in chain order shadows by name: a scoped value replaces the global value
// but keeps the global name's position, so a shadowed registration does not move
// in the presented order. Exactly one entry survives per name.
func (s *Layers[L]) Merge(scope Key, pick func(L) *NamedEntries[any]) *NamedEntries[any] {
	merged := NewNamedEntries[any](nil)
	collect := func(layer L) {
		table := pick(layer)
		if table == nil {
			return
		}
		for _, item := range table.Entries() {
			if merged.Has(item.Name) {
				// Shadow in place: replace the value, keep the position.
				merged.values[item.Name] = item.Value
				continue
			}
			merged.names = append(merged.names, item.Name)
			merged.values[item.Name] = item.Value
		}
	}
	collect(s.global)
	for _, layer := range s.Chain(scope) {
		collect(layer)
	}
	return merged
}

// Effect registers one contribution through ctx and returns the disposer that
// removes it.
//
// Visibility and ownership come from the same context: the scope the
// registration is filed under is also the scope that owns its disposal. That is
// what prevents a contribution from being visible in one scope while torn down
// with another.
//
// action runs *eagerly*, before Effect returns, so a rejected registration fails
// at the call site instead of surfacing later from a cleanup. It receives the
// scope's layer (created for this call when the scope had none) and returns the
// undo for exactly what it inserted, or an error.
//
// On failure the layer is dropped when this call created it and nothing landed in
// it, so a rejected registration cannot leak an empty layer. On success the
// returned disposer runs the undo, reclaims the layer when the whole aggregate is
// empty, then notifies. The disposer is the context's own, because cordis joins
// and orders teardown by disposer identity.
func (s *Layers[L]) Effect(ctx *cordis.Context, action func(L) (func(), error)) (cordis.Cleanup, error) {
	if ctx == nil {
		return nil, fmt.Errorf("scope: effect needs a context")
	}
	if action == nil {
		return nil, fmt.Errorf("scope: effect needs an action")
	}
	scope := ScopeOf(ctx)
	var layer L
	created := false
	if scope == nil {
		layer = s.global
	} else if existing, ok := s.scoped[scope]; ok {
		layer = existing
	} else {
		layer = s.create(scope)
		s.scoped[scope] = layer
		created = true
	}
	undo, err := action(layer)
	if err != nil {
		if created && layer.IsEmpty() {
			delete(s.scoped, scope)
		}
		return nil, err
	}
	if undo == nil {
		if created && layer.IsEmpty() {
			delete(s.scoped, scope)
		}
		return nil, fmt.Errorf("scope: effect action returned no undo for scope %v", scope)
	}
	// Notify only after the contribution is in place, so a derived view never
	// observes a partially registered layer.
	if s.onChange != nil {
		s.onChange(scope)
	}
	cleanup, registerErr := ctx.OnDispose("scope:contribution", func() error {
		undo()
		// Reclaim notifies when it actually drops a layer, so removal is reported
		// exactly once and a retained layer is not announced as gone.
		s.Reclaim(scope)
		return nil
	})
	if registerErr != nil {
		// Roll the contribution back: the activation refused to own it.
		undo()
		if created && layer.IsEmpty() {
			delete(s.scoped, scope)
		}
		return nil, registerErr
	}
	return cleanup, nil
}

// Reclaim drops a scope's layer when its whole aggregate is empty, and reports the
// change so a derived view can be invalidated.
//
// It is called by the owner after a contribution is removed, so a layer does not
// outlive its last entry. A layer that still holds an anonymous registration is
// retained, and that retention is reported too: the entry set changed even though
// the layer stayed.
func (s *Layers[L]) Reclaim(scope Key) {
	if scope == nil {
		return
	}
	layer, ok := s.scoped[scope]
	if ok && layer.IsEmpty() {
		delete(s.scoped, scope)
	}
	if s.onChange != nil {
		s.onChange(scope)
	}
}

// Len reports how many scope-owned layers exist. It exists so a caller can prove
// that reads do not create layers.
func (s *Layers[L]) Len() int { return len(s.scoped) }
