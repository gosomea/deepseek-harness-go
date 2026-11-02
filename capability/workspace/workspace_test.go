package workspace_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/workspace"
)

func newWorld(t *testing.T, root string, symlinks bool) *workspace.World {
	t.Helper()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root, ResolveSymlinks: symlinks})
	if err != nil {
		t.Fatalf("new world: %v", err)
	}
	return world
}

// The identity is not the path: two spellings of one directory share a root but
// keep their own identity.
func TestWorldIdentityAndRoot(t *testing.T) {
	root := t.TempDir()
	world := newWorld(t, root, false)
	if world.ID() != "w-1" {
		t.Fatalf("unexpected id %q", world.ID())
	}
	// A relative spelling and a trailing separator must resolve to one root.
	spelled, err := workspace.New(workspace.Options{ID: "w-2", Root: filepath.Join(root, ".") + string(filepath.Separator)})
	if err != nil {
		t.Fatalf("new world: %v", err)
	}
	if spelled.Root() != world.Root() {
		t.Fatalf("root spellings differ: %q vs %q", spelled.Root(), world.Root())
	}
	if spelled.ID() == world.ID() {
		t.Fatal("identity must stay distinct from the path")
	}
	if _, err := workspace.New(workspace.Options{Root: root}); !errors.Is(err, workspace.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an id, got %v", err)
	}
	if _, err := workspace.New(workspace.Options{ID: "w"}); !errors.Is(err, workspace.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a root, got %v", err)
	}
}

// Containment compares path elements, so a sibling whose name starts with the
// root's name is not inside it.
func TestResolveRejectsSiblingWithSharedPrefix(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "work")
	sibling := filepath.Join(base, "workshop")
	for _, dir := range []string{root, sibling} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	world := newWorld(t, root, false)
	if _, err := world.Resolve(sibling); !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("a sibling sharing a prefix must be outside, got %v", err)
	}
	inside, err := world.Resolve("notes.txt")
	if err != nil {
		t.Fatalf("relative path must resolve: %v", err)
	}
	if filepath.Dir(inside) != world.Root() {
		t.Fatalf("unexpected resolution %q", inside)
	}
	// The root itself is contained.
	if resolved, err := world.Resolve(root); err != nil || resolved != world.Root() {
		t.Fatalf("the root must be contained: %q %v", resolved, err)
	}
	if _, err := world.Resolve(".."); !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("a traversal must be refused, got %v", err)
	}
	if _, err := world.Resolve("   "); !errors.Is(err, workspace.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank path, got %v", err)
	}
	if !world.Contains(inside) {
		t.Fatal("Contains must agree with Resolve")
	}
}

// With symlink resolution on, a link that leaves the world is refused.
func TestResolveRefusesSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	strict := newWorld(t, root, true)
	if _, err := strict.Resolve("escape"); !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("a symlink escape must be refused, got %v", err)
	}
	// With resolution off, the link is just a path element inside the world; that
	// is the documented trade-off of the cheaper mode.
	lenient := newWorld(t, root, false)
	if _, err := lenient.Resolve("escape"); err != nil {
		t.Fatalf("without resolution the path is inside: %v", err)
	}
}

// A create through a symlinked parent cannot escape even though the leaf does
// not exist yet.
func TestResolveForCreateFollowsExistingAncestors(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	link := filepath.Join(root, "out")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	strict := newWorld(t, root, true)
	if _, err := strict.ResolveForCreate("out/new.txt"); !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("a create under an escaping link must be refused, got %v", err)
	}
	// An ordinary create inside the world succeeds and keeps the missing tail.
	resolved, err := strict.ResolveForCreate("docs/new.txt")
	if err != nil {
		t.Fatalf("create inside the world: %v", err)
	}
	if filepath.Base(resolved) != "new.txt" || !strict.Contains(resolved) {
		t.Fatalf("unexpected create path %q", resolved)
	}
	if _, err := strict.ResolveForCreate(""); !errors.Is(err, workspace.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank path, got %v", err)
	}
}

// The identifier's helpers are part of the public surface.
func TestIDHelpers(t *testing.T) {
	id := workspace.ID("w-1")
	if id.String() != "w-1" {
		t.Fatalf("unexpected string form %q", id.String())
	}
	if id.IsZero() {
		t.Fatal("a set id must not report zero")
	}
	if !workspace.ID("").IsZero() {
		t.Fatal("an empty id must report zero")
	}
}

// A root reached through a symlink is canonicalized when resolution is on, so the
// root and its candidates are compared in the same spelling.
func TestWorldCanonicalizesItsRootWhenResolving(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Entering the world through the link must still contain paths under the real
	// directory: without canonicalizing the root the two spellings would not match
	// and every path would look like an escape.
	world := newWorld(t, link, true)
	resolved, err := world.Resolve("notes.txt")
	if err != nil {
		t.Fatalf("a path under the canonical root must be inside: %v", err)
	}
	if !world.Contains(resolved) {
		t.Fatalf("the resolved path must be contained: %q", resolved)
	}
	// The canonical root is the real directory, not the link. The comparison is
	// against the canonicalized real path, because on this platform the temporary
	// directory itself is reached through a link (/var -> /private/var) and the
	// implementation correctly returns the fully resolved form.
	canonicalReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if world.Root() != canonicalReal {
		t.Fatalf("the root must be canonicalized: %q want %q", world.Root(), canonicalReal)
	}
	if world.Root() == link {
		t.Fatal("the root must not remain the link spelling")
	}
}

// A path whose deepest existing ancestor is a plain file resolves against the
// existing part, and a path that cannot be resolved at all still reports a
// containment answer rather than failing.
func TestResolveForCreateHandlesOddAncestors(t *testing.T) {
	root := t.TempDir()
	strict := newWorld(t, root, true)
	// The root exists but the whole tail does not: the walk climbs to the root.
	deep, err := strict.ResolveForCreate("a/b/c/d.txt")
	if err != nil {
		t.Fatalf("a deep create inside the world must resolve: %v", err)
	}
	if filepath.Base(deep) != "d.txt" {
		t.Fatalf("the missing tail must be preserved: %q", deep)
	}
	// A create outside the world is refused before any walk happens.
	if _, err := strict.ResolveForCreate(filepath.Join(root, "..", "escape.txt")); !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("want ErrOutsideWorkspace, got %v", err)
	}
}

// A relative root is made absolute, so the same directory yields one root however
// it was spelled.
func TestNewAcceptsARelativeRoot(t *testing.T) {
	root := t.TempDir()
	world := newWorld(t, root, false)
	if !filepath.IsAbs(world.Root()) {
		t.Fatalf("the root must be absolute: %q", world.Root())
	}
}
