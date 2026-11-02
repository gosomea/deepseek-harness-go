package persistence

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
)

// A seeded memory store returns dense sequences and copies on the way out, so a
// test cannot mutate stored content through a returned slice.
func TestMemoryStoreSeedAndRead(t *testing.T) {
	store := NewMemoryStore()
	store.Seed("s-1",
		session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)},
		session.Event{Type: "turn/end", Data: []byte(`{"turn":0}`)},
	)
	handle, err := store.Open(context.Background(), OpenOptions{ID: "s-1", Access: AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	events, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 2 || events[0].Seq != 0 || events[1].Seq != 1 {
		t.Fatalf("seeding must number events densely, got %+v", events)
	}
	// The returned slice is independent of the store.
	events[0].Type = "changed"
	again, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if again[0].Type != "turn/start" {
		t.Fatal("Read must return a copy")
	}
	// Reading from an offset returns the suffix; past the end is empty.
	tail, err := handle.Read(context.Background(), 1)
	if err != nil || len(tail) != 1 {
		t.Fatalf("suffix read: events=%d err=%v", len(tail), err)
	}
	past, err := handle.Read(context.Background(), 5)
	if err != nil || len(past) != 0 {
		t.Fatalf("past-end read: events=%d err=%v", len(past), err)
	}
}

// An append through the memory store enforces the same envelope rules as a real
// backend, so a caller cannot use the fake to bypass the log's contract.
func TestMemoryStoreAppendEnforcesTheEnvelope(t *testing.T) {
	store := NewMemoryStore()
	store.Seed("s-1")
	handle, err := store.Open(context.Background(), OpenOptions{ID: "s-1", Access: AccessWrite})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	event, err := handle.Append(context.Background(), session.AppendInput{
		Type: "turn/start", Data: []byte(`{"turn":0}`),
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if event.Seq != 0 {
		t.Fatalf("want seq 0, got %d", event.Seq)
	}
	// A missing type or payload is refused.
	if _, err := handle.Append(context.Background(), session.AppendInput{Data: []byte(`{}`)}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a type, got %v", err)
	}
	if _, err := handle.Append(context.Background(), session.AppendInput{Type: "turn/end"}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without data, got %v", err)
	}
	// A surface-eligible event without its op is refused by the log's rule.
	if _, err := handle.Append(context.Background(), session.AppendInput{
		Type: "user/message", Data: []byte(`{"id":"m"}`),
	}); err == nil {
		t.Fatal("the envelope rule must still apply")
	}
	// A failed append wrote nothing, so the next sequence is still 1.
	next, err := handle.Append(context.Background(), session.AppendInput{
		Type: "turn/end", Data: []byte(`{"turn":0}`),
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if next.Seq != 1 {
		t.Fatalf("a refused append must not consume a sequence, got %d", next.Seq)
	}
}

// A surface op supplied through the memory store is stored, so a restore folds
// the same history the writer recorded.
func TestMemoryStoreStoresSurfaceMetadata(t *testing.T) {
	store := NewMemoryStore()
	store.Seed("s-1")
	handle, err := store.Open(context.Background(), OpenOptions{ID: "s-1", Access: AccessWrite})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	payload, err := buildUserPayload(t, "m1", "hello")
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	if _, err := handle.Append(context.Background(), session.AppendInput{
		Type: "user/message", Data: payload, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	events, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 1 || events[0].SurfaceOp == nil || !events[0].SurfaceOp.IsAppend() {
		t.Fatalf("the surface op must survive, got %+v", events)
	}
	// Mutating the read event must not change what is stored.
	events[0].SurfaceOp = nil
	again, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if again[0].SurfaceOp == nil {
		t.Fatal("surface metadata must be copied")
	}
}

// Stat reports the recorded event count and distinguishes a missing session from
// an invalid request.
func TestMemoryStoreStat(t *testing.T) {
	store := NewMemoryStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{}`)})
	stat, err := store.Stat(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if stat.ID != "s-1" || stat.Events != 1 || stat.FormatVersion == 0 {
		t.Fatalf("unexpected stat %+v", stat)
	}
	if _, err := store.Stat(context.Background(), "absent"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	if _, err := store.Stat(context.Background(), ""); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Stat(ctx, "s-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if _, err := store.Open(ctx, OpenOptions{ID: "s-1", Access: AccessRead}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from Open, got %v", err)
	}
}

// The memory store enforces the closed and read-only rules like any backend.
func TestMemoryStoreHandleRules(t *testing.T) {
	store := NewMemoryStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{}`)})
	reader, err := store.Open(context.Background(), OpenOptions{ID: "s-1", Access: AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := reader.Append(context.Background(), session.AppendInput{Type: "turn/end", Data: []byte(`{}`)}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
	if err := reader.Flush(context.Background()); err != nil {
		t.Fatalf("a read handle may flush as a no-op: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
	if _, err := reader.Read(context.Background(), 0); !errors.Is(err, ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed, got %v", err)
	}
	if err := reader.Flush(context.Background()); !errors.Is(err, ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed from Flush, got %v", err)
	}
	if _, err := store.Open(context.Background(), OpenOptions{}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an id, got %v", err)
	}
	if _, err := store.Open(context.Background(), OpenOptions{ID: "absent", Access: AccessRead}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

// Concurrent appends through separate handles stay dense, which is what makes the
// memory store usable as a fake in a race test.
func TestMemoryStoreConcurrentAppends(t *testing.T) {
	store := NewMemoryStore()
	store.Seed("s-1")
	const writers = 4
	const perWriter = 5
	var group sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < writers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			handle, err := store.Open(context.Background(), OpenOptions{ID: "s-1", Access: AccessWrite})
			if err != nil {
				t.Errorf("open: %v", err)
				return
			}
			defer func() { _ = handle.Close() }()
			<-start
			for count := 0; count < perWriter; count++ {
				if _, err := handle.Append(context.Background(), session.AppendInput{
					Type: "step/start", Data: []byte(`{"turn":0,"step":0}`),
				}); err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}()
	}
	close(start)
	group.Wait()
	stat, err := store.Stat(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if stat.Events != writers*perWriter {
		t.Fatalf("want %d events, got %d", writers*perWriter, stat.Events)
	}
}

// The injection seams report the scripted failure and only for their own call.
func TestMemoryStoreScriptedFailures(t *testing.T) {
	store := NewMemoryStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{}`)})
	boom := errors.New("scripted failure")
	store.FailOpen(boom)
	if _, err := store.Open(context.Background(), OpenOptions{ID: "s-1", Access: AccessRead}); !errors.Is(err, boom) {
		t.Fatalf("want the open failure, got %v", err)
	}
	// Stat is a different call, so it still works.
	if _, err := store.Stat(context.Background(), "s-1"); err != nil {
		t.Fatalf("stat must still work: %v", err)
	}
	store.FailStat(boom)
	if _, err := store.Stat(context.Background(), "s-1"); !errors.Is(err, boom) {
		t.Fatalf("want the stat failure, got %v", err)
	}
}

// buildUserPayload renders a user message payload the log accepts.
//
// A surface-eligible event must carry a message the fold can read, so a test
// cannot use an arbitrary JSON object for it.
func buildUserPayload(t *testing.T, id, text string) ([]byte, error) {
	t.Helper()
	message, err := llm.NewUserMessage(text)
	if err != nil {
		return nil, err
	}
	message.ID = llm.MessageID(id)
	return llm.MarshalMessage(message)
}
