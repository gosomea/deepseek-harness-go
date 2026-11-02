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

// The id encoding reports a malformed escape instead of producing a wrong id.
func TestDecodeIDRejectsBadEscapes(t *testing.T) {
	if _, err := DecodeID("a%2"); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("want ErrInvalidHeader for a truncated escape, got %v", err)
	}
	if _, err := DecodeID("a%zz"); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("want ErrInvalidHeader for a bad escape, got %v", err)
	}
	// A plain id round-trips unchanged.
	if got, err := DecodeID("plain-id_1"); err != nil || got != "plain-id_1" {
		t.Fatalf("plain id: %q %v", got, err)
	}
}

// A record whose payload field is missing is corrupt, not empty.
func TestDecodeRecordRejectsMissingPayload(t *testing.T) {
	if _, err := decodeRecord([]byte(`{"seq":0,"checksum":"00"}`)); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord, got %v", err)
	}
}

// Reading a log with a blank line in the middle fails through the handle, which
// is the path a reader actually takes.
func TestReadRejectsBlankMiddleRecord(t *testing.T) {
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 2)
	path := handlePath(t, provider, "s-1")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	mixed := []string{lines[0], lines[1], "", lines[2]}
	if err := os.WriteFile(path, []byte(strings.Join(mixed, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reader := openRead(t, provider, "s-1")
	if _, err := reader.Read(context.Background(), 0); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord from Read, got %v", err)
	}
}

// Reading a log whose record declares the wrong sequence fails through the handle.
func TestReadRejectsSequenceMismatch(t *testing.T) {
	provider := newProvider(t)
	writeSession(t, provider, "s-1", 2)
	path := handlePath(t, provider, "s-1")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	lines[2] = strings.Replace(lines[2], `"seq":1`, `"seq":5`, 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reader := openRead(t, provider, "s-1")
	if _, err := reader.Read(context.Background(), 0); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord from Read, got %v", err)
	}
}

// Reading a log whose event payload no longer decodes fails through the handle:
// a valid checksum is not enough if the event itself cannot be read.
func TestReadRejectsUnreadableEvent(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	// A record with a correct checksum over a payload that is not a session event.
	payload := []byte(`{"not":"an event"}`)
	line, err := EncodeRecord(0, payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	header, err := EncodeHeader("s-1")
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	path := handlePath(t, provider, "s-1")
	if err := os.WriteFile(path, append(append(append(header, '\n'), line...), '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reader := openRead(t, provider, "s-1")
	if _, err := reader.Read(context.Background(), 0); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord, got %v", err)
	}
	// Inspect reaches the same conclusion through its own decoder.
	analysis, err := provider.Inspect(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if analysis.Repairable || analysis.Reason == "" {
		t.Fatalf("an unreadable event must be refused: %+v", analysis)
	}
}

// An empty artifact has no header, so opening it is refused before a handle is
// handed out: a caller must not receive a handle it can only fail through.
func TestEmptyArtifactIsRefusedAtOpen(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("want ErrInvalidHeader from Open, got %v", err)
	}
	// The header is checked before a write handle claims the session, so a
	// refused open leaves no writer claim behind.
	if provider.service.WriterHeld("s-1") {
		t.Fatal("a refused open must not leave the writer claim held")
	}
}

// Inspect and Recover refuse a blank id, and Inspect reports a missing session.
func TestInspectAndRecoverValidations(t *testing.T) {
	provider := newProvider(t)
	if _, err := provider.Inspect(context.Background(), ""); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue from Inspect, got %v", err)
	}
	if _, err := provider.Inspect(context.Background(), "absent"); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound from Inspect, got %v", err)
	}
	if _, err := provider.Recover(context.Background(), "absent"); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound from Recover, got %v", err)
	}
}

// A lease path cannot be derived for a blank id, so the claim fails cleanly.
func TestClaimLeaseRejectsABlankID(t *testing.T) {
	root := t.TempDir()
	if _, err := claimLease(root, ""); err == nil {
		t.Fatal("a blank id must not produce a lease")
	}
	// An existing directory at the lease path is not an "already claimed" error
	// but a real failure, and it is reported as one.
	path, err := SessionPath(root, "s-1")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := os.MkdirAll(path+LeaseSuffix, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := claimLease(root, "s-1"); err == nil {
		t.Fatal("an unusable lease path must report an error")
	}
}

// ReleaseLease reports a failure that is not "already gone", and tolerates one
// that is.
func TestReleaseLeaseReportsRealFailures(t *testing.T) {
	root := t.TempDir()
	path, err := SessionPath(root, "s-1")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := releaseLease(path + LeaseSuffix); err != nil {
		t.Fatalf("releasing an absent lease must be a no-op: %v", err)
	}
	// A non-empty directory cannot be removed by Remove, which is a real failure.
	dir := path + LeaseSuffix
	if err := os.MkdirAll(filepath.Join(dir, "child"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := releaseLease(dir); err == nil {
		t.Fatal("a lease that cannot be removed must report the failure")
	}
}

// The log refuses a second read of an artifact whose header is missing, which is
// the shape a partially written creation would leave.
func TestLoadEventsRejectsAMissingHeader(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	line, err := EncodeRecord(0, []byte(`{"type":"turn/start","seq":0,"time":1,"data":{}}`))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(path, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); err == nil {
		t.Fatal("a record without a header must be refused at open")
	}
}

// A very long record is read in full rather than truncated by a small buffer.
func TestLongRecordRoundTrips(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	text := strings.Repeat("x", 200_000)
	appendEvent(t, handle, "step/start", `{"turn":0,"step":0,"note":"`+text+`"}`)
	if err := handle.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reader := openRead(t, provider, "s-1")
	events, err := reader.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 1 || len(events[0].Data) < len(text) {
		t.Fatalf("a long record must round-trip whole, got %d events", len(events))
	}
	if !strings.Contains(string(events[0].Data), text[:100]) {
		t.Fatal("the long payload lost its content")
	}
}

// A handle whose cached view was loaded from disk keeps serving reads from it.
func TestHandleCachesItsOwnView(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	appendEvent(t, handle, "turn/start", `{"turn":0}`)
	first, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	appendEvent(t, handle, "turn/end", `{"turn":0}`)
	second, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(first) != 1 || len(second) != 2 {
		t.Fatalf("the cached view must grow with appends: %d then %d", len(first), len(second))
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// A fresh read handle loads from disk instead of reusing the writer's cache.
	reader := openRead(t, provider, "s-1")
	events, err := reader.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 committed events, got %d", len(events))
	}
}

// An event that the envelope refuses is refused before it is stored, whatever the
// reason.
func TestEnvelopeRefusalIsPropagated(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	// A surface-eligible type without a surface op violates the envelope.
	if _, err := handle.Append(context.Background(), session.AppendInput{
		Type: "user/message",
		Data: []byte(`{"id":"m","role":"user","content":[{"type":"text","text":"x"}],"source":{"kind":"user"}}`),
	}); err == nil {
		t.Fatal("a surface-eligible event without a surface op must be refused")
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
