package sessionjsonl

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// writeSession creates a session with the given number of committed events.
func writeSession(t *testing.T, provider *Provider, id persistence.SessionID, count int) {
	t.Helper()
	createSession(t, provider, id)
	handle := openWrite(t, provider, id)
	for index := 0; index < count; index++ {
		appendEvent(t, handle, "step/start", `{"turn":0,"step":`+string(rune('0'+index))+`}`)
	}
	if err := handle.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// A torn final record is reported as repairable and repaired without touching the
// committed prefix.
func TestTornTailIsRepairable(t *testing.T) {
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 3)
	path := handlePath(t, provider, "s-1")

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Simulate a writer interrupted mid-record: append an incomplete line.
	torn := append(append([]byte(nil), original...), []byte(`{"seq":3,"payl`)...)
	if err := os.WriteFile(path, torn, 0o644); err != nil {
		t.Fatalf("write torn: %v", err)
	}

	analysis, err := provider.Inspect(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if analysis.Complete != 3 || !analysis.Repairable || analysis.TornTailBytes == 0 {
		t.Fatalf("unexpected analysis %+v", analysis)
	}

	repaired, err := provider.Recover(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	// After a successful repair there is nothing left to repair, so Repairable is
	// false and the committed events are all still present.
	if repaired.TornTailBytes != 0 || repaired.Repairable || repaired.Complete != 3 {
		t.Fatalf("unexpected repaired analysis %+v", repaired)
	}
	// The committed prefix is byte-for-byte identical: repairing drops bytes, it
	// never rewrites what was already stored.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != string(original) {
		t.Fatalf("repair changed the committed prefix:\n%q\n%q", after, original)
	}
	// The repaired session opens and reads its three events.
	reader := openRead(t, provider, "s-1")
	events, err := reader.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("want 3 events after repair, got %d", len(events))
	}
}

// Damage in the middle is refused rather than silently shortened.
func TestMiddleCorruptionIsRefused(t *testing.T) {
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 3)
	path := handlePath(t, provider, "s-1")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header plus 3 records, got %d lines", len(lines))
	}
	// Damage the checksum of the middle record, leaving the rest intact.
	lines[2] = strings.Replace(lines[2], `"checksum":"`, `"checksum":"00`, 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	analysis, err := provider.Inspect(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if analysis.Repairable || analysis.Reason == "" {
		t.Fatalf("middle damage must not be repairable: %+v", analysis)
	}
	if _, err := provider.Recover(context.Background(), "s-1"); !errors.Is(err, persistence.ErrCorruptReport) {
		t.Fatalf("want ErrCorruptReport, got %v", err)
	}
	// The damaged artifact is left exactly as it was found.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(after), `"checksum":"00`) {
		t.Fatal("a refused repair must not modify the artifact")
	}

	// Reading it through a handle fails rather than returning a short history.
	reader := openRead(t, provider, "s-1")
	if _, err := reader.Read(context.Background(), 0); err == nil {
		t.Fatal("a corrupt log must not read as a shorter one")
	}
}

// A sequence gap is corruption, not a repairable tail.
func TestSequenceGapIsRefused(t *testing.T) {
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 2)
	path := handlePath(t, provider, "s-1")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	lines[2] = strings.Replace(lines[2], `"seq":1`, `"seq":7`, 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	analysis, err := provider.Inspect(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if analysis.Repairable || !strings.Contains(analysis.Reason, "seq") {
		t.Fatalf("a sequence gap must be refused: %+v", analysis)
	}
}

// A future version is refused with its own error, distinct from a foreign file.
func TestFutureVersionIsRefused(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	if err := os.WriteFile(path, []byte(`{"format":"`+FormatID+`","version":99,"id":"s-1"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("want ErrUnsupportedVersion, got %v", err)
	}
	if _, err := provider.Stat(context.Background(), "s-1"); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("want ErrUnsupportedVersion from Stat, got %v", err)
	}
	if _, err := provider.List(context.Background()); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("want ErrUnsupportedVersion from List, got %v", err)
	}
	// A foreign artifact is distinguished from a newer one of ours.
	if err := os.WriteFile(path, []byte(`{"format":"dsh-session-v4","version":1,"id":"s-1"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); !errors.Is(err, ErrForeignFormat) {
		t.Fatalf("want ErrForeignFormat, got %v", err)
	}
}

// Recovery refuses to run while a writer holds the session: repairing under a
// live writer would race its appends.
func TestRecoverRefusesWhileAWriterHoldsTheSession(t *testing.T) {
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 1)
	handle := openWrite(t, provider, "s-1")
	if _, err := provider.Recover(context.Background(), "s-1"); !errors.Is(err, persistence.ErrWriterExists) {
		t.Fatalf("want ErrWriterExists, got %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// With the writer gone, recovery on a clean artifact is a no-op.
	analysis, err := provider.Recover(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if analysis.TornTailBytes != 0 || analysis.Complete != 1 {
		t.Fatalf("unexpected analysis %+v", analysis)
	}
	if _, err := provider.Recover(context.Background(), ""); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank id, got %v", err)
	}
	if _, err := provider.Inspect(context.Background(), "absent"); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

// A completely empty artifact is refused: a file with no header says nothing.
func TestEmptyArtifactIsRefused(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Inspect(context.Background(), "s-1"); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("want ErrInvalidHeader, got %v", err)
	}
}

// The event stored is the one the log rule expects: the sequence is the current
// length, so a gap cannot be produced by the writer.
func TestStoredEventMatchesTheLogRule(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	first := appendEvent(t, handle, "turn/start", `{"turn":0}`)
	if first.Seq != 0 || first.Time == 0 {
		t.Fatalf("unexpected stored event %+v", first)
	}
	if err := session.ValidateEventEnvelope(first); err != nil {
		t.Fatalf("a stored event must satisfy the envelope rule: %v", err)
	}
	// An event the envelope refuses is refused before it is written.
	if _, err := handle.Append(context.Background(), session.AppendInput{
		Type: "turn/start", Data: []byte(`{`),
	}); err == nil {
		t.Fatal("an event with a non-JSON payload must be refused")
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// The exported inspector reads any artifact path, so a salvage copy can be
// examined without being placed where a provider would look for a session.
func TestInspectArtifactAtReadsAnArbitraryPath(t *testing.T) {
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 2)
	body, err := os.ReadFile(handlePath(t, provider, "s-1"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	copyPath := t.TempDir() + "/salvaged.jsonl"
	if err := os.WriteFile(copyPath, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	analysis, err := InspectArtifactAt(copyPath)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if analysis.Complete != 2 || analysis.ID != "s-1" || analysis.Repairable {
		t.Fatalf("unexpected analysis %+v", analysis)
	}
	// A path that does not exist is reported, not treated as empty.
	if _, err := InspectArtifactAt(t.TempDir() + "/absent.jsonl"); err == nil {
		t.Fatal("a missing artifact must be reported")
	}
}
