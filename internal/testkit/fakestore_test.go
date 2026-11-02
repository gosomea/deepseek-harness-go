package testkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// A seeded store returns the scripted events with dense sequences, so a fixture
// does not have to number them.
func TestFakeStoreSeedAndRead(t *testing.T) {
	store := testkit.NewFakeStore()
	store.Seed("s-1",
		session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)},
		session.Event{Type: "turn/end", Data: []byte(`{"turn":0}`)},
	)
	events := store.Events("s-1")
	if len(events) != 2 || events[0].Seq != 0 || events[1].Seq != 1 {
		t.Fatalf("seeding must number the events, got %+v", events)
	}
	// The returned slice is a copy: mutating it must not change the store.
	events[0].Type = "changed"
	if again := store.Events("s-1"); again[0].Type != "turn/start" {
		t.Fatal("Events must return a copy")
	}
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	read, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("want 2 events, got %d", len(read))
	}
	// A read from an offset returns the suffix, and one past the end is empty.
	tail, err := handle.Read(context.Background(), 1)
	if err != nil || len(tail) != 1 {
		t.Fatalf("suffix read: events=%d err=%v", len(tail), err)
	}
	past, err := handle.Read(context.Background(), 9)
	if err != nil || len(past) != 0 {
		t.Fatalf("past-end read: events=%d err=%v", len(past), err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// Every scripted failure is returned by the call it belongs to, and only that
// call: a fake that failed everything would not let a test tell the paths apart.
func TestFakeStoreScriptedFailures(t *testing.T) {
	store := testkit.NewFakeStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	boom := errors.New("scripted failure")

	store.FailStat(boom)
	if _, err := store.Stat(context.Background(), "s-1"); !errors.Is(err, boom) {
		t.Fatalf("want the stat failure, got %v", err)
	}
	// Open is a different call, so it still works.
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessWrite})
	if err != nil {
		t.Fatalf("open must still work: %v", err)
	}

	store.FailAppend(boom)
	if _, err := handle.Append(context.Background(), session.AppendInput{Type: "turn/end", Data: []byte(`{}`)}); !errors.Is(err, boom) {
		t.Fatalf("want the append failure, got %v", err)
	}
	// A failed append wrote nothing.
	if events := store.Events("s-1"); len(events) != 1 {
		t.Fatalf("a failed append must write nothing, got %d events", len(events))
	}

	store.FailFlush(boom)
	if err := handle.Flush(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("want the flush failure, got %v", err)
	}

	// Clearing the append failure lets a real append through.
	store.FailAppend(nil)
	if _, err := handle.Append(context.Background(), session.AppendInput{
		Type: "turn/end", Data: []byte(`{"turn":0}`),
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if events := store.Events("s-1"); len(events) != 2 {
		t.Fatalf("want 2 events after a successful append, got %d", len(events))
	}
	_ = handle.Close()

	store.FailOpen(boom)
	if _, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); !errors.Is(err, boom) {
		t.Fatalf("want the open failure, got %v", err)
	}
}

// The opened/closed counters are what let a test assert a caller released its
// handles.
func TestFakeStoreCountsHandles(t *testing.T) {
	store := testkit.NewFakeStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	if opened, closed := store.Opened(); opened != 0 || closed != 0 {
		t.Fatalf("a fresh store has no handles, got %d/%d", opened, closed)
	}
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened, _ := store.Opened(); opened != 1 {
		t.Fatalf("want one open, got %d", opened)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
	opened, closed := store.Opened()
	if opened != 1 || closed != 1 {
		t.Fatalf("a double close must count once, got %d/%d", opened, closed)
	}
}

// The fake enforces the same handle rules as a real backend, so a test that
// passes against it is testing the contract rather than the fake.
func TestFakeStoreHandleRules(t *testing.T) {
	store := testkit.NewFakeStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	reader, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := reader.Append(context.Background(), session.AppendInput{Type: "turn/end", Data: []byte(`{}`)}); !errors.Is(err, persistence.ErrReadOnly) {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := reader.Read(context.Background(), 0); !errors.Is(err, persistence.ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed, got %v", err)
	}
	if err := reader.Flush(context.Background()); !errors.Is(err, persistence.ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed from Flush, got %v", err)
	}

	writer, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessWrite})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := writer.Append(context.Background(), session.AppendInput{Type: "", Data: []byte(`{}`)}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a type, got %v", err)
	}
	if _, err := writer.Append(context.Background(), session.AppendInput{Type: "turn/end"}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without data, got %v", err)
	}
	// A surface-eligible event without a surface op is refused by the envelope.
	if _, err := writer.Append(context.Background(), session.AppendInput{
		Type: "user/message", Data: []byte(`{"id":"m"}`),
	}); err == nil {
		t.Fatal("the log's envelope rule must still apply")
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := writer.Append(ctx, session.AppendInput{Type: "turn/end", Data: []byte(`{}`)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if _, err := writer.Read(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Read, got %v", err)
	}
	_ = writer.Close()
}

// The store's own validations match a real backend's.
func TestFakeStoreValidations(t *testing.T) {
	store := testkit.NewFakeStore()
	if _, err := store.Stat(context.Background(), ""); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank id, got %v", err)
	}
	if _, err := store.Stat(context.Background(), "absent"); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	if _, err := store.Open(context.Background(), persistence.OpenOptions{ID: "absent", Access: persistence.AccessRead}); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound from Open, got %v", err)
	}
	if _, err := store.Open(context.Background(), persistence.OpenOptions{}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an id, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Stat(ctx, "s-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Stat, got %v", err)
	}
	if _, err := store.Open(ctx, persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Open, got %v", err)
	}
	stat, err := store.Stat(context.Background(), "s-1")
	if err == nil {
		t.Fatalf("an unseeded session must be missing, got %+v", stat)
	}
}

// assertNestedMessage checks that a payload carries its message under "message",
// which is the shape the four nested event types must use.
func assertNestedMessage(t *testing.T, payload []byte) {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("the payload must be a JSON object: %v", err)
	}
	inner, ok := envelope["message"]
	if !ok {
		t.Fatalf("the nested form must carry \"message\": %s", payload)
	}
	if _, err := llm.UnmarshalMessage(inner); err != nil {
		t.Fatalf("the nested message must decode: %v", err)
	}
}

// The message builders produce payloads the log accepts, which is what makes them
// usable in a fixture.
func TestFakeStoreMessageBuilders(t *testing.T) {
	user, err := testkit.UserMessage("m1", "hello")
	if err != nil {
		t.Fatalf("user message: %v", err)
	}
	decoded, err := llm.UnmarshalMessage(user)
	if err != nil {
		t.Fatalf("the payload must decode: %v", err)
	}
	if decoded.ID != "m1" || llm.ContentText(decoded.Content) != "hello" {
		t.Fatalf("unexpected decoded message %+v", decoded)
	}

	nested, err := testkit.NestedMessage(user)
	if err != nil {
		t.Fatalf("nest: %v", err)
	}
	// The nested form is what the surface reader decodes, so the assertion is on
	// the shape itself rather than on a helper that would hide it.
	assertNestedMessage(t, nested)

	assistant, err := testkit.AssistantMessage("a1", "fake", "fake-1", "answer")
	if err != nil {
		t.Fatalf("assistant message: %v", err)
	}
	assertNestedMessage(t, assistant)
	if _, err := testkit.NestedMessage(nil); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an empty message, got %v", err)
	}
	if _, err := testkit.NestedMessage([]byte(`{`)); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for broken JSON, got %v", err)
	}
}

// A seeded surface event keeps its metadata through the store, which is what lets
// a restore fold to the same history the writer recorded.
func TestFakeStorePreservesSurfaceMetadata(t *testing.T) {
	store := testkit.NewFakeStore()
	payload, err := testkit.UserMessage("m1", "hello")
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	store.Seed("s-1", session.Event{
		Type: "user/message", Data: payload, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	})
	events := store.Events("s-1")
	if len(events) != 1 || events[0].SurfaceOp == nil || !events[0].SurfaceOp.IsAppend() {
		t.Fatalf("the surface metadata must survive seeding: %+v", events)
	}
	// Mutating the returned metadata must not change the store's copy.
	events[0].SurfaceOp = nil
	if again := store.Events("s-1"); again[0].SurfaceOp == nil {
		t.Fatal("the surface metadata must be copied, not shared")
	}
}

// An append carrying a surface op stores it, and the refused forms leave the log
// alone.
func TestFakeStoreAppendWithSurface(t *testing.T) {
	store := testkit.NewFakeStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessWrite})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	payload, err := testkit.UserMessage("m1", "hello")
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	event, err := handle.Append(context.Background(), session.AppendInput{
		Type: "user/message", Data: payload, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if event.SurfaceOp == nil || !event.SurfaceOp.IsAppend() {
		t.Fatalf("the surface op must be recorded: %+v", event)
	}
	if event.Seq != 1 {
		t.Fatalf("the appended event must follow the seeded one, got %d", event.Seq)
	}
}
