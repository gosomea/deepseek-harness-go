package sessionjsonl

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// A directory occupying the artifact path is a real filesystem state, and every
// operation must report it rather than treat it as an empty session.
func TestDirectoryAtTheArtifactPathIsReported(t *testing.T) {
	provider := newProvider(t)
	path := handlePath(t, provider, "s-1")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create sees the path as occupied and refuses.
	if _, err := provider.Create(context.Background(), persistence.CreateOptions{ID: "s-1"}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue when the session path is a directory, got %v", err)
	}
	// Stat and Open read a header from it and fail.
	if _, err := provider.Stat(context.Background(), "s-1"); err == nil {
		t.Fatal("stating a directory as a session must report an error")
	}
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); err == nil {
		t.Fatal("opening a directory as a session must report an error")
	}
	if _, err := provider.Inspect(context.Background(), "s-1"); err == nil {
		t.Fatal("inspecting a directory as a session must report an error")
	}
}

// An unreadable parent directory makes Stat fail with something other than
// "not found", which must be reported as an I/O failure rather than as a missing
// session.
func TestUnreadableParentIsNotReportedAsMissing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions do not deny access")
	}
	root := t.TempDir()
	provider, err := New(Options{DataDir: root})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	createSession(t, provider, "s-1")
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(root, 0o755) }()
	_, err = provider.Stat(context.Background(), "s-1")
	if errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("an unreadable directory must not be reported as a missing session: %v", err)
	}
	if err == nil {
		t.Fatal("an unreadable directory must report an error")
	}
	if _, err := provider.Create(context.Background(), persistence.CreateOptions{ID: "s-2"}); err == nil {
		t.Fatal("creating inside an unreadable directory must report an error")
	}
}

// A write that cannot reach the disk is reported by Flush, which is the call that
// promises durability.
//
// Append may legitimately succeed here: it promises only that the record entered
// this handle's buffer and is visible to its own reads, which is the weaker
// promise the contract draws. What must never happen is a *successful Flush*
// after the descriptor is gone, because that would report an unpersisted record
// as durable.
func TestFlushReportsUnwritableDescriptor(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a closed descriptor is still writable")
	}
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	jsonl := handle.(*jsonlHandle)
	if err := jsonl.file.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}
	// Append either buffers the record or reports the failure; both are within
	// the contract, so the result is not asserted.
	_, _ = handle.Append(context.Background(), session.AppendInput{
		Type: "turn/start", Data: []byte(`{"turn":0}`),
	})
	if err := handle.Flush(context.Background()); err == nil {
		t.Fatal("a flush that cannot persist must report the failure, not claim durability")
	}
	_ = handle.Close()
}

// A read through a handle whose file became unreadable reports the failure.
func TestReadReportsFailureAfterTheFileDisappears(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	appendEvent(t, handle, "turn/start", `{"turn":0}`)
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Remove the artifact after opening a read handle that has not yet loaded it.
	reader, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := os.Remove(handlePath(t, provider, "s-1")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := reader.Read(context.Background(), 0); err == nil {
		t.Fatal("reading a vanished artifact must report an error")
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// Recover on an artifact whose header declares a future version is refused.
func TestRecoverRefusesAFutureVersion(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	if err := os.WriteFile(path, []byte(`{"format":"`+FormatID+`","version":42,"id":"s-1"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Recover(context.Background(), "s-1"); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("want ErrUnsupportedVersion, got %v", err)
	}
}
