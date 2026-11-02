package local_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/fs"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
	"github.com/gosomea/deepseek-harness-go/plugins/local"
)

// newFS builds a confined filesystem over a fresh temporary world.
func newFS(t *testing.T, mode fs.Mode) (*local.OSFS, string) {
	t.Helper()
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root, ResolveSymlinks: true})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	provider, err := local.NewFS(local.FSConfig{World: world, Mode: mode})
	if err != nil {
		t.Fatalf("fs: %v", err)
	}
	return provider, world.Root()
}

// A write inside the world lands there and is readable back.
func TestWriteAndReadRoundTrip(t *testing.T) {
	provider, root := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	written, err := provider.Write(ctx, fs.WriteRequest{Path: "notes/a.txt", Content: []byte("hello")})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if written.Size != 5 || written.IsDir {
		t.Fatalf("unexpected stat %+v", written)
	}
	read, err := provider.Read(ctx, "notes/a.txt")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(read.Content) != "hello" {
		t.Fatalf("unexpected content %q", read.Content)
	}
	// The recorded modification time is what a conditional write cites back.
	if read.Modified.IsZero() {
		t.Fatal("a read must report the modification time")
	}
	// The path stays inside the world.
	if !strings.HasPrefix(read.Path, root) {
		t.Fatalf("the read path escaped the world: %q", read.Path)
	}
	// Listing is sorted by name.
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "notes/b.txt", Content: []byte("b")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := provider.List(ctx, "notes")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 || entries[0].Name != "a.txt" || entries[1].Name != "b.txt" {
		t.Fatalf("the listing must be sorted, got %+v", entries)
	}
}

// A read-only provider refuses to write, and does so before touching the disk.
func TestReadOnlyRefusesWrites(t *testing.T) {
	provider, root := newFS(t, fs.ModeReadOnly)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "a.txt", Content: []byte("x")}); err == nil {
		t.Fatal("a read-only provider must refuse a write")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("a refused write must not create the file")
	}
	if err := provider.Remove(ctx, "a.txt"); err == nil {
		t.Fatal("a read-only provider must refuse a removal")
	}
}

// A path outside the world is refused by the provider, not by the caller.
func TestPathsOutsideTheWorldAreRefused(t *testing.T) {
	provider, root := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	outside := filepath.Join(root, "..", "escape.txt")
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: outside, Content: []byte("x")}); !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("want ErrOutsideWorkspace, got %v", err)
	}
	if _, err := provider.Read(ctx, outside); !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("want ErrOutsideWorkspace from Read, got %v", err)
	}
	// A symlink inside the world that points outside is refused too.
	sibling := filepath.Join(filepath.Dir(root), "outside-target.txt")
	if err := os.WriteFile(sibling, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(sibling, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := provider.Read(ctx, "escape"); !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("a symlink escape must be refused, got %v", err)
	}
}

// A conditional write reports a conflict instead of overwriting someone's edit.
func TestConditionalWriteDetectsAConflict(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "a.txt", Content: []byte("first")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	read, err := provider.Read(ctx, "a.txt")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Someone else changes the file after the read.
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "a.txt", Content: []byte("someone else")}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	at := read.Modified
	_, err = provider.Write(ctx, fs.WriteRequest{Path: "a.txt", Content: []byte("mine"), ExpectedModified: &at})
	if !errors.Is(err, fs.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	// The conflict must not have overwritten the other edit.
	after, err := provider.Read(ctx, "a.txt")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after.Content) != "someone else" {
		t.Fatalf("a conflicting write must not overwrite: %q", after.Content)
	}
	// Writing with the current time succeeds.
	current := after.Modified
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "a.txt", Content: []byte("mine"), ExpectedModified: &current}); err != nil {
		t.Fatalf("a matching expectation must succeed: %v", err)
	}
}

// A create refuses to replace an existing file, so "create" cannot silently
// become "replace".
func TestCreateRefusesAnExistingFile(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "a.txt", Content: []byte("first"), Create: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "a.txt", Content: []byte("second"), Create: true}); !errors.Is(err, fs.ErrConflict) {
		t.Fatalf("want ErrConflict for an existing file, got %v", err)
	}
	read, err := provider.Read(ctx, "a.txt")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(read.Content) != "first" {
		t.Fatalf("a refused create must not change the file: %q", read.Content)
	}
}

// A conditional write on a missing file reports not-found rather than creating it.
func TestConditionalWriteOnAMissingFile(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	at := readTime(t)
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "absent.txt", Content: []byte("x"), ExpectedModified: &at}); !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// Removal deletes a file and refuses a directory.
func TestRemoveRefusesADirectory(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "notes/a.txt", Content: []byte("x")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := provider.Remove(ctx, "notes"); err == nil {
		t.Fatal("removing a directory must be refused")
	}
	if err := provider.Remove(ctx, "notes/a.txt"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := provider.Read(ctx, "notes/a.txt"); !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("want ErrNotFound after removal, got %v", err)
	}
	if err := provider.Remove(ctx, "notes/a.txt"); !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("want ErrNotFound for a second removal, got %v", err)
	}
}

// The provider reports its own configuration and validates it.
func TestFSConfiguration(t *testing.T) {
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	provider, err := local.NewFS(local.FSConfig{World: world, Mode: fs.ModeReadOnly})
	if err != nil {
		t.Fatalf("fs: %v", err)
	}
	if provider.Mode() != fs.ModeReadOnly {
		t.Fatalf("unexpected mode %q", provider.Mode())
	}
	if provider.Workspace().ID() != "w-1" {
		t.Fatalf("unexpected workspace %q", provider.Workspace().ID())
	}
	if _, err := local.NewFS(local.FSConfig{Mode: fs.ModeReadOnly}); !errors.Is(err, local.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a workspace, got %v", err)
	}
	if _, err := local.NewFS(local.FSConfig{World: world}); !errors.Is(err, local.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a mode, got %v", err)
	}
}

// A cancelled context refuses the work rather than performing it.
func TestCancelledContextRefusesWork(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	seam := local.AsCtx(ctx)
	if _, err := provider.Read(seam, "a.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Read, got %v", err)
	}
	if _, err := provider.Write(seam, fs.WriteRequest{Path: "a.txt", Content: []byte("x")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Write, got %v", err)
	}
	if _, err := provider.Stat(seam, "a.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Stat, got %v", err)
	}
	if _, err := provider.List(seam, "."); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from List, got %v", err)
	}
	if err := provider.Remove(seam, "a.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Remove, got %v", err)
	}
}

// Reading a directory, or a missing path, reports the right condition.
func TestReadAndStatErrors(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "notes/a.txt", Content: []byte("x")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Read(ctx, "notes"); err == nil {
		t.Fatal("reading a directory must be refused")
	}
	if _, err := provider.Read(ctx, "absent.txt"); !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := provider.Stat(ctx, "absent.txt"); !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("want ErrNotFound from Stat, got %v", err)
	}
	if _, err := provider.List(ctx, "absent"); !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("want ErrNotFound from List, got %v", err)
	}
}

// readTime returns a fixed timestamp for conflict tests.
func readTime(t *testing.T) time.Time {
	t.Helper()
	return time.UnixMilli(1_700_000_000_000)
}

// A create inside a directory that does not exist yet makes the parents, so a
// first write into a new folder works without a separate step.
func TestWriteCreatesMissingParents(t *testing.T) {
	provider, root := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "a/b/c/deep.txt", Content: []byte("deep")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "b", "c", "deep.txt")); err != nil {
		t.Fatalf("the parents must exist: %v", err)
	}
}

// A list of a large directory is sorted, which is the property the insertion
// sort exists for; three entries already exercise the swap path.
func TestListSortsManyEntries(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	for _, name := range []string{"z.txt", "a.txt", "m.txt", "b.txt"} {
		if _, err := provider.Write(ctx, fs.WriteRequest{Path: name, Content: []byte("x")}); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	entries, err := provider.List(ctx, ".")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"a.txt", "b.txt", "m.txt", "z.txt"}
	if len(entries) != len(want) {
		t.Fatalf("want %d entries, got %d", len(want), len(entries))
	}
	for index, name := range want {
		if entries[index].Name != name {
			t.Fatalf("entry %d is %q, want %q", index, entries[index].Name, name)
		}
	}
	// Each entry reports its size and kind.
	if entries[0].IsDir || entries[0].Size != 1 {
		t.Fatalf("unexpected entry metadata %+v", entries[0])
	}
}

// Stat reports a directory as such, which is what lets a caller tell a file from
// a folder before reading it.
func TestStatReportsADirectory(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "notes/a.txt", Content: []byte("x")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	stat, err := provider.Stat(ctx, "notes")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !stat.IsDir {
		t.Fatalf("a directory must be reported as one: %+v", stat)
	}
	if stat.Mode == 0 {
		t.Fatal("permission bits must be reported")
	}
}

// Writing through a provider whose file became unreadable reports the failure
// rather than pretending the write happened.
func TestWriteReportsAnIOFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions do not deny writes")
	}
	provider, root := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	// A directory occupying the target path cannot be written as a file.
	if err := os.MkdirAll(filepath.Join(root, "blocked.txt"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "blocked.txt", Content: []byte("x")}); err == nil {
		t.Fatal("writing over a directory must report an error")
	}
	// A create over the same path is also refused, and as a conflict rather than
	// as a silent success.
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "blocked.txt", Content: []byte("x"), Create: true}); !errors.Is(err, fs.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}
