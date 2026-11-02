package sessionjsonl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// A cancelled context refuses every service operation before it touches disk.
func TestServiceOperationsHonourCancellation(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Create(ctx, persistence.CreateOptions{ID: "s-2"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Create, got %v", err)
	}
	if _, err := provider.Open(ctx, persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Open, got %v", err)
	}
	if _, err := provider.Stat(ctx, "s-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Stat, got %v", err)
	}
	if _, err := provider.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from List, got %v", err)
	}
	if _, err := provider.Inspect(ctx, "s-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Inspect, got %v", err)
	}
	if _, err := provider.Recover(ctx, "s-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Recover, got %v", err)
	}
	if _, err := provider.Stat(context.Background(), ""); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue from Stat, got %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// An existing writer lease is reported as a claim failure rather than as a
// generic I/O error, and the lease file is not left behind by the refused open.
func TestExistingLeaseRefusesWriterWithoutSideEffects(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	lease, err := claimLease(provider.DataDir(), "s-1")
	if err != nil {
		t.Fatalf("claim lease: %v", err)
	}
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessWrite}); err == nil {
		t.Fatal("a held lease must refuse a writer")
	}
	// The refused open must not remove the existing lease.
	if _, err := os.Stat(lease); err != nil {
		t.Fatalf("the existing lease must survive a refused open: %v", err)
	}
	if err := releaseLease(lease); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Releasing twice is a no-op.
	if err := releaseLease(lease); err != nil {
		t.Fatalf("second release must be a no-op, got %v", err)
	}
	handle := openWrite(t, provider, "s-1")
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// The writer claim is released when the append file cannot be opened, so a
// failed open does not lock the session out.
func TestFailedOpenReleasesTheWriterClaim(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	// Make the artifact unopenable for appending by removing it after the header
	// check would have passed: the open then fails on the append file.
	path := handlePath(t, provider, "s-1")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skipf("cannot make the artifact unreadable on this platform: %v", err)
	}
	defer func() { _ = os.Chmod(path, info.Mode()) }()
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessWrite}); err == nil {
		// Some platforms allow root to read anything; the claim must still be free
		// afterwards either way.
		t.Log("the platform allowed the open; checking the claim is not stuck")
	}
	if provider.service.WriterHeld("s-1") {
		t.Fatal("a failed open must not leave the writer claim held")
	}
}

// A provider whose data directory cannot be created reports the failure.
func TestNewRefusesAnUnusableDirectory(t *testing.T) {
	if _, err := New(Options{DataDir: "  "}); !errors.Is(err, ErrDataDirMissing) {
		t.Fatalf("want ErrDataDirMissing, got %v", err)
	}
	// A path whose parent is a regular file cannot be created.
	root := t.TempDir()
	file := filepath.Join(root, "blocker")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := New(Options{DataDir: filepath.Join(file, "nested")}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unusable directory, got %v", err)
	}
}

// A body that is not our format is refused by every reader.
func TestForeignArtifactIsRefusedEverywhere(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	if err := os.WriteFile(path, []byte(`{"format":"someone-else","version":1,"id":"s-1"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for name, call := range map[string]func() error{
		"Stat":    func() error { _, err := provider.Stat(context.Background(), "s-1"); return err },
		"List":    func() error { _, err := provider.List(context.Background()); return err },
		"Inspect": func() error { _, err := provider.Inspect(context.Background(), "s-1"); return err },
		"Open": func() error {
			_, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead})
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrForeignFormat) {
			t.Fatalf("%s must refuse a foreign artifact, got %v", name, err)
		}
	}
}

// A blank line in the middle is corruption; a blank line at the very end is a
// torn tail that recovery drops.
func TestBlankRecordIsCorruption(t *testing.T) {
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 2)
	path := handlePath(t, provider, "s-1")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	// Insert a blank line between the two records.
	mixed := []string{lines[0], lines[1], "", lines[2]}
	if err := os.WriteFile(path, []byte(strings.Join(mixed, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	analysis, err := provider.Inspect(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if analysis.Repairable || !strings.Contains(analysis.Reason, "blank") {
		t.Fatalf("a blank middle record must be refused: %+v", analysis)
	}
}

// An event whose payload is not valid JSON is refused before it reaches the file.
func TestInvalidPayloadIsRefused(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	if _, err := handle.Append(context.Background(), session.AppendInput{Type: "turn/start", Data: []byte(`{`)}); err == nil {
		t.Fatal("a non-JSON payload must be refused")
	}
	// The refused append left the log empty.
	events, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("a refused append must write nothing, got %d events", len(events))
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// A listing skips non-artifact files but reports one it cannot read.
func TestListIgnoresUnrelatedFiles(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	if err := os.WriteFile(filepath.Join(provider.DataDir(), "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(provider.DataDir(), "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stats, err := provider.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(stats) != 1 || stats[0].ID != "s-1" {
		t.Fatalf("only artifacts must be listed, got %+v", stats)
	}
	// A file with the artifact suffix but a broken body is reported, not skipped.
	broken := filepath.Join(provider.DataDir(), "broken"+LogSuffix)
	if err := os.WriteFile(broken, []byte("not a header\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.List(context.Background()); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("want ErrInvalidHeader for a broken artifact, got %v", err)
	}
}

// Stat reports a held writer and a torn tail, which is what a diagnostic reads.
func TestStatReportsWriterAndTail(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	appendEvent(t, handle, "turn/start", `{"turn":0}`)
	if err := handle.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	stat, err := provider.Stat(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !stat.WriterHeld || stat.Events != 1 || stat.Bytes == 0 {
		t.Fatalf("unexpected stat %+v", stat)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Add a torn tail and confirm Stat surfaces it.
	path := handlePath(t, provider, "s-1")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(path, append(body, []byte(`{"seq":1`)...), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	stat, err = provider.Stat(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !stat.RecoverableTail || stat.Events != 1 {
		t.Fatalf("a torn tail must be reported as recoverable: %+v", stat)
	}
}
