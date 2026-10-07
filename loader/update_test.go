package loader_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

func raw(value string) *json.RawMessage {
	message := json.RawMessage(value)
	return &message
}

// TestApplyParseFailureLeavesTreeUntouched pins that a malformed document is
// rejected before anything changes, so the running tree keeps every entry and
// every live resource.
func TestApplyParseFailureLeavesTreeUntouched(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}
	before, _ := tree.Dump(loader.Running)

	for _, broken := range []string{
		`{"entries":[{"id":"a","name":"leaf"},]}`,                         // syntax error
		`{"entries":[{"id":"a","name":"leaf","nope":1}]}`,                 // unknown field
		`{"entries":[{"id":"a","name":"leaf"},{"id":"a","name":"leaf"}]}`, // duplicate id
	} {
		if _, err := tree.Apply([]byte(broken)); !errors.Is(err, loader.ErrInvalidConfig) && !errors.Is(err, loader.ErrDuplicateID) {
			t.Fatalf("document %s: %v", broken, err)
		}
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1: a rejected document must not touch the tree", got)
	}
	after, _ := tree.Dump(loader.Running)
	if before != after {
		t.Fatalf("running tree changed after a rejected document:\n%v\n%v", before, after)
	}
	if paths := tree.Paths(); len(paths) != 1 || paths[0] != "a" {
		t.Fatalf("paths = %v", paths)
	}
}

// TestApplyReusesPathWithoutChangingIdentity pins that an entry keeps its id
// when the document reuses that id with a different plugin, and that the old
// instance is released rather than leaked.
func TestApplyReusesPathWithoutChangingIdentity(t *testing.T) {
	var first, second int64
	catalog := loader.NewCatalog()
	if err := catalog.Register("one", resourcePlugin("one", &first)); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Register("two", resourcePlugin("two", &second)); err != nil {
		t.Fatal(err)
	}
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"x","name":"one"}]}`)); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&first); got != 1 {
		t.Fatalf("first = %d", got)
	}

	result, err := tree.Apply([]byte(`{"entries":[{"id":"x","name":"two"}]}`))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Updated) != 1 || result.Updated[0] != "x" {
		t.Fatalf("updated = %v", result.Updated)
	}
	// The identity is the path, which the document did not change.
	view, ok := tree.View("x")
	if !ok || view.ID != "x" || view.Name != "two" {
		t.Fatalf("view = %+v, %v", view, ok)
	}
	if got := atomic.LoadInt64(&first); got != 0 {
		t.Fatalf("the replaced instance must be released, first = %d", got)
	}
	if got := atomic.LoadInt64(&second); got != 1 {
		t.Fatalf("second = %d", got)
	}
}

// TestConfigUpdateReloadsInstanceInPlace pins that a configuration change keeps
// the instance identity: the same fiber is updated rather than replaced.
func TestConfigUpdateReloadsInstanceInPlace(t *testing.T) {
	var counter int64
	var sawConfig atomic.Value
	catalog := loader.NewCatalog()
	if err := catalog.Register("worker", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "worker",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				atomic.AddInt64(&counter, 1)
				return func() error { atomic.AddInt64(&counter, -1); return nil }, nil
			},
			Validate: func(config any) (any, error) {
				sawConfig.Store(config)
				return config, nil
			},
		}
	}); err != nil {
		t.Fatal(err)
	}
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"w","name":"worker","config":{"level":1}}]}`)); err != nil {
		t.Fatal(err)
	}
	before, _ := tree.Fiber("w")
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d", got)
	}

	if _, err := tree.Update("w", loader.Update{Config: raw(`{"level":2}`)}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, _ := tree.Fiber("w")
	if before != after {
		t.Fatal("a configuration change must reload the same instance, not replace it")
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1 (no remount)", got)
	}
}

// TestUpdateRejectsInvalidCandidateBeforeTouchingTheTree pins that an update
// whose new declaration cannot resolve leaves both declaration and instance as
// they were, so a typo never damages a healthy entry.
func TestUpdateRejectsInvalidCandidateBeforeTouchingTheTree(t *testing.T) {
	var counter int64
	catalog := loader.NewCatalog()
	if err := catalog.Register("strict", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "strict",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				atomic.AddInt64(&counter, 1)
				return func() error { atomic.AddInt64(&counter, -1); return nil }, nil
			},
			Validate: func(config any) (any, error) {
				if _, ok := config.(string); !ok {
					return nil, errors.New("strict needs a string config")
				}
				return config, nil
			},
		}
	}); err != nil {
		t.Fatal(err)
	}
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"s","name":"strict","config":"ok"}]}`)); err != nil {
		t.Fatal(err)
	}

	if _, err := tree.Update("s", loader.Update{Config: raw(`123`)}); err == nil {
		t.Fatal("an invalid candidate must be rejected")
	}
	view, _ := tree.View("s")
	if view.Name != "strict" || view.State != cordis.Active {
		t.Fatalf("the running entry must be untouched: %+v", view)
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1", got)
	}
	// An unknown plugin name is rejected the same way.
	if _, err := tree.Update("s", loader.Update{Name: stringPtr("missing")}); !errors.Is(err, loader.ErrUnknownPlugin) {
		t.Fatalf("unknown name: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1", got)
	}
}

func stringPtr(value string) *string { return &value }

// TestDisableKeepsIdentityAndGatesConsumer is the C05 reader experiment: a
// consumer waits while its provider is disabled, and returns to the same entry
// id and resource count when the provider is enabled again.
func TestDisableKeepsIdentityAndGatesConsumer(t *testing.T) {
	var providerLive, consumerLive int64
	catalog := loader.NewCatalog()
	// The provider must publish the service the consumer injects; otherwise the
	// consumer would stay pending forever and the experiment would prove nothing.
	if err := catalog.Register("store", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "store",
			Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
				atomic.AddInt64(&providerLive, 1)
				if _, err := ctx.Provide("store", "store-value"); err != nil {
					return nil, err
				}
				return func() error { atomic.AddInt64(&providerLive, -1); return nil }, nil
			},
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Register("consumer", func() cordis.Plugin {
		return cordis.Plugin{
			Name:   "consumer",
			Inject: []string{"store"},
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				atomic.AddInt64(&consumerLive, 1)
				return func() error { atomic.AddInt64(&consumerLive, -1); return nil }, nil
			},
		}
	}); err != nil {
		t.Fatal(err)
	}

	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	// The consumer starts pending because its provider is not mounted yet.
	if err := tree.Load([]byte(`{"entries":[{"id":"consumer","name":"consumer"}]}`)); err != nil {
		t.Fatal(err)
	}
	if view, _ := tree.View("consumer"); view.State != cordis.Pending {
		t.Fatalf("consumer before its provider = %s, want pending", view.State)
	}
	if got := atomic.LoadInt64(&consumerLive); got != 0 {
		t.Fatalf("consumer resources = %d, want 0", got)
	}

	// Adding the provider activates the consumer.
	if _, err := tree.Apply([]byte(`{"entries":[{"id":"consumer","name":"consumer"},{"id":"store","name":"store"}]}`)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := atomic.LoadInt64(&providerLive); got != 1 {
		t.Fatalf("provider resources = %d", got)
	}
	if got := atomic.LoadInt64(&consumerLive); got != 1 {
		t.Fatalf("consumer resources = %d, want 1 once its provider is up", got)
	}
	providerView, _ := tree.View("store")
	if providerView.State != cordis.Active {
		t.Fatalf("provider state = %s", providerView.State)
	}

	// Disabling the provider returns the consumer to pending without losing ids.
	disabled, err := tree.Disable("store")
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if !disabled.Disabled || disabled.Runnable || disabled.ID != "store" {
		t.Fatalf("disabled view = %+v", disabled)
	}
	if disabled.State != "" {
		t.Fatalf("a disabled entry must have no instance state, got %q", disabled.State)
	}
	consumerView, _ := tree.View("consumer")
	if consumerView.State != cordis.Pending {
		t.Fatalf("consumer after disabling its provider = %s, want pending", consumerView.State)
	}
	if got := atomic.LoadInt64(&providerLive); got != 0 {
		t.Fatalf("disabled provider resources = %d, want 0", got)
	}
	if got := atomic.LoadInt64(&consumerLive); got != 0 {
		t.Fatalf("consumer resources = %d, want 0 while gated", got)
	}

	// Enabling it again restores the entry and the consumer.
	enabled, err := tree.Enable("store")
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if enabled.Disabled || !enabled.Runnable || enabled.ID != "store" {
		t.Fatalf("enabled view = %+v", enabled)
	}
	if got := atomic.LoadInt64(&providerLive); got != 1 {
		t.Fatalf("provider resources after enable = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&consumerLive); got != 1 {
		t.Fatalf("consumer resources after enable = %d, want 1", got)
	}
	// The identity did not change across the whole cycle.
	restored, _ := tree.View("store")
	if restored.ID != providerView.ID || restored.Name != providerView.Name {
		t.Fatalf("identity changed: %+v -> %+v", providerView, restored)
	}
}

// TestApplyReportsPartialFailureWithoutRollback pins the non-transactional
// semantics: one entry's activation failure does not undo the entries that
// already applied, and the result names exactly which paths changed.
func TestApplyReportsPartialFailureWithoutRollback(t *testing.T) {
	var goodLive int64
	catalog := loader.NewCatalog()
	if err := catalog.Register("good", resourcePlugin("good", &goodLive)); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Register("boom", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "boom",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				return nil, errors.New("apply failed")
			},
		}
	}); err != nil {
		t.Fatal(err)
	}
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()

	result, err := tree.Apply([]byte(`{"entries":[` +
		`{"id":"first","name":"good"},` +
		`{"id":"broken","name":"boom"},` +
		`{"id":"second","name":"good"}]}`))
	if err == nil {
		t.Fatal("a failing entry must be reported")
	}
	if len(result.Failed) != 1 || result.Failed[0] != "broken" {
		t.Fatalf("failed = %v", result.Failed)
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("the error should name the failing path: %v", err)
	}
	// The entries that succeeded stay applied: applying is not transactional.
	for _, path := range []string{"first", "second"} {
		if _, ok := tree.Entry(path); !ok {
			t.Fatalf("%q applied before the failure and must remain", path)
		}
	}
	if got := atomic.LoadInt64(&goodLive); got != 2 {
		t.Fatalf("live resources = %d, want 2", got)
	}
	if len(result.Added) != 2 {
		t.Fatalf("added = %v", result.Added)
	}
}

// TestSamePluginUnderDifferentIdsStaysIndependent pins that one catalog name can
// back several entries without them sharing identity or state.
func TestSamePluginUnderDifferentIdsStaysIndependent(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if _, err := tree.Apply([]byte(`{"entries":[
		{"id":"a","name":"leaf"},{"id":"b","name":"leaf"}]}`)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 2 {
		t.Fatalf("live resources = %d, want 2", got)
	}
	fa, _ := tree.Fiber("a")
	fb, _ := tree.Fiber("b")
	if fa == fb {
		t.Fatal("the same plugin name under two ids must produce two instances")
	}
	if _, err := tree.Disable("a"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("disabling one entry must not affect the other: %d", got)
	}
	if view, _ := tree.View("b"); view.State != cordis.Active {
		t.Fatalf("b = %+v", view)
	}
}

// TestDuplicateIdIsRejectedAndUpdateKeepsIdentity pins the duplicate rule and
// that an update never changes an entry's id.
func TestDuplicateIdIsRejectedAndUpdateKeepsIdentity(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if _, err := tree.Apply([]byte(`{"entries":[{"id":"a","name":"leaf"},{"id":"a","name":"leaf"}]}`)); !errors.Is(err, loader.ErrDuplicateID) {
		t.Fatalf("duplicate id: %v", err)
	}
	if err := tree.Load([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}

	view, err := tree.Update("a", loader.Update{Name: stringPtr("leaf")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if view.ID != "a" {
		t.Fatalf("id changed to %q", view.ID)
	}
	// Updating to the same declaration is a no-op, not a remount.
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1", got)
	}
}

// TestMoveChangesPathAndPreservesInstance pins the cross-group move rule: the
// path moves with the entry, the instance survives, and the group bookkeeping
// follows.
func TestMoveChangesPathAndPreservesInstance(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[` +
		`{"id":"left","group":true,"config":[{"id":"item","name":"leaf"}]},` +
		`{"id":"right","group":true,"config":[]}]}`)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	before, _ := tree.Fiber("left:item")
	if before == nil {
		t.Fatal("left:item should be mounted")
	}

	view, err := tree.Move("left:item", "right")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if view.ID != "right:item" {
		t.Fatalf("moved id = %q, want right:item", view.ID)
	}
	if _, ok := tree.Entry("left:item"); ok {
		t.Fatal("the old path must no longer resolve")
	}
	after, ok := tree.Fiber("right:item")
	if !ok || after != before {
		t.Fatal("a move must preserve the instance")
	}
	if owner, found := tree.Locate(after); !found || owner != "right:item" {
		t.Fatalf("Locate = %q, %v", owner, found)
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1", got)
	}
	left, _ := tree.Group("left")
	right, _ := tree.Group("right")
	if left.Len() != 0 || right.Len() != 1 {
		t.Fatalf("group children after move: left=%d right=%d", left.Len(), right.Len())
	}
	if got := right.Children()[0]; got.ID() != "right:item" {
		t.Fatalf("right child = %q", got.ID())
	}
}

// TestMoveRejectsInvalidTargets keeps each move failure distinguishable.
func TestMoveRejectsInvalidTargets(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[` +
		`{"id":"g","group":true,"config":[` +
		`{"id":"inner","group":true,"config":[{"id":"deep","name":"leaf"}]},` +
		`{"id":"item","name":"leaf"}]},` +
		`{"id":"other","name":"leaf"}]}`)); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, err := tree.Move("g", "g:inner"); !errors.Is(err, loader.ErrMoveIntoSubtree) {
		t.Fatalf("moving a group into its own subtree: %v", err)
	}
	if _, err := tree.Move("g", "g"); !errors.Is(err, loader.ErrMoveIntoSubtree) {
		t.Fatalf("moving into itself: %v", err)
	}
	if _, err := tree.Move("missing", "g"); !errors.Is(err, loader.ErrEntryNotFound) {
		t.Fatalf("missing source: %v", err)
	}
	if _, err := tree.Move("other", "missing"); !errors.Is(err, loader.ErrEntryNotFound) {
		t.Fatalf("missing target: %v", err)
	}
	if _, err := tree.Move("other", "g:item"); !errors.Is(err, loader.ErrEntryNotFound) {
		t.Fatalf("a non-group target: %v", err)
	}
	// Moving into the group it already belongs to is a no-op, not an error.
	view, err := tree.Move("other", "")
	if err != nil {
		t.Fatalf("moving to the root group: %v", err)
	}
	if view.ID != "other" {
		t.Fatalf("view = %+v", view)
	}
}

// TestUpdateOnGroupAndMissingEntryAreDistinct keeps the two lookup failures apart.
func TestUpdateOnGroupAndMissingEntryAreDistinct(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"g","group":true,"config":[]}]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Update("g", loader.Update{}); !errors.Is(err, loader.ErrGroupNotUpdatable) {
		t.Fatalf("update on a group: %v", err)
	}
	if _, err := tree.Update("nope", loader.Update{}); !errors.Is(err, loader.ErrEntryNotFound) {
		t.Fatalf("update on a missing entry: %v", err)
	}
}

// TestDependencyFailureAndInvalidConfigStayDistinct keeps the two diagnostics
// separate: an unavailable service is a pending state, a malformed declaration
// is an error.
func TestDependencyFailureAndInvalidConfigStayDistinct(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	if err := catalog.Register("needs", func() cordis.Plugin {
		return cordis.Plugin{
			Name:   "needs",
			Inject: []string{"absent-service"},
			Apply:  func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, nil },
		}
	}); err != nil {
		t.Fatal(err)
	}
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()

	// An unmet dependency is pending, not an error.
	result, err := tree.Apply([]byte(`{"entries":[{"id":"n","name":"needs"}]}`))
	if err != nil {
		t.Fatalf("an unmet dependency must not fail the apply: %v", err)
	}
	if len(result.Added) != 1 {
		t.Fatalf("added = %v", result.Added)
	}
	view, _ := tree.View("n")
	if view.State != cordis.Pending {
		t.Fatalf("state = %s, want pending", view.State)
	}
	if lines := tree.Diagnose(); len(lines) != 1 || !strings.Contains(lines[0], "pending") {
		t.Fatalf("pending should be diagnosed: %v", lines)
	}

	// An invalid declaration is an error and does not touch the tree.
	if _, err := tree.Apply([]byte(`{"entries":[{"id":"n","name":"needs","nope":1}]}`)); !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("invalid declaration: %v", err)
	}
	if got, _ := tree.View("n"); got.State != cordis.Pending {
		t.Fatalf("the entry must be untouched: %+v", got)
	}
}

// TestApplyRemovesOmittedEntries pins that a document is the tree's full
// declaration, so an omitted path is removed and its resources released.
func TestApplyRemovesOmittedEntries(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if _, err := tree.Apply([]byte(`{"entries":[{"id":"a","name":"leaf"},{"id":"b","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&counter); got != 2 {
		t.Fatalf("live resources = %d", got)
	}

	result, err := tree.Apply([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "b" {
		t.Fatalf("removed = %v", result.Removed)
	}
	if len(result.Unchanged) != 1 || result.Unchanged[0] != "a" {
		t.Fatalf("unchanged = %v", result.Unchanged)
	}
	if _, ok := tree.Entry("b"); ok {
		t.Fatal("b should be removed")
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1", got)
	}
}

// TestRemoveReleasesSubtree keeps removal total for a group and its children.
func TestRemoveReleasesSubtree(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[` +
		`{"id":"g","group":true,"config":[` +
		`{"id":"inner","group":true,"config":[{"id":"deep","name":"leaf"}]},` +
		`{"id":"side","name":"leaf"}]},` +
		`{"id":"keep","name":"leaf"}]}`)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 3 {
		t.Fatalf("live resources = %d, want 3", got)
	}
	if err := tree.Remove("g"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, path := range []string{"g", "g:inner", "g:inner:deep", "g:side"} {
		if _, ok := tree.Entry(path); ok {
			t.Fatalf("%q should be removed", path)
		}
		if _, ok := tree.Group(path); ok {
			t.Fatalf("group %q should be removed", path)
		}
	}
	if _, ok := tree.Entry("keep"); !ok {
		t.Fatal("an unrelated entry must survive")
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("live resources = %d, want 1", got)
	}
	if err := tree.Remove("g"); !errors.Is(err, loader.ErrEntryNotFound) {
		t.Fatalf("removing twice: %v", err)
	}
}

// TestUpdateAfterCloseIsRejected keeps a closed tree closed for every mutator.
func TestUpdateAfterCloseIsRejected(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	if err := tree.Load([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}
	if err := tree.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Update("a", loader.Update{}); !errors.Is(err, loader.ErrTreeClosed) {
		t.Fatalf("Update after close: %v", err)
	}
	if _, err := tree.Move("a", ""); !errors.Is(err, loader.ErrTreeClosed) {
		t.Fatalf("Move after close: %v", err)
	}
	if err := tree.Remove("a"); !errors.Is(err, loader.ErrTreeClosed) {
		t.Fatalf("Remove after close: %v", err)
	}
	if _, err := tree.Apply([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); !errors.Is(err, loader.ErrTreeClosed) {
		t.Fatalf("Apply after close: %v", err)
	}
}
