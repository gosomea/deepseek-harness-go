package sessionjsonl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosomea/deepseek-harness-go/persistence"
)

// A create that cannot write the artifact reports the failure instead of leaving
// an unusable session behind.
func TestCreateReportsWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not deny writes")
	}
	root := t.TempDir()
	provider, err := New(Options{DataDir: root})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	// Make the directory unwritable so the artifact cannot be created.
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(root, 0o755) }()
	if _, err := provider.Create(context.Background(), persistence.CreateOptions{ID: "s-1"}); err == nil {
		t.Fatal("a create that cannot write must report an error")
	}
	// No artifact may be left behind by the failed create.
	_ = os.Chmod(root, 0o755)
	if _, err := provider.Stat(context.Background(), "s-1"); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("a failed create must leave no session, got %v", err)
	}
}

// A close that cannot flush reports the failure rather than pretending the data
// is safe.
func TestCloseReportsFlushFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a closed descriptor is still writable")
	}
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	appendEvent(t, handle, "turn/start", `{"turn":0}`)
	// Close the underlying descriptor out from under the buffered writer, so the
	// flush cannot succeed.
	jsonl, ok := handle.(*jsonlHandle)
	if !ok {
		t.Fatalf("unexpected handle type %T", handle)
	}
	if err := jsonl.file.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}
	if err := handle.Close(); err == nil {
		t.Fatal("a close that cannot flush must report the failure")
	}
	// The writer claim is released even though the close failed, so the session
	// is not locked out forever.
	if provider.service.WriterHeld("s-1") {
		t.Fatal("a failed close must still release the writer claim")
	}
}

// An artifact that cannot be read reports the failure rather than an empty log.
func TestReadReportsOpenFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions do not deny reads")
	}
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(path, 0o644) }()
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); err == nil {
		t.Fatal("opening an unreadable artifact must report an error")
	}
	if _, err := provider.Stat(context.Background(), "s-1"); err == nil {
		t.Fatal("stating an unreadable artifact must report an error")
	}
}

// A recovery that cannot write the repaired artifact leaves the original intact
// and reports the failure.
func TestRecoverReportsWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not deny writes")
	}
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 2)
	path := handlePath(t, provider, "s-1")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(path, append(body, []byte(`{"seq":2`)...), 0o644); err != nil {
		t.Fatalf("write torn: %v", err)
	}
	// Repair truncates the artifact in place, so it needs write permission on the
	// file rather than on the directory. Making the file read-only is therefore the
	// condition that makes the repair fail.
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(path, 0o644) }()
	if _, err := provider.Recover(context.Background(), "s-1"); err == nil {
		t.Fatal("a repair that cannot write must report an error")
	}
	_ = os.Chmod(path, 0o644)
	// The original artifact is still there, still with its torn tail.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(after) != len(body)+len(`{"seq":2`) {
		t.Fatalf("a failed repair must not modify the artifact: %d bytes", len(after))
	}
}

// Listing a directory that cannot be read reports the failure.
func TestListReportsReadDirFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions do not deny reads")
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
	if _, err := provider.List(context.Background()); err == nil {
		t.Fatal("listing an unreadable directory must report an error")
	}
}

// A header-only artifact is a valid empty session, and a truncated header is not.
func TestHeaderOnlyAndTruncatedHeader(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	// Header only: readable, zero events.
	reader := openRead(t, provider, "s-1")
	events, err := reader.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("a fresh session has no events, got %d", len(events))
	}
	_ = reader.Close()
	// A truncated header line is a header problem, not a torn record.
	if err := os.WriteFile(path, []byte(`{"format":"`+FormatID+`"`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Inspect(context.Background(), "s-1"); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("want ErrInvalidHeader, got %v", err)
	}
}

// The session id encoding is reversible, so a listing shows the real identity.
func TestEncodedIDIsRecoverableFromTheListing(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "id with spaces:and/slash")
	stats, err := provider.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(stats) != 1 || stats[0].ID != "id with spaces:and/slash" {
		t.Fatalf("the listing must show the original id, got %+v", stats)
	}
	// The artifact file itself must not contain a separator.
	entries, err := os.ReadDir(provider.DataDir())
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if filepath.Base(entry.Name()) != entry.Name() {
			t.Fatalf("artifact name must be a single path element: %s", entry.Name())
		}
	}
}
