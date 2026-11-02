package persistence_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// userMessage builds a user message event payload.
func userMessage(t *testing.T, id string) []byte {
	t.Helper()
	message, err := llm.NewUserMessage("hello " + id)
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	message.ID = llm.MessageID(id)
	data, err := llm.MarshalMessage(message)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// A session whose last turn never closed is reported as having an open turn, and
// the tool call it left unsettled is listed rather than re-executed.
func TestResumeReportsUnsettledWork(t *testing.T) {
	store := persistence.NewMemoryStore()
	id := persistence.SessionID("s-1")
	store.Seed(id,
		session.Event{Type: "turn/start", Data: []byte(`{"turn":0,"input":"read a.txt"}`)},
		session.Event{Type: "user/message", Data: userMessage(t, "m1"), SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		session.Event{Type: "step/start", Data: []byte(`{"turn":0,"step":0}`)},
		session.Event{Type: "tool/call", Data: []byte(`{"turn":0,"step":0,"callId":"call-1","name":"read_file","arguments":"{}"}`)},
		// The process died here: the call was requested and never settled, and the
		// turn never closed.
	)
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: id, Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	state, err := persistence.Resume(context.Background(), handle)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !state.OpenTurn {
		t.Fatal("a turn that never closed must be reported")
	}
	if len(state.UnsettledCalls) != 1 || state.UnsettledCalls[0] != "call-1" {
		t.Fatalf("the unsettled call must be listed, got %v", state.UnsettledCalls)
	}
	if !state.NeedsAttention() {
		t.Fatal("unsettled work must require attention")
	}
	// Resume reports; it never executes. The stored history is unchanged.
	after, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(after) != 4 {
		t.Fatalf("resume must not append anything, got %d events", len(after))
	}
}

// A settled session resumes with nothing outstanding.
func TestResumeReportsCleanState(t *testing.T) {
	store := persistence.NewMemoryStore()
	id := persistence.SessionID("s-1")
	store.Seed(id,
		session.Event{Type: "turn/start", Data: []byte(`{"turn":0,"input":"x"}`)},
		session.Event{Type: "user/message", Data: userMessage(t, "m1"), SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		session.Event{Type: "step/start", Data: []byte(`{"turn":0,"step":0}`)},
		session.Event{Type: "tool/call", Data: []byte(`{"callId":"call-1"}`)},
		session.Event{Type: "tool/result", Data: []byte(`{"callId":"call-1"}`)},
		session.Event{Type: "step/end", Data: []byte(`{"turn":0,"step":0}`)},
		session.Event{Type: "turn/end", Data: []byte(`{"turn":0,"steps":1}`)},
	)
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: id, Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	state, err := persistence.Resume(context.Background(), handle)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if state.OpenTurn || len(state.UnsettledCalls) != 0 || state.NeedsAttention() {
		t.Fatalf("a settled session must resume clean, got %+v", state)
	}
	// The user message was claimed by the step, so nothing is pending.
	if state.PendingInputs != 0 {
		t.Fatalf("a claimed input must not be pending, got %d", state.PendingInputs)
	}
}

// Input recorded after the last step was never claimed, so it is reported as
// pending work rather than silently dropped.
func TestResumeReportsPendingInput(t *testing.T) {
	store := persistence.NewMemoryStore()
	id := persistence.SessionID("s-1")
	store.Seed(id,
		session.Event{Type: "turn/start", Data: []byte(`{"turn":0,"input":"x"}`)},
		session.Event{Type: "user/message", Data: userMessage(t, "m1"), SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		session.Event{Type: "step/start", Data: []byte(`{"turn":0,"step":0}`)},
		session.Event{Type: "step/end", Data: []byte(`{"turn":0,"step":0}`)},
		// Steering arrived after the step ended and before the turn closed.
		session.Event{Type: "user/message", Data: userMessage(t, "m2"), SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		session.Event{Type: "turn/end", Data: []byte(`{"turn":0,"steps":1}`)},
	)
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: id, Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	state, err := persistence.Resume(context.Background(), handle)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if state.PendingInputs != 1 {
		t.Fatalf("the unclaimed input must be reported, got %d", state.PendingInputs)
	}
	if state.OpenTurn {
		t.Fatal("the turn closed, so it must not be reported as open")
	}
}

// A checkpoint is durable when WriteCheckpoint returns, and readable afterwards.
func TestCheckpointRoundTrip(t *testing.T) {
	store := persistence.NewMemoryStore()
	id := persistence.SessionID("s-1")
	store.Seed(id, session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: id, Access: persistence.AccessWrite})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	checkpoint, err := persistence.WriteCheckpoint(context.Background(), handle, 1)
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if checkpoint.ID != id || !checkpoint.Complete || checkpoint.Seq != 2 {
		t.Fatalf("unexpected checkpoint %+v", checkpoint)
	}
	read, err := persistence.ReadCheckpoint(context.Background(), handle)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if read.Seq != checkpoint.Seq {
		t.Fatalf("checkpoint did not round-trip: %+v vs %+v", read, checkpoint)
	}
	// A log with no checkpoint says so rather than inventing one.
	empty := persistence.NewMemoryStore()
	empty.Seed("s-2", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	other, err := empty.Open(context.Background(), persistence.OpenOptions{ID: "s-2", Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := persistence.ReadCheckpoint(context.Background(), other); !errors.Is(err, persistence.ErrNoCheckpoint) {
		t.Fatalf("want ErrNoCheckpoint, got %v", err)
	}
}

// A checkpoint requires a writing handle and a handle at all.
func TestCheckpointRefusals(t *testing.T) {
	store := persistence.NewMemoryStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	reader, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := persistence.WriteCheckpoint(context.Background(), reader, 0); !errors.Is(err, persistence.ErrReadOnly) {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
	if _, err := persistence.WriteCheckpoint(context.Background(), nil, 0); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a nil handle, got %v", err)
	}
	if _, err := persistence.Resume(context.Background(), nil); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue from Resume, got %v", err)
	}
	if _, err := persistence.ReadCheckpoint(context.Background(), nil); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue from ReadCheckpoint, got %v", err)
	}
}

// Restore returns both the in-flight state and the derived history, so a caller
// cannot proceed on an empty view.
func TestRestoreReturnsStateAndHistory(t *testing.T) {
	store := persistence.NewMemoryStore()
	id := persistence.SessionID("s-1")
	assistant, err := llm.NewAssistantMessage("p", "m", llm.TextContent("answer"))
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	assistantData, err := llm.MarshalMessage(assistant)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	nested, err := persistence.NestMessage(assistantData)
	if err != nil {
		t.Fatalf("nest: %v", err)
	}
	store.Seed(id,
		session.Event{Type: "user/message", Data: userMessage(t, "m1"), SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		session.Event{Type: "assistant/message", Data: nested, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
	)
	cache := persistence.NewCache()
	state, messages, err := persistence.Restore(context.Background(), store, id, cache)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(state.Events) != 2 || len(messages) != 2 {
		t.Fatalf("unexpected restore result: %d events, %d messages", len(state.Events), len(messages))
	}
	if !cache.Valid() {
		t.Fatal("Restore must populate the cache")
	}
	if _, _, err := persistence.Restore(context.Background(), nil, id, cache); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a nil store, got %v", err)
	}
}

// A store that cannot open reports the failure rather than an empty session.
func TestRestorePropagatesStorageFailure(t *testing.T) {
	store := persistence.NewMemoryStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	// An unknown session is reported as missing while storage is healthy.
	if _, err := store.Open(context.Background(), persistence.OpenOptions{ID: "absent", Access: persistence.AccessRead}); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	boom := errors.New("storage unavailable")
	store.FailOpen(boom)
	if _, _, err := persistence.Restore(context.Background(), store, "s-1", nil); !errors.Is(err, boom) {
		t.Fatalf("want the storage failure, got %v", err)
	}
	// Stat is a different call, so it still works until its own failure is set.
	if _, err := store.Stat(context.Background(), "s-1"); err != nil {
		t.Fatalf("stat must still work: %v", err)
	}
	store.FailStat(boom)
	if _, err := store.Stat(context.Background(), "s-1"); !errors.Is(err, boom) {
		t.Fatalf("want the stat failure, got %v", err)
	}
	if _, err := store.Stat(context.Background(), ""); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank id, got %v", err)
	}
}

// The memory store enforces the same handle rules as any other backend.
func TestMemoryStoreHandleRules(t *testing.T) {
	store := persistence.NewMemoryStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	reader, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := reader.Append(context.Background(), session.AppendInput{Type: "turn/start", Data: []byte(`{}`)}); !errors.Is(err, persistence.ErrReadOnly) {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
	if err := reader.Flush(context.Background()); err != nil {
		t.Fatalf("a read handle may flush as a no-op: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := reader.Read(context.Background(), 0); !errors.Is(err, persistence.ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed, got %v", err)
	}
	writer, err := store.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessWrite})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := writer.Append(context.Background(), session.AppendInput{Type: "", Data: []byte(`{}`)}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a missing type, got %v", err)
	}
	if _, err := writer.Append(context.Background(), session.AppendInput{Type: "turn/end"}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for missing data, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := writer.Append(ctx, session.AppendInput{Type: "turn/end", Data: []byte(`{}`)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
