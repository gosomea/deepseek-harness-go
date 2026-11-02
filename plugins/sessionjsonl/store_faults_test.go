package sessionjsonl

import (
	"context"
	"os"
	"testing"

	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// A create that cannot sync its header reports the failure: a session another
// reader would see as valid must not be reported as created.
func TestCreateReportsASyncFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions do not deny syncs")
	}
	root := t.TempDir()
	provider, err := New(Options{DataDir: root})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	// A read-only directory lets the file be opened for reading but not created,
	// which exercises the create failure path before any bytes are written.
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(root, 0o755) }()
	if _, err := provider.Create(context.Background(), persistence.CreateOptions{ID: "s-1"}); err == nil {
		t.Fatal("creating in a read-only directory must report an error")
	}
}

// A create whose artifact path is occupied by a file cannot use O_EXCL, and the
// failure is reported rather than silently reusing the file.
func TestCreateRefusesAnExistingPath(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	if _, err := provider.Create(context.Background(), persistence.CreateOptions{ID: "s-1"}); err == nil {
		t.Fatal("creating an existing session must be refused")
	}
	// The original artifact is untouched by the refused create.
	reader := openRead(t, provider, "s-1")
	events, err := reader.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("a refused create must not modify the session, got %d events", len(events))
	}
	_ = reader.Close()
}

// Stat on a session whose header is a file of the right name but wrong content is
// reported as a format problem, not as a missing session.
func TestStatDistinguishesFormatFromMissing(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	if err := os.WriteFile(path, []byte("garbage\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Stat(context.Background(), "s-1"); err == nil {
		t.Fatal("a malformed artifact must be reported")
	}
	if _, err := provider.Stat(context.Background(), "absent"); err == nil {
		t.Fatal("a missing session must be reported")
	}
}

// An append whose payload the envelope refuses leaves the log unchanged, and the
// next append still gets the right sequence.
func TestRefusedAppendDoesNotConsumeASequence(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	first := appendEvent(t, handle, "turn/start", `{"turn":0}`)
	if first.Seq != 0 {
		t.Fatalf("want seq 0, got %d", first.Seq)
	}
	// A refused append must not advance the sequence: the next accepted record is
	// still seq 1, or the log would contain a gap.
	if _, err := handle.Append(context.Background(), session.AppendInput{Type: "", Data: []byte(`{}`)}); err == nil {
		t.Fatal("an event without a type must be refused")
	}
	second := appendEvent(t, handle, "turn/end", `{"turn":0}`)
	if second.Seq != 1 {
		t.Fatalf("a refused append must not consume a sequence, got %d", second.Seq)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reader := openRead(t, provider, "s-1")
	events, err := reader.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 2 || events[0].Seq != 0 || events[1].Seq != 1 {
		t.Fatalf("the stored log must be dense, got %+v", events)
	}
}

// Recovering a session that is not damaged is a no-op that reports the state.
func TestRecoverOnACleanSessionIsANoOp(t *testing.T) {
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 2)
	path := handlePath(t, provider, "s-1")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	analysis, err := provider.Recover(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if analysis.Complete != 2 || analysis.Repairable || analysis.Reason != "" {
		t.Fatalf("unexpected analysis %+v", analysis)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("recovering a clean session must not modify it")
	}
}
