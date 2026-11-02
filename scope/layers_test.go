package scope

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// testLayer is a minimal layer with one named table and one anonymous table, which
// is what a real registry aggregates. Keeping both shapes here pins that the
// reclaim predicate is "whole aggregate empty" rather than "one table empty".
type testLayer struct {
	named      *NamedEntries[string]
	anonymous  *AnonymousEntries[string]
	reclaimed  bool
	creatingOf Key
}

// newTestLayer builds an empty layer and records which scope it was made for.
func newTestLayer(key Key) *testLayer {
	return &testLayer{named: NewNamedEntries[string](nil), anonymous: NewAnonymousEntries[string](), creatingOf: key}
}

// IsEmpty reports whether every table in the layer is empty.
func (l *testLayer) IsEmpty() bool { return l.named.IsEmpty() && l.anonymous.IsEmpty() }

// TestLayersReadsNeverCreateLayers pins that a lookup does not materialize a
// layer: an empty registration must not appear to exist, and a scope nobody
// contributed to must not consume state.
func TestLayersReadsNeverCreateLayers(t *testing.T) {
	layers := NewLayers[*testLayer](newTestLayer, nil)
	scope := new(any)
	if layers.Len() != 0 {
		t.Fatalf("Len() = %d before any read, want 0", layers.Len())
	}
	if _, ok := layers.Peek(scope); ok {
		t.Fatal("Peek() reported a layer nobody created")
	}
	if chain := layers.Chain(scope); len(chain) != 0 {
		t.Fatalf("Chain() = %v, want no layer", chain)
	}
	if layers.Len() != 0 {
		t.Fatalf("Len() = %d after reads, want 0: reads must not create layers", layers.Len())
	}
	if layers.Global() == nil {
		t.Fatal("Global() = nil, want the eager global layer")
	}
}

// TestLayersPeekIsChainBlind pins the load-bearing distinction: Peek answers for a
// scope's own contributions only, so a caller addressing its own restrictions
// cannot accidentally inherit an ancestor's.
func TestLayersPeekIsChainBlind(t *testing.T) {
	layers := NewLayers[*testLayer](newTestLayer, nil)
	parent, child := new(any), new(any)
	if _, err := BindParent(child, parent); err != nil {
		t.Fatalf("BindParent() error = %v", err)
	}
	parentLayer := newTestLayer(parent)
	parentLayer.named.values["restriction"] = "parent"
	layers.scoped[parent] = parentLayer

	if _, ok := layers.Peek(child); ok {
		t.Fatal("Peek(child) saw an ancestor's layer")
	}
	if got, ok := layers.Peek(parent); !ok || !got.named.Has("restriction") {
		t.Fatal("Peek(parent) did not return its own layer")
	}
	if _, ok := layers.Peek(nil); ok {
		t.Fatal("Peek(nil) must always report absent; the global layer is reached through Global")
	}
	// Chain does see the ancestor.
	chain := layers.Chain(child)
	if len(chain) != 1 || !chain[0].named.Has("restriction") {
		t.Fatalf("Chain(child) = %v, want the ancestor's layer", chain)
	}
	// The layer a factory was asked to build records which scope it belongs to.
	if got := chain[0].creatingOf; got != Key(parent) {
		t.Fatalf("layer scope = %v, want the parent", got)
	}
}

// TestLayersMergeShadowsInPlace pins the shadow rule: a scoped value replaces a
// global value of the same name while keeping the global name's position, and
// exactly one entry survives.
func TestLayersMergeShadowsInPlace(t *testing.T) {
	layers := NewLayers[*testLayer](newTestLayer, nil)
	scope := new(any)
	// The global layer is built first, then a scoped layer is added directly so the
	// test drives Merge without an effect.
	layers.Global().named.values["a"] = "global-a"
	layers.Global().named.names = []string{"a"}
	layers.Global().named.values["b"] = "global-b"
	layers.Global().named.names = []string{"a", "b"}

	scoped := newTestLayer(scope)
	scoped.named.values["b"] = "scoped-b"
	scoped.named.names = []string{"b"}
	scoped.named.values["c"] = "scoped-c"
	scoped.named.names = []string{"b", "c"}
	layers.scoped[scope] = scoped

	merged := layers.Merge(scope, func(layer *testLayer) *NamedEntries[any] {
		converted := NewNamedEntries[any](nil)
		for _, item := range layer.named.Entries() {
			converted.names = append(converted.names, item.Name)
			converted.values[item.Name] = item.Value
		}
		return converted
	})
	if got := merged.Names(); strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("Merge() names = %v, want the global position of b preserved", got)
	}
	if value, _ := merged.Get("b"); value != "scoped-b" {
		t.Fatalf("Merge() b = %v, want the scoped value to win", value)
	}
	if value, _ := merged.Get("a"); value != "global-a" {
		t.Fatalf("Merge() a = %v, want the untouched global value", value)
	}
	if value, _ := merged.Get("c"); value != "scoped-c" {
		t.Fatalf("Merge() c = %v, want the scoped addition present", value)
	}
	// A pick function that returns nothing is tolerated.
	empty := layers.Merge(scope, func(*testLayer) *NamedEntries[any] { return nil })
	if !empty.IsEmpty() {
		t.Fatalf("Merge() with an empty pick = %v, want only the global layer", empty.Names())
	}
}

// TestLayersEffectOwnsContributionByRegistrationScope pins that the scope a
// contribution is filed under is the scope that owns its disposal, and that the
// action runs eagerly so a rejected registration fails at the call site.
func TestLayersEffectOwnsContributionByRegistrationScope(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	notified := []Key{}
	layers := NewLayers[*testLayer](newTestLayer, func(key Key) { notified = append(notified, key) })
	agent := new(any)
	scoped, err := Create(root, agent)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	cleanup, err := layers.Effect(scoped.Ctx(), func(layer *testLayer) (func(), error) {
		if layer.creatingOf != Key(agent) {
			t.Fatalf("layer scope = %v, want the registration context's scope", layer.creatingOf)
		}
		return layer.named.Insert("tool", "scoped-tool")
	})
	if err != nil {
		t.Fatalf("Effect() error = %v", err)
	}
	// The action ran eagerly, so the contribution is visible before any dispose.
	layer, ok := layers.Peek(agent)
	if !ok || !layer.named.Has("tool") {
		t.Fatal("Effect() did not register the contribution eagerly")
	}
	if len(notified) != 1 || notified[0] != Key(agent) {
		t.Fatalf("onChange saw %v, want the contribution's scope", notified)
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() = %v, want nil", err)
	}
	if _, ok := layers.Peek(agent); ok {
		t.Fatal("cleanup() did not reclaim the layer whose whole aggregate became empty")
	}
	if len(notified) != 2 || notified[1] != Key(agent) {
		t.Fatalf("onChange ran %v, want exactly one notification per change", notified)
	}
}

// TestLayersEffectRegistersInTheGlobalLayerForAnUnscopedContext pins that a
// registration through the root context lands in the eager global layer and is
// owned by that activation.
func TestLayersEffectRegistersInTheGlobalLayerForAnUnscopedContext(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	layers := NewLayers[*testLayer](newTestLayer, nil)
	if ScopeOf(root) != nil {
		t.Fatal("the root context must be unscoped")
	}
	cleanup, err := layers.Effect(root, func(layer *testLayer) (func(), error) {
		if layer != layers.Global() {
			t.Fatal("an unscoped registration must land in the global layer")
		}
		return layer.named.Insert("global-tool", "v")
	})
	if err != nil {
		t.Fatalf("Effect() error = %v", err)
	}
	if layers.Len() != 0 {
		t.Fatalf("Len() = %d, want no scoped layer for an unscoped registration", layers.Len())
	}
	if !layers.Global().named.Has("global-tool") {
		t.Fatal("the global contribution is missing")
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() = %v, want nil", err)
	}
}

// TestLayersEffectRollsBackAFailedAction pins that a rejected registration fails
// at the call site and does not leak the layer it was about to fill.
func TestLayersEffectRollsBackAFailedAction(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	layers := NewLayers[*testLayer](newTestLayer, nil)
	agent := new(any)
	scoped, err := Create(root, agent)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	want := errors.New("registration rejected")
	if _, err := layers.Effect(scoped.Ctx(), func(*testLayer) (func(), error) { return nil, want }); !errors.Is(err, want) {
		t.Fatalf("Effect() = %v, want the action error at the call site", err)
	}
	if _, ok := layers.Peek(agent); ok {
		t.Fatal("a failed action leaked an empty layer")
	}
	// An action that returns no undo is a programming error and is reported.
	if _, err := layers.Effect(scoped.Ctx(), func(*testLayer) (func(), error) { return nil, nil }); err == nil {
		t.Fatal("Effect() = nil, want an error for an action without an undo")
	}
	if _, ok := layers.Peek(agent); ok {
		t.Fatal("an undo-less action leaked an empty layer")
	}
	// A layer that a surviving anonymous contribution holds is NOT dropped.
	if _, err := layers.Effect(scoped.Ctx(), func(layer *testLayer) (func(), error) {
		return layer.anonymous.Append("guard"), nil
	}); err != nil {
		t.Fatalf("Effect(anonymous) error = %v", err)
	}
	if _, err := layers.Effect(scoped.Ctx(), func(*testLayer) (func(), error) { return nil, want }); !errors.Is(err, want) {
		t.Fatalf("Effect() = %v, want the action error", err)
	}
	if _, ok := layers.Peek(agent); !ok {
		t.Fatal("a rejected action dropped a layer that still held a contribution")
	}
}

// TestLayersEffectRejectsNilContext pins the boundary case.
func TestLayersEffectRejectsNilContext(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)
	layers := NewLayers[*testLayer](newTestLayer, nil)
	if _, err := layers.Effect(nil, func(*testLayer) (func(), error) { return nil, nil }); err == nil {
		t.Fatal("Effect(nil) = nil, want an error")
	}
	if _, err := layers.Effect(root, nil); err == nil {
		t.Fatal("Effect(nil action) = nil, want an error")
	}
}

// TestLayersReclaimKeepsANonEmptyAggregate pins the reclaim predicate: a layer
// with only anonymous entries stays alive.
func TestLayersReclaimKeepsANonEmptyAggregate(t *testing.T) {
	layers := NewLayers[*testLayer](newTestLayer, nil)
	scope := new(any)
	layer := newTestLayer(scope)
	layer.anonymous.Append("guard")
	layers.scoped[scope] = layer

	layers.Reclaim(scope)
	if _, ok := layers.Peek(scope); !ok {
		t.Fatal("Reclaim() dropped a layer that still held an anonymous registration")
	}
	// Reclaim on an unknown scope announces a no-op change and keeps the retained
	// layer; Reclaim(nil) is a no-op with no notification.
	layers.Reclaim(new(any))
	layers.Reclaim(nil)
	if layers.Len() != 1 {
		t.Fatalf("Len() = %d, want the retained layer", layers.Len())
	}
}

// TestNewLayersRequiresAFactory pins the programming-error guard.
func TestNewLayersRequiresAFactory(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewLayers(nil) did not panic")
		}
	}()
	NewLayers[*testLayer](nil, nil)
}

// TestDisposeReleasesParentLinkWithoutRetainingDeadKeys pins this package's
// explicit stand-in for weak-map reclamation.
func TestDisposeReleasesParentLinkWithoutRetainingDeadKeys(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	agent := new(any)
	scoped, err := Create(root, agent)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := BindParent(agent, new(any)); err != nil {
		t.Fatalf("BindParent() error = %v", err)
	}
	if len(ChainOf(agent)) != 2 {
		t.Fatal("the parent link was not recorded")
	}
	if err := scoped.Dispose(); err != nil {
		t.Fatalf("Dispose() = %v", err)
	}
	if got := ChainOf(agent); len(got) != 1 || got[0] != Key(agent) {
		t.Fatalf("ChainOf() after Dispose = %v, want the key with its parent link released", got)
	}
	if got := ParentOf(agent); got != nil {
		t.Fatalf("ParentOf() after Dispose = %v, want nil", got)
	}
}

// TestCarrierFilterAdmitsGloballyMarkedListeners pins that the dispatcher this
// package builds still lets a global listener through, because an unscoped
// observer must hear every dispatch.
func TestCarrierFilterAdmitsGloballyMarkedListeners(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	agentA, agentB := new(any), new(any)
	a, err := Create(root, agentA)
	if err != nil {
		t.Fatalf("Create(A) error = %v", err)
	}
	var heardTagged, heardGlobal int
	if _, err := a.Ctx().On("ev", func(cordis.Event, cordis.Next) (any, error) {
		heardTagged++
		return nil, nil
	}, cordis.EventOptions{}); err != nil {
		t.Fatalf("A.On() error = %v", err)
	}
	if _, err := root.On("ev", func(cordis.Event, cordis.Next) (any, error) {
		heardGlobal++
		return nil, nil
	}, cordis.EventOptions{Global: true}); err != nil {
		t.Fatalf("root.On(global) error = %v", err)
	}
	dispatcher := NewCarrier(agentB).Filter(root)
	if err := dispatcher.Emit("ev"); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	if heardTagged != 0 {
		t.Fatalf("A's tagged listener heard a B dispatch %d times, want 0", heardTagged)
	}
	if heardGlobal != 1 {
		t.Fatalf("the global listener heard %d dispatches, want 1", heardGlobal)
	}
	// A scope-filtered dispatch always needs a live context; a nil base is a
	// programming error reported at the call site rather than a nil panic inside
	// cordis on first use.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Filter(nil) did not report a missing context")
			}
		}()
		NewCarrier(agentA).Filter(nil)
	}()
}

// TestChainOfContextWithoutTag pins that a context this package never tagged is
// reported as unscoped rather than being mistaken for a scope.
func TestChainOfContextWithoutTag(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)
	if got := ScopeOf(nil); got != nil {
		t.Fatalf("ScopeOf(nil) = %v, want nil", got)
	}
	if got := ScopeOf(root); got != nil {
		t.Fatalf("ScopeOf(root) = %v, want nil", got)
	}
}

// TestCloseReleasesScopes pins that closing the runtime runs every scope's cleanup,
// including the parent-link release.
func TestCloseReleasesScopes(t *testing.T) {
	root := newRoot(t)
	agent := new(any)
	scoped, err := Create(root, agent)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := BindParent(agent, new(any)); err != nil {
		t.Fatalf("BindParent() error = %v", err)
	}
	released := false
	if _, err := scoped.Ctx().OnDispose("test:resource", func() error {
		released = true
		return nil
	}); err != nil {
		t.Fatalf("OnDispose() error = %v", err)
	}
	if err := root.Close(context.Background()); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if !released {
		t.Fatal("Close() did not release the scope's contribution")
	}
	if got := ParentOf(agent); got != nil {
		t.Fatalf("ParentOf() after Close = %v, want the parent link released", got)
	}
}
