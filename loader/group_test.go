package loader_test

import (
	"context"
	"testing"

	"github.com/gosomea/deepseek-harness-go/loader"
)

// TestGroupTraversalPinsTheDocumentedTree builds one document with two nesting
// levels and checks the group queries that a configuration dump relies on.
func TestGroupTraversalPinsTheDocumentedTree(t *testing.T) {
	var counter int64
	catalog := treeCatalog(t, &counter, "leaf")
	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()

	document := `{"entries":[` +
		`{"id":"outer","group":true,"config":[` +
		`{"id":"inner","group":true,"config":[{"id":"deep","name":"leaf"}]},` +
		`{"id":"side","name":"leaf"}]}]}`
	if err := tree.Load([]byte(document)); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := tree.Context(); got == nil {
		t.Fatal("Context must expose the root context that owns the tree")
	}

	root := tree.Root()
	if root.ID() != "" {
		t.Fatalf("root group id = %q, want empty", root.ID())
	}
	if root.Owner() != nil {
		t.Fatal("the root group has no owning entry")
	}
	if root.Parent() != nil {
		t.Fatal("the root group has no parent")
	}
	if root.Len() != 1 {
		t.Fatalf("root children = %d, want 1", root.Len())
	}

	// Children returns only direct children; Entries walks the whole subtree.
	outer, ok := tree.Group("outer")
	if !ok {
		t.Fatal("outer group should exist")
	}
	if outer.Len() != 2 {
		t.Fatalf("outer children = %d, want 2", outer.Len())
	}
	if got := outer.Parent(); got == nil || got.ID() != "" {
		t.Fatalf("outer parent = %v, want the root group", got)
	}
	if owner, ok := tree.Entry("outer"); !ok || outer.Owner() != owner {
		t.Fatal("outer group must be owned by the entry that declared it")
	}

	children := outer.Children()
	if len(children) != 2 || children[0].ID() != "outer:inner" || children[1].ID() != "outer:side" {
		t.Fatalf("outer children = %v", []string{children[0].ID(), children[1].ID()})
	}
	// The returned slice is a copy: reordering it must not reorder the tree.
	children[0], children[1] = children[1], children[0]
	if again := outer.Children(); again[0].ID() != "outer:inner" {
		t.Fatal("reordering a returned children slice must not affect the tree")
	}
	if got := outer.Entries(); len(got) != 3 {
		t.Fatalf("outer subtree entries = %d, want 3", len(got))
	}

	// A group is addressable from another group as well as from the tree.
	nested, ok := outer.Group("outer:inner")
	if !ok {
		t.Fatal("a group should be resolvable from another group")
	}
	if nested.ID() != "outer:inner" {
		t.Fatalf("nested group id = %q", nested.ID())
	}
	if _, ok := outer.Group("absent"); ok {
		t.Fatal("an absent group should not resolve")
	}

	// Tree.Entries returns every mounted entry in declaration order. The document
	// declares four: two groups and the two plugins the deeper group and the side
	// entry name.
	entries := tree.Entries()
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(entries))
	}
	want := []string{"outer", "outer:inner", "outer:inner:deep", "outer:side"}
	for index, path := range want {
		if entries[index].ID() != path {
			t.Fatalf("entries[%d] = %q, want %q", index, entries[index].ID(), path)
		}
	}
}

// TestGroupWithDetachedTreeReportsNoLookup keeps the zero Group from panicking.
func TestGroupWithDetachedTreeReportsNoLookup(t *testing.T) {
	group := &loader.Group{}
	if _, ok := group.Group("anything"); ok {
		t.Fatal("a group without a tree must not resolve a lookup")
	}
	if group.ID() != "" || group.Owner() != nil || group.Parent() != nil {
		t.Fatal("a zero group reports empty identity")
	}
}
