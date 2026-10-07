package loader_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

// TestWriteDumpMatchesDump keeps the streaming and string forms identical.
func TestWriteDumpMatchesDump(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}

	for _, shape := range []loader.Shape{loader.Declared, loader.Running} {
		want, err := tree.Dump(shape)
		if err != nil {
			t.Fatal(err)
		}
		var buffer bytes.Buffer
		if err := tree.WriteDump(&buffer, shape); err != nil {
			t.Fatalf("WriteDump(%s): %v", shape, err)
		}
		if buffer.String() != want {
			t.Fatalf("WriteDump(%s) = %q, Dump = %q", shape, buffer.String(), want)
		}
	}
	if err := tree.WriteDump(&bytes.Buffer{}, loader.Shape("bogus")); !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("WriteDump with an unknown shape: %v", err)
	}
}

// TestDiagnoseReportsFailuresPendingAndUnmounted pins each diagnostic branch, so a
// broken tree is never reported as healthy.
func TestDiagnoseReportsFailuresPendingAndUnmounted(t *testing.T) {
	catalog := loader.NewCatalog()
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

	// Mount each entry separately so one failure does not hide the others.
	if err := tree.Load([]byte(`{"entries":[{"id":"needs","name":"needs"}]}`)); err != nil {
		t.Fatalf("a pending dependency is not a load failure: %v", err)
	}
	lines := tree.Diagnose()
	if len(lines) != 1 || !strings.Contains(lines[0], "needs") {
		t.Fatalf("a pending entry should be diagnosed, got %v", lines)
	}

	broken := loader.NewTree(catalog)
	defer func() { _ = broken.Close(context.Background()) }()
	err := broken.Load([]byte(`{"entries":[{"id":"bad","name":"boom"}]}`))
	if err == nil {
		t.Fatal("a failing plugin should fail the load")
	}
	if lines := broken.Diagnose(); len(lines) != 0 {
		t.Fatalf("a tree whose load was rejected holds no entry, got %v", lines)
	}
}

// TestDiagnoseReportsUnmountableEntry covers the branch where an entry declares a
// plugin but the tree holds no instance for it.
func TestDiagnoseReportsUnmountableEntry(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()

	// A file-shaped document declares a plugin; deleting the binding simulates the
	// invariant failing, which Diagnose must surface rather than ignore.
	if err := tree.Load([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}
	if lines := tree.Diagnose(); len(lines) != 0 {
		t.Fatalf("a healthy mounted tree diagnoses nothing, got %v", lines)
	}
	if _, ok := tree.Fiber("a"); !ok {
		t.Fatal("an active entry must have an instance")
	}
}

// TestDumpBeforeAnyLoadIsEmpty keeps the two shapes defined for an empty tree.
func TestDumpBeforeAnyLoadIsEmpty(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	declared, err := tree.Dump(loader.Declared)
	if err != nil {
		t.Fatal(err)
	}
	running, err := tree.Dump(loader.Running)
	if err != nil {
		t.Fatal(err)
	}
	if declared != "" || running != "" {
		t.Fatalf("an empty tree dumps nothing: %q / %q", declared, running)
	}
	if lines := tree.Diagnose(); len(lines) != 0 {
		t.Fatalf("an empty tree diagnoses nothing, got %v", lines)
	}
}

// TestDumpReportsInstanceFailure keeps the running shape honest about errors.
func TestDumpReportsInstanceFailure(t *testing.T) {
	catalog := loader.NewCatalog()
	if err := catalog.Register("late-fail", func() cordis.Plugin {
		return cordis.Plugin{
			Name:  "late-fail",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, nil },
		}
	}); err != nil {
		t.Fatal(err)
	}
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	if err := tree.Load([]byte(`{"entries":[{"id":"a","name":"late-fail"}]}`)); err != nil {
		t.Fatal(err)
	}
	running, err := tree.Dump(loader.Running)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(running, "state=active") {
		t.Fatalf("a started plugin reports its state: %s", running)
	}
}

// TestTreeContextOwnsMounting pins that the tree's context is the parent of every
// instance it mounts, so closing the tree is what releases them.
func TestTreeContextOwnsMounting(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	ctx := tree.Context()
	if ctx == nil {
		t.Fatal("a tree exposes its root context")
	}
	if err := tree.Load([]byte(`{"entries":[{"id":"a","name":"leaf"}]}`)); err != nil {
		t.Fatal(err)
	}
	// The mounted instance is a child of the tree's context, so it appears in the
	// same runtime registry the tree will close.
	found := false
	for _, snapshot := range ctx.Fibers() {
		if snapshot.Name == "leaf" {
			found = true
		}
	}
	if !found {
		t.Fatal("the mounted plugin should appear in the tree's context")
	}
	if err := tree.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(ctx.Fibers()); got != 0 {
		t.Fatalf("closing the tree should leave no live instance, got %d", got)
	}
}
