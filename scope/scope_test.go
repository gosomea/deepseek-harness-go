package scope

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// newRoot builds an isolated runtime so tests do not share global registration.
func newRoot(t *testing.T) *cordis.Context {
	t.Helper()
	return cordis.New()
}

// closeRoot releases a test runtime and reports teardown failures.
func closeRoot(t *testing.T, root *cordis.Context) {
	t.Helper()
	if err := root.Close(context.Background()); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}

// TestCreateTagsContextAndDisposesContributions pins the core ownership rule:
// every registration made through a scope's context is disposed with it, and the
// context a registration receives reports that scope.
func TestCreateTagsContextAndDisposesContributions(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	agent := new(any)
	scoped, err := Create(root, agent)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if scoped.Ctx() == nil {
		t.Fatal("Ctx() = nil, want an active scoped context")
	}
	if got := ScopeOf(scoped.Ctx()); got != Key(agent) {
		t.Fatalf("ScopeOf(scoped ctx) = %v, want the minted key", got)
	}
	if got := ScopeOf(root); got != nil {
		t.Fatalf("ScopeOf(root) = %v, want nil for an unscoped context", got)
	}
	if scoped.Key() != Key(agent) {
		t.Fatalf("Key() = %v, want the minted key", scoped.Key())
	}
	if scoped.Fiber() == nil {
		t.Fatal("Fiber() = nil, want the owning activation")
	}

	disposed := false
	if _, err := scoped.Ctx().OnDispose("test:contribution", func() error {
		disposed = true
		return nil
	}); err != nil {
		t.Fatalf("OnDispose() error = %v", err)
	}
	if disposed {
		t.Fatal("contribution ran before Dispose")
	}
	if err := scoped.Dispose(); err != nil {
		t.Fatalf("Dispose() = %v, want nil", err)
	}
	if !disposed {
		t.Fatal("Dispose() did not release the scope's contribution")
	}
	if got := ScopeOf(scoped.Ctx()); got != nil {
		t.Fatalf("ScopeOf() after Dispose = %v, want the tag released", got)
	}
}

// TestDisposeIsIdempotent pins that a repeated or racing dispose unwinds once.
func TestDisposeIsIdempotent(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	scoped, err := Create(root, new(any))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	count := 0
	if _, err := scoped.Ctx().OnDispose("test:count", func() error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("OnDispose() error = %v", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := scoped.Dispose(); err != nil {
			t.Fatalf("Dispose() attempt %d = %v, want nil", attempt, err)
		}
	}
	if count != 1 {
		t.Fatalf("contribution ran %d times, want exactly 1", count)
	}
}

// TestSiblingScopesDoNotLeak pins the isolation contract: two sibling scopes see
// their own registrations and never each other's, while an unscoped observer sees
// every dispatch.
func TestSiblingScopesDoNotLeak(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	agentA, agentB := new(any), new(any)
	a, err := Create(root, agentA)
	if err != nil {
		t.Fatalf("Create(A) error = %v", err)
	}
	b, err := Create(root, agentB)
	if err != nil {
		t.Fatalf("Create(B) error = %v", err)
	}

	var heardA, heardB, heardGlobal []string
	// A listener registered through A's context belongs to A's scope.
	if _, err := a.Ctx().On("agent/status", func(event cordis.Event, _ cordis.Next) (any, error) {
		heardA = append(heardA, event.Args[0].(string))
		return nil, nil
	}, cordis.EventOptions{}); err != nil {
		t.Fatalf("A.On() error = %v", err)
	}
	if _, err := b.Ctx().On("agent/status", func(event cordis.Event, _ cordis.Next) (any, error) {
		heardB = append(heardB, event.Args[0].(string))
		return nil, nil
	}, cordis.EventOptions{}); err != nil {
		t.Fatalf("B.On() error = %v", err)
	}
	// An unscoped listener is global: it hears every dispatch.
	if _, err := root.On("agent/status", func(event cordis.Event, _ cordis.Next) (any, error) {
		heardGlobal = append(heardGlobal, event.Args[0].(string))
		return nil, nil
	}, cordis.EventOptions{}); err != nil {
		t.Fatalf("root.On() error = %v", err)
	}

	dispatch := func(carrier *Carrier, note string) {
		dispatcher := carrier.Filter(root)
		if _, err := dispatcher.Bail("agent/status", note); err != nil {
			t.Fatalf("Bail(%s) error = %v", note, err)
		}
	}
	dispatch(NewCarrier(agentA), "to-A")
	dispatch(NewCarrier(agentB), "to-B")

	if len(heardA) != 1 || heardA[0] != "to-A" {
		t.Fatalf("A heard %v, want only its own dispatch", heardA)
	}
	if len(heardB) != 1 || heardB[0] != "to-B" {
		t.Fatalf("B heard %v, want only its own dispatch", heardB)
	}
	if len(heardGlobal) != 2 {
		t.Fatalf("global heard %v, want both dispatches", heardGlobal)
	}
}

// TestEventAdmissionFlowsUpTheChain pins the documented direction: a listener
// tagged with an ancestor hears a descendant's dispatch, while a listener tagged
// with a descendant does not hear an ancestor's.
func TestEventAdmissionFlowsUpTheChain(t *testing.T) {
	parent, child, sibling := new(any), new(any), new(any)
	if _, err := BindParent(child, parent); err != nil {
		t.Fatalf("BindParent(child, parent) error = %v", err)
	}
	if _, err := BindParent(sibling, parent); err != nil {
		t.Fatalf("BindParent(sibling, parent) error = %v", err)
	}

	toChild := NewCarrier(child)
	if !toChild.Admit(nil) {
		t.Fatal("an untagged listener must always be admitted")
	}
	if !toChild.Admit(parent) {
		t.Fatal("an ancestor's listener must hear a descendant dispatch")
	}
	if !toChild.Admit(child) {
		t.Fatal("the scope's own listener must hear its dispatch")
	}
	if toChild.Admit(sibling) {
		t.Fatal("a sibling's listener must not hear this scope's dispatch")
	}

	toParent := NewCarrier(parent)
	if toParent.Admit(child) {
		t.Fatal("a descendant's listener must not hear an ancestor dispatch: events flow up only")
	}
	if !toParent.Admit(parent) {
		t.Fatal("the scope's own listener must hear its dispatch")
	}

	// An unkeyed carrier admits untagged listeners only.
	unkeyed := NewCarrier(nil)
	if !unkeyed.Admit(nil) {
		t.Fatal("an unkeyed carrier must admit an untagged listener")
	}
	if unkeyed.Admit(parent) {
		t.Fatal("an unkeyed carrier must not admit a tagged listener")
	}
}

// TestBindParentIsOnceAndCycleChecked pins the two rejections on the parent
// relation, including that only the returned binding may re-link.
func TestBindParentIsOnceAndCycleChecked(t *testing.T) {
	first, second, third := new(any), new(any), new(any)
	binding, err := BindParent(second, first)
	if err != nil {
		t.Fatalf("BindParent() error = %v", err)
	}
	if _, err := BindParent(second, third); !errors.Is(err, ErrAlreadyBound) {
		t.Fatalf("second BindParent() = %v, want ErrAlreadyBound", err)
	}
	if got := ParentOf(second); got != Key(first) {
		t.Fatalf("ParentOf(second) = %v, want the first parent", got)
	}
	// The holder may re-link.
	if err := binding.Rebind(third); err != nil {
		t.Fatalf("Rebind() error = %v", err)
	}
	if got := ParentOf(second); got != Key(third) {
		t.Fatalf("ParentOf(second) after Rebind = %v, want the new parent", got)
	}
	// A link that would close a cycle is refused, and the old link survives.
	if err := binding.Rebind(second); !errors.Is(err, ErrCycle) {
		t.Fatalf("Rebind(cycle) = %v, want ErrCycle", err)
	}
	if got := ParentOf(second); got != Key(third) {
		t.Fatalf("ParentOf(second) after a refused cycle = %v, want the link unchanged", got)
	}
	// A cycle through a grandparent is also refused.
	if _, err := BindParent(third, second); !errors.Is(err, ErrCycle) {
		t.Fatalf("BindParent(third, second) = %v, want ErrCycle", err)
	}
	// Nil handling.
	if _, err := BindParent(nil, first); err == nil {
		t.Fatal("BindParent(nil, ...) = nil, want an error")
	}
	if got := ChainOf(nil); len(got) != 0 {
		t.Fatalf("ChainOf(nil) = %v, want an empty chain", got)
	}
	if got := ParentOf(new(any)); got != nil {
		t.Fatalf("ParentOf(unbound) = %v, want nil", got)
	}
}

// TestChainIsNearestFirst pins the chain order every other read depends on.
func TestChainIsNearestFirst(t *testing.T) {
	rootKey, mid, leaf := new(any), new(any), new(any)
	if _, err := BindParent(mid, rootKey); err != nil {
		t.Fatalf("BindParent(mid) error = %v", err)
	}
	if _, err := BindParent(leaf, mid); err != nil {
		t.Fatalf("BindParent(leaf) error = %v", err)
	}
	chain := ChainOf(leaf)
	if len(chain) != 3 || chain[0] != Key(leaf) || chain[1] != Key(mid) || chain[2] != Key(rootKey) {
		t.Fatalf("ChainOf(leaf) = %v, want [leaf mid root]", chain)
	}
	if got := ChainOf(rootKey); len(got) != 1 || got[0] != Key(rootKey) {
		t.Fatalf("ChainOf(rootKey) = %v, want [rootKey]", got)
	}
}

// TestCarrierIdentityIsTracked pins that a carrier is distinguishable from an
// arbitrary value and that its key is readable.
func TestCarrierIdentityIsTracked(t *testing.T) {
	key := new(any)
	carrier := NewCarrier(key)
	if !IsCarrier(carrier) {
		t.Fatal("IsCarrier() = false for a minted carrier")
	}
	if got := CarrierKeyOf(carrier); got != Key(key) {
		t.Fatalf("CarrierKeyOf() = %v, want the minted key", got)
	}
	if IsCarrier("not a carrier") || IsCarrier(nil) {
		t.Fatal("IsCarrier() accepted a non-carrier")
	}
	if got := CarrierKeyOf(struct{}{}); got != nil {
		t.Fatalf("CarrierKeyOf(non-carrier) = %v, want nil", got)
	}
	unkeyed := NewCarrier(nil)
	if !IsCarrier(unkeyed) {
		t.Fatal("an unkeyed carrier is still a carrier")
	}
	if got := CarrierKeyOf(unkeyed); got != nil {
		t.Fatalf("CarrierKeyOf(unkeyed) = %v, want nil", got)
	}
	if got := (*Carrier)(nil).Key(); got != nil {
		t.Fatalf("nil carrier Key() = %v, want nil", got)
	}
	if got := (*Carrier)(nil).Admit(nil); !got {
		t.Fatal("a nil carrier must admit only an untagged listener")
	}
	if (*Carrier)(nil).Admit(key) {
		t.Fatal("a nil carrier must not admit a tagged listener")
	}
}

// TestCheckDispatch pins the runtime defence against a wrong or missing carrier,
// which Go cannot express as a type error.
func TestCheckDispatch(t *testing.T) {
	key, other := new(any), new(any)
	if err := CheckDispatch("agent/status", NewCarrier(key), key); err != nil {
		t.Fatalf("CheckDispatch() = %v, want nil", err)
	}
	if err := CheckDispatch("agent/status", nil, key); !errors.Is(err, ErrMissingCarrier) {
		t.Fatalf("CheckDispatch(nil carrier) = %v, want ErrMissingCarrier", err)
	}
	if err := CheckDispatch("agent/status", NewCarrier(other), key); !errors.Is(err, ErrDispatchSubject) {
		t.Fatalf("CheckDispatch(mismatched) = %v, want ErrDispatchSubject", err)
	}
}

// TestCreateRejectsMissingInputs pins the boundary values of Create.
func TestCreateRejectsMissingInputs(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)
	if _, err := Create(nil, new(any)); err == nil {
		t.Fatal("Create(nil ctx) = nil, want an error")
	}
	if _, err := Create(root, nil); err == nil {
		t.Fatal("Create(nil key) = nil, want an error")
	}
}

// TestCreateWithParentBindsBeforeApply pins that a scope created with a parent is
// already in the chain, so contributions made during activation see it.
func TestCreateWithParentBindsBeforeApply(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	parent, child := new(any), new(any)
	created, err := Create(root, child, WithParent(parent))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := ParentOf(child); got != Key(parent) {
		t.Fatalf("ParentOf(child) = %v, want the requested parent", got)
	}
	if got := ChainOf(child); len(got) != 2 {
		t.Fatalf("ChainOf(child) = %v, want two keys", got)
	}
	// A second explicit bind must fail: WithParent already linked it.
	if _, err := BindParent(child, new(any)); !errors.Is(err, ErrAlreadyBound) {
		t.Fatalf("BindParent() after WithParent = %v, want ErrAlreadyBound", err)
	}
	// Dispose releases the parent link, which is this package's explicit stand-in
	// for weak-map reclamation.
	if err := created.Dispose(); err != nil {
		t.Fatalf("Dispose() = %v, want nil", err)
	}
	if got := ParentOf(child); got != nil {
		t.Fatalf("ParentOf(child) after Dispose = %v, want the link released", got)
	}
	// RawDispose stays callable after Dispose: it is the same idempotent Cordis
	// disposer, so a second call reports no work.
	if err := created.RawDispose()(); err != nil {
		t.Fatalf("RawDispose()() after Dispose = %v, want nil", err)
	}
}

// TestRawDisposeIsUsableForOrderedComposite pins that RawDispose yields a
// disposer that a composite can order, rather than an already-run no-op.
func TestRawDisposeIsUsableForOrderedComposite(t *testing.T) {
	root := newRoot(t)
	defer closeRoot(t, root)

	scoped, err := Create(root, new(any))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	order := []string{}
	if _, err := root.OnDispose("test:inner", func() error {
		order = append(order, "inner")
		return nil
	}); err != nil {
		t.Fatalf("OnDispose(inner) error = %v", err)
	}
	if _, err := scoped.Ctx().OnDispose("test:scope-owned", func() error {
		order = append(order, "scope")
		return nil
	}); err != nil {
		t.Fatalf("OnDispose(scope) error = %v", err)
	}
	if err := scoped.RawDispose()(); err != nil {
		t.Fatalf("RawDispose()() = %v, want nil", err)
	}
	if len(order) == 0 || order[len(order)-1] != "scope" {
		t.Fatalf("order = %v, want the scope's own registration released", order)
	}
	if err := scoped.Dispose(); err != nil {
		t.Fatalf("Dispose() after RawDispose = %v, want nil", err)
	}
}
