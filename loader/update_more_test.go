package loader_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gosomea/deepseek-harness-go/loader"
)

// TestApplyAddsNestedEntryUnderExistingGroup covers the add path for a path whose
// owning group already exists, including the group/child ordering.
func TestApplyAddsNestedEntryUnderExistingGroup(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"g","group":true,"config":[]}]}`)); err != nil {
		t.Fatal(err)
	}
	if got := tree.Root().Len(); got != 1 {
		t.Fatalf("root children = %d", got)
	}

	// Adding a child under the existing group must find that group already mounted.
	result, err := tree.Apply([]byte(`{"entries":[` +
		`{"id":"g","group":true,"config":[{"id":"kid","name":"leaf"}]}]}`))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := tree.Entry("g:kid"); !ok {
		t.Fatalf("g:kid should be mounted; added=%v failed=%v", result.Added, result.Failed)
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d", got)
	}
	group, _ := tree.Group("g")
	if group.Len() != 1 || group.Children()[0].ID() != "g:kid" {
		t.Fatalf("group children = %v", group.Children())
	}
}

// TestApplyOnClosedTreeRejects keeps Apply consistent with the other mutators.
func TestApplyOnClosedTreeRejects(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	if err := tree.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Apply([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); !errors.Is(err, loader.ErrTreeClosed) {
		t.Fatalf("Apply on a closed tree: %v", err)
	}
}

// TestUpdateDisableThenConfigKeepsIdentity covers updating a disabled entry: it
// stays disabled with no instance, and states stay reported consistently.
func TestUpdateDisableThenConfigKeepsIdentity(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Disable("a"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&counter); got != 0 {
		t.Fatalf("live resources = %d, want 0", got)
	}
	// A config change on a disabled entry keeps it disabled and unmounted.
	view, err := tree.Update("a", loader.Update{Config: raw(`{"k":1}`)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if view.ID != "a" || !view.Disabled || view.Runnable || view.State != "" {
		t.Fatalf("view = %+v", view)
	}
	if got := atomic.LoadInt64(&counter); got != 0 {
		t.Fatalf("live resources = %d, want 0", got)
	}
	if _, err := tree.Enable("a"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources after enable = %d, want 1", got)
	}
}

// TestMoveNestedGroupMovesDescendants covers rekeying a subtree, the path that
// rewrites several map keys at once.
func TestMoveNestedGroupMovesDescendants(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[` +
		`{"id":"src","group":true,"config":[` +
		`{"id":"mid","group":true,"config":[{"id":"deep","name":"leaf"}]}]},` +
		`{"id":"dst","group":true,"config":[]}]}`)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	deepBefore, _ := tree.Fiber("src:mid:deep")
	if deepBefore == nil {
		t.Fatal("src:mid:deep should be mounted")
	}

	view, err := tree.Move("src:mid", "dst")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if view.ID != "dst:mid" {
		t.Fatalf("moved group id = %q", view.ID)
	}
	// Every descendant path moved with it, and the instance survived.
	if _, ok := tree.Entry("dst:mid:deep"); !ok {
		t.Fatalf("descendant path should move; paths = %v", tree.Paths())
	}
	if _, ok := tree.Entry("src:mid:deep"); ok {
		t.Fatal("the old descendant path must not resolve")
	}
	deepAfter, ok := tree.Fiber("dst:mid:deep")
	if !ok || deepAfter != deepBefore {
		t.Fatal("a subtree move must preserve instances")
	}
	if owner, found := tree.Locate(deepAfter); !found || owner != "dst:mid:deep" {
		t.Fatalf("Locate = %q, %v", owner, found)
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1", got)
	}
	group, _ := tree.Group("dst:mid")
	if group.Owner() == nil || group.Owner().ID() != "dst:mid" {
		t.Fatalf("moved group owner = %v", group.Owner())
	}
}

// TestMoveToSameGroupIsNoOp keeps an idempotent move from being reported as a
// change or from disturbing the instance.
func TestMoveToSameGroupIsNoOp(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[` +
		`{"id":"g","group":true,"config":[{"id":"item","name":"leaf"}]}]}`)); err != nil {
		t.Fatal(err)
	}
	before, _ := tree.Fiber("g:item")
	view, err := tree.Move("g:item", "g")
	if err != nil {
		t.Fatalf("Move within the same group: %v", err)
	}
	if view.ID != "g:item" {
		t.Fatalf("view = %+v", view)
	}
	after, _ := tree.Fiber("g:item")
	if before != after {
		t.Fatal("a no-op move must not disturb the instance")
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d", got)
	}
}

// TestMoveRejectsDuplicateTargetPath keeps a move from silently overwriting.
func TestMoveRejectsDuplicateTargetPath(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[` +
		`{"id":"a","group":true,"config":[{"id":"same","name":"leaf"}]},` +
		`{"id":"b","group":true,"config":[{"id":"same","name":"leaf"}]}]}`)); err != nil {
		t.Fatal(err)
	}
	// a:same and b:same are distinct paths, so moving a:same into b collides.
	if _, err := tree.Move("a:same", "b"); !errors.Is(err, loader.ErrDuplicateID) {
		t.Fatalf("colliding move: %v", err)
	}
	if _, ok := tree.Entry("a:same"); !ok {
		t.Fatal("a rejected move must leave the source in place")
	}
	if got := atomic.LoadInt64(&counter); got != 2 {
		t.Fatalf("live resources = %d, want 2", got)
	}
}

// TestApplyRemovesGroupWithSubtree covers removal of a whole declared subgroup.
func TestApplyRemovesGroupWithSubtree(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if _, err := tree.Apply([]byte(`{"entries":[` +
		`{"id":"keep","name":"leaf"},` +
		`{"id":"gone","group":true,"config":[{"id":"kid","name":"leaf"}]}]}`)); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&counter); got != 2 {
		t.Fatalf("live resources = %d, want 2", got)
	}

	result, err := tree.Apply([]byte(`{"entries":[{"id":"keep","name":"leaf"}]}`))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "gone" {
		t.Fatalf("removed = %v", result.Removed)
	}
	for _, path := range []string{"gone", "gone:kid"} {
		if _, ok := tree.Entry(path); ok {
			t.Fatalf("%q should be removed", path)
		}
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1", got)
	}
}

// TestApplyRejectsBadGroupChildBeforeTouchingTree pins pre-validation for Apply.
func TestApplyRejectsBadGroupChildBeforeTouchingTree(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"keep","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}
	before := strings.Join(tree.Paths(), ",")
	_, err := tree.Apply([]byte(`{"entries":[` +
		`{"id":"g","group":true,"config":[{"id":"x","name":"leaf","nope":1}]}]}`))
	if !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("bad group child: %v", err)
	}
	if after := strings.Join(tree.Paths(), ","); after != before {
		t.Fatalf("tree changed: %q -> %q", before, after)
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1", got)
	}
}

// TestEntrySetGroupThroughMoveKeepsGroupLookup covers the group field an entry
// records when it is moved.
func TestEntrySetGroupThroughMoveKeepsGroupLookup(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[` +
		`{"id":"one","group":true,"config":[{"id":"item","name":"leaf"}]},` +
		`{"id":"two","group":true,"config":[]}]}`)); err != nil {
		t.Fatal(err)
	}
	entry, _ := tree.Entry("one:item")
	if entry == nil {
		t.Fatal("one:item should exist")
	}
	if _, err := tree.Move("one:item", "two"); err != nil {
		t.Fatal(err)
	}
	one, _ := tree.Group("one")
	two, _ := tree.Group("two")
	if one.Len() != 0 {
		t.Fatalf("source group still holds %d children", one.Len())
	}
	if two.Len() != 1 {
		t.Fatalf("target group holds %d children", two.Len())
	}
	// Dump must render the moved entry under its new path only.
	declared, err := tree.Dump(loader.Declared)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(declared, "one:item") || !strings.Contains(declared, "two:item") {
		t.Fatalf("dump after move:\n%s", declared)
	}
}
