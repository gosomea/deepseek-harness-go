package loader_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

// resourcePlugin records how many live resources it holds and releases them on
// cleanup, so a test can assert the tree actually released everything.
func resourcePlugin(name string, counter *int64) loader.Factory {
	return func() cordis.Plugin {
		return cordis.Plugin{
			Name: name,
			Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
				atomic.AddInt64(counter, 1)
				return func() error {
					atomic.AddInt64(counter, -1)
					return nil
				}, nil
			},
		}
	}
}

func treeCatalog(t *testing.T, counter *int64, names ...string) *loader.Catalog {
	t.Helper()
	catalog := loader.NewCatalog()
	for _, name := range names {
		if err := catalog.Register(name, resourcePlugin(name, counter)); err != nil {
			t.Fatal(err)
		}
	}
	return catalog
}

// TestNestedIdentityAndOwnerAgree pins the first required proof: a nested entry's
// reported path is its full path, and the plugin instance binds to that same
// path in both directions.
func TestNestedIdentityAndOwnerAgree(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() {
		if err := tree.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	document := `{"entries":[{"id":"outer","group":true,"config":[` +
		`{"id":"mid","group":true,"config":[{"id":"leaf","name":"leaf"}]},` +
		`{"id":"direct","name":"leaf"}]}]}`
	if err := tree.Load([]byte(document)); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// A nested entry reports its full path, not its local id.
	for _, path := range []string{"outer", "outer:mid", "outer:mid:leaf", "outer:direct"} {
		if _, ok := tree.Entry(path); !ok {
			t.Fatalf("entry %q should exist; paths = %v", path, tree.Paths())
		}
		if _, ok := tree.Entry("leaf"); ok {
			t.Fatal("a local id must not also resolve as a top-level path")
		}
	}
	if got := tree.Paths(); len(got) != 4 {
		t.Fatalf("paths = %v, want 4 entries", got)
	}

	// Entry -> Fiber -> Locate must agree for every mounted plugin.
	for _, path := range []string{"outer:mid:leaf", "outer:direct"} {
		fiber, ok := tree.Fiber(path)
		if !ok {
			t.Fatalf("entry %q should have a fiber", path)
		}
		owner, ok := tree.Locate(fiber)
		if !ok || owner != path {
			t.Fatalf("Locate(fiber at %q) = %q, %v", path, owner, ok)
		}
	}

	// A group entry has no instance but owns a nested group.
	if _, ok := tree.Fiber("outer"); ok {
		t.Fatal("a group entry must not have a plugin instance")
	}
	group, ok := tree.Group("outer:mid")
	if !ok {
		t.Fatal("nested group outer:mid should exist")
	}
	if group.ID() != "outer:mid" {
		t.Fatalf("group id = %q", group.ID())
	}
	owner, ok := tree.Entry("outer:mid")
	if !ok || group.Owner() != owner {
		t.Fatal("a nested group must be owned by the entry that declared it")
	}
	if got := group.Parent(); got == nil || got.ID() != "outer" {
		t.Fatalf("group parent = %v, want outer", got)
	}
	if got := group.Len(); got != 1 {
		t.Fatalf("outer:mid children = %d, want 1", got)
	}
	if got := tree.Root().Entries(); len(got) != 4 {
		t.Fatalf("root subtree entries = %d, want 4", len(got))
	}
	if got := atomic.LoadInt64(&counter); got != 2 {
		t.Fatalf("live resources = %d, want 2 (only leaf plugins run)", got)
	}
}

// TestCloseReleasesEveryEntry pins the second required proof: after closing the
// root tree, every entry's plugin released what it acquired and the tree reports
// no remaining instance.
func TestCloseReleasesEveryEntry(t *testing.T) {
	var counter int64
	var applied int64
	catalog := loader.NewCatalog()
	if err := catalog.Register("leaf", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "leaf",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				atomic.AddInt64(&applied, 1)
				atomic.AddInt64(&counter, 1)
				return func() error { atomic.AddInt64(&counter, -1); return nil }, nil
			},
		}
	}); err != nil {
		t.Fatal(err)
	}
	tree := loader.NewTree(catalog)

	document := `{"entries":[
	  {"id":"a","name":"leaf"},
	  {"id":"grp","group":true,"config":[
	    {"id":"b","name":"leaf"},
	    {"id":"off","name":"leaf","disabled":true}
	  ]},
	  {"id":"c","name":"leaf"}
	]}`
	if err := tree.Load([]byte(document)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 3 {
		t.Fatalf("live resources before close = %d, want 3", got)
	}
	if got := atomic.LoadInt64(&applied); got != 3 {
		t.Fatalf("applied = %d, want 3 (disabled entry must not run)", got)
	}
	if lines := tree.Diagnose(); len(lines) != 0 {
		t.Fatalf("healthy tree should diagnose nothing, got %v", lines)
	}

	if err := tree.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 0 {
		t.Fatalf("live resources after close = %d, want 0", got)
	}
	if !tree.Closed() {
		t.Fatal("tree should report closed")
	}
	if got := len(tree.Diagnose()); got != 0 {
		t.Fatalf("a closed tree should have nothing to report, got %d lines", got)
	}
	// Closing again is idempotent and must not double-release.
	if err := tree.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 0 {
		t.Fatalf("live resources after second close = %d, want 0", got)
	}
}

// TestMountAfterClosePublishesNothing pins that a closed tree never accepts new work.
func TestMountAfterClosePublishesNothing(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	if err := tree.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := tree.Load([]byte(`{"entries":[{"id":"late","name":"leaf"}]}`))
	if !errors.Is(err, loader.ErrTreeClosed) {
		t.Fatalf("loading a closed tree: %v", err)
	}
	if _, ok := tree.Entry("late"); ok {
		t.Fatal("a rejected entry must not be published")
	}
	if got := atomic.LoadInt64(&counter); got != 0 {
		t.Fatalf("no plugin should have run, live resources = %d", got)
	}
}

// TestCloseDuringLoadDoesNotPublishStaleEntry pins the third required proof: an
// entry whose Apply is still running when the tree closes must be disposed and
// never published, because publishing it would leave a resource without an owner.
func TestCloseDuringLoadDoesNotPublishStaleEntry(t *testing.T) {
	var counter int64
	entered := make(chan struct{})
	release := make(chan struct{})
	catalog := loader.NewCatalog()
	if err := catalog.Register("slow", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "slow",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				atomic.AddInt64(&counter, 1)
				close(entered)
				<-release
				return func() error { atomic.AddInt64(&counter, -1); return nil }, nil
			},
		}
	}); err != nil {
		t.Fatal(err)
	}
	tree := loader.NewTree(catalog)

	loadDone := make(chan error, 1)
	go func() {
		loadDone <- tree.Load([]byte(`{"entries":[{"id":"slow","name":"slow"}]}`))
	}()
	<-entered

	closeDone := make(chan error, 1)
	go func() { closeDone <- tree.Close(context.Background()) }()

	// Give Close a chance to mark the tree closed while Apply is still blocked,
	// then let Apply finish. Without the closed check the entry would publish.
	deadline := time.After(2 * time.Second)
	for !tree.Closed() {
		select {
		case <-deadline:
			t.Fatal("Close did not mark the tree closed")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(release)

	if err := <-loadDone; !errors.Is(err, loader.ErrTreeClosed) {
		t.Fatalf("load during close: %v", err)
	}
	if _, ok := tree.Entry("slow"); ok {
		t.Fatal("a stale entry must not be published")
	}
	if _, ok := tree.Fiber("slow"); ok {
		t.Fatal("a stale instance must not be bound")
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 0 {
		t.Fatalf("stale instance resources = %d, want 0 (it must be disposed)", got)
	}
}

// TestDumpShowsDeclaredAndRunningShapes pins that one configuration yields two
// comparable views: what the document declares and what the tree runs.
func TestDumpShowsDeclaredAndRunningShapes(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()

	document := `{"entries":[
	  {"id":"grp","group":true,"config":[
	    {"id":"live","name":"leaf"},
	    {"id":"off","name":"leaf","disabled":true}
	  ]}
	]}`
	if err := tree.Load([]byte(document)); err != nil {
		t.Fatalf("Load: %v", err)
	}

	declared, err := tree.Dump(loader.Declared)
	if err != nil {
		t.Fatal(err)
	}
	running, err := tree.Dump(loader.Running)
	if err != nil {
		t.Fatal(err)
	}

	// Both shapes indent nested entries and share the path, so a reader can align them.
	if !strings.Contains(declared, "grp [group]") {
		t.Fatalf("declared dump: %s", declared)
	}
	if !strings.Contains(declared, "  grp:live [plugin]") {
		t.Fatalf("nested entry should be indented: %s", declared)
	}
	if !strings.Contains(declared, "grp:off [plugin] disabled") {
		t.Fatalf("declared dump should mark disabled: %s", declared)
	}
	if strings.Contains(declared, "state=") {
		t.Fatalf("the declared shape must not report runtime state: %s", declared)
	}
	if !strings.Contains(running, "grp:live [plugin] state=active") {
		t.Fatalf("running dump: %s", running)
	}
	if !strings.Contains(running, "grp [group] state=not-mounted") {
		t.Fatalf("a group has no instance: %s", running)
	}
	// A disabled entry is declared but never mounted, which is the difference the
	// two shapes exist to show.
	if !strings.Contains(running, "grp:off [plugin] disabled state=not-mounted") {
		t.Fatalf("disabled entry should be declared but unmounted: %s", running)
	}
	if got := strings.Count(declared, "\n"); got != 3 {
		t.Fatalf("declared dump lines = %d, want 3", got)
	}

	// Dumping is deterministic.
	again, err := tree.Dump(loader.Declared)
	if err != nil {
		t.Fatal(err)
	}
	if again != declared {
		t.Fatal("two dumps of an unchanged tree must match")
	}
	if _, err := tree.Dump(loader.Shape("bogus")); !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("unknown shape: %v", err)
	}
}

// TestConcurrentLookupsAreSafe runs lookups against a loaded tree to catch data
// races in the mounting maps.
func TestConcurrentLookupsAreSafe(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()

	var document strings.Builder
	document.WriteString(`{"entries":[`)
	for index := 0; index < 8; index++ {
		if index > 0 {
			document.WriteString(",")
		}
		fmt.Fprintf(&document, `{"id":"n%d","name":"leaf"}`, index)
	}
	document.WriteString("]}")
	if err := tree.Load([]byte(document.String())); err != nil {
		t.Fatalf("Load: %v", err)
	}

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for iteration := 0; iteration < 50; iteration++ {
				path := fmt.Sprintf("n%d", (worker+iteration)%8)
				if _, ok := tree.Entry(path); !ok {
					t.Errorf("entry %q missing", path)
					return
				}
				if fiber, ok := tree.Fiber(path); ok {
					if owner, found := tree.Locate(fiber); !found || owner != path {
						t.Errorf("Locate(%q) = %q, %v", path, owner, found)
						return
					}
				}
				_ = tree.Paths()
				_, _ = tree.Dump(loader.Running)
			}
		}(worker)
	}
	wg.Wait()
}

// TestLoadRejectsDuplicatePathAndBadGroupChildren keeps tree-level failures
// observable instead of silently dropping entries.
func TestLoadRejectsDuplicatePathAndBadGroupChildren(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()

	// The same local id under one group is a duplicate path.
	err := tree.Load([]byte(`{"entries":[{"id":"g","group":true,"config":[{"id":"x","name":"leaf"},{"id":"x","name":"leaf"}]}]}`))
	if !errors.Is(err, loader.ErrDuplicateID) {
		t.Fatalf("duplicate nested id: %v", err)
	}
	// A path separator inside a local id would make two entries collide.
	err = tree.Load([]byte(`{"entries":[{"id":"a:b","name":"leaf"}]}`))
	if !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("separator in id: %v", err)
	}
	// An unknown field inside group children is a configuration error.
	err = tree.Load([]byte(`{"entries":[{"id":"g","group":true,"config":[{"id":"x","name":"leaf","nope":1}]}]}`))
	if !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("bad group child: %v", err)
	}
	// A plugin failure during mount is reported with the entry path.
	failing := loader.NewCatalog()
	if err := failing.Register("boom", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "boom",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				return nil, errors.New("apply failed")
			},
		}
	}); err != nil {
		t.Fatal(err)
	}
	broken := loader.NewTree(failing)
	defer func() { _ = broken.Close(context.Background()) }()
	err = broken.Load([]byte(`{"entries":[{"id":"bad","name":"boom"}]}`))
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("mount failure should name the entry: %v", err)
	}
}

// TestEntryAndGroupLookupMisses keeps absent lookups distinguishable from present ones.
func TestEntryAndGroupLookupMisses(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if _, ok := tree.Entry("absent"); ok {
		t.Fatal("absent entry should not resolve")
	}
	if _, ok := tree.Fiber("absent"); ok {
		t.Fatal("absent fiber should not resolve")
	}
	if _, ok := tree.Locate(nil); ok {
		t.Fatal("nil fiber has no owner")
	}
	if _, ok := tree.Group(""); !ok {
		t.Fatal("the root group is addressed by the empty id")
	}
	if _, ok := tree.Group("absent"); ok {
		t.Fatal("absent group should not resolve")
	}
	if got := tree.Root().Children(); len(got) != 0 {
		t.Fatalf("empty tree children = %v", got)
	}
	if got := tree.Root().Entries(); len(got) != 0 {
		t.Fatalf("empty tree entries = %v", got)
	}
	if err := tree.Load(nil); !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("nil document: %v", err)
	}
}

// TestTreeCloseWithCanceledContextReportsTheBoundary keeps a bounded close honest.
func TestTreeCloseWithCanceledContextReportsTheBoundary(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	if err := tree.Load([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Cancellation bounds the wait only, so Close may return before cleanup
	// finishes. Two things must hold: the tree is marked closed regardless, and
	// the resources are released once the work is awaited with a live context.
	if err := tree.Close(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Close returned an unexpected error: %v", err)
	}
	if !tree.Closed() {
		t.Fatal("a bounded Close must still mark the tree closed")
	}
	// Await the cleanup that the bounded wait may have left running.
	if err := tree.Context().Wait(context.Background()); err != nil {
		t.Fatalf("waiting for cleanup: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 0 {
		t.Fatalf("live resources = %d, want 0", got)
	}
}
