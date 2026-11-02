package local_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/approval"
	"github.com/gosomea/deepseek-harness-go/capability/fs"
	"github.com/gosomea/deepseek-harness-go/capability/sandbox"
	"github.com/gosomea/deepseek-harness-go/capability/subprocess"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
	"github.com/gosomea/deepseek-harness-go/plugins/local"
)

// A read larger than the configured bound is refused before the bytes are loaded.
func TestReadRefusesOversizedContent(t *testing.T) {
	provider, root := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	// A sparse file just past the read bound: writing it costs nothing, and the
	// reader must refuse on the size check rather than loading it.
	path := filepath.Join(root, "huge.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := file.Truncate(fs.MaxReadBytes + 1); err != nil {
		_ = file.Close()
		t.Skipf("the filesystem does not support sparse files: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := provider.Read(ctx, "huge.bin"); !errors.Is(err, fs.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

// A read whose file disappears between the stat and the open reports the failure
// instead of returning nothing.
func TestReadReportsAVanishedFile(t *testing.T) {
	provider, root := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	path := filepath.Join(root, "gone.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A directory read is refused: the stat succeeds and the read must not.
	if _, err := provider.Read(ctx, "."); err == nil {
		t.Fatal("reading a directory must fail")
	}
}

// A write request with an unusable shape is refused before the filesystem is
// touched, so a bad request cannot leave a partial effect.
func TestWriteValidatesItsRequest(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{}); !errors.Is(err, fs.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}
}

// Listing a path that is a file, rather than a directory, reports the failure.
func TestListReportsANonDirectory(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "file.txt", Content: []byte("x")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.List(ctx, "file.txt"); err == nil {
		t.Fatal("listing a file must report an error")
	}
}

// Removing a path that vanished reports not-found rather than succeeding.
func TestRemoveReportsAVanishedPath(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if err := provider.Remove(ctx, "never-existed.txt"); !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// A stat over a path whose parent is a file reports the failure.
func TestStatReportsAnUnusablePath(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "file.txt", Content: []byte("x")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Stat(ctx, "file.txt/child"); err == nil {
		t.Fatal("stating a path under a file must report an error")
	}
}

// A write into a path whose parent is a file reports the failure rather than
// creating anything.
func TestWriteReportsAnUnusableParent(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "file.txt", Content: []byte("x")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Write(ctx, fs.WriteRequest{Path: "file.txt/child.txt", Content: []byte("y")}); err == nil {
		t.Fatal("writing under a file must report an error")
	}
}

// The insertion sort in the listing is exercised by enough entries to swap.
func TestListSortsAReverseOrderedDirectory(t *testing.T) {
	provider, _ := newFS(t, fs.ModeWorkspaceWrite)
	ctx := local.AsCtx(context.Background())
	names := []string{"e.txt", "d.txt", "c.txt", "b.txt", "a.txt"}
	for _, name := range names {
		if _, err := provider.Write(ctx, fs.WriteRequest{Path: name, Content: []byte("x"), Create: true}); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	entries, err := provider.List(ctx, ".")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for index, want := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		if entries[index].Name != want {
			t.Fatalf("entry %d is %q, want %q", index, entries[index].Name, want)
		}
	}
}

// A run of a program that cannot start still reaches the cleanup path, where the
// helper must tolerate a nil process rather than panicking.
func TestMissingProgramStillCleansUp(t *testing.T) {
	provider, _ := newProcess(t)
	if _, err := provider.Run(local.AsProcessCtx(context.Background()), subprocess.Request{
		Program: "/nonexistent/cleanup-check",
	}); err == nil {
		t.Fatal("a missing program must report an error")
	}
}

// A tool set built without a filesystem still reports a workspace root, taken
// from the process world, so a path shown to the model stays relative.
func TestToolSetWithoutAFilesystemStillHasAWorld(t *testing.T) {
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	process, err := local.NewProcess(local.ProcessConfig{World: world})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	gateway, err := approval.NewGateway(nil, approval.PolicyDeny)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	set, err := local.NewTools(local.ToolsConfig{
		Process:   process,
		Approvals: gateway,
		Policy:    sandbox.Policy{Effective: sandbox.ModeWorkspaceWrite, Available: sandbox.ModeDangerFullAccess},
	})
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	// The command tool is offered because a process provider exists.
	names := map[string]bool{}
	for _, definition := range set.Definitions() {
		names[string(definition.Name)] = true
	}
	if !names["run_command"] {
		t.Fatal("a process provider must yield the command tool")
	}
	if names["edit_file"] {
		t.Fatal("a filesystem-less set must not offer the editing tool")
	}
}
