package persistence_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// A discarded cache rebuilds to the same answer, which is what makes it a cache
// rather than a second source of truth.
func TestCacheRebuildsIdenticallyAfterDiscard(t *testing.T) {
	store := persistence.NewMemoryStore()
	id := persistence.SessionID("s-1")
	store.Seed(id,
		session.Event{Type: "user/message", Data: userMessage(t, "m1"), SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
	)
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: id, Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	events, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	cache := persistence.NewCache()
	first, err := cache.Messages(events)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !cache.Valid() {
		t.Fatal("a successful build must mark the cache valid")
	}
	// A second call with the same log serves the cached value.
	second, err := cache.Messages(events)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected history sizes %d and %d", len(first), len(second))
	}
	// Deleting the cache must not change the answer.
	cache.Discard()
	if cache.Valid() {
		t.Fatal("Discard must invalidate the cache")
	}
	third, err := cache.Messages(events)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if llm.ContentText(third[0].Content) != llm.ContentText(first[0].Content) {
		t.Fatalf("rebuild differs: %q vs %q", llm.ContentText(third[0].Content), llm.ContentText(first[0].Content))
	}
}

// A cache built from a shorter log rebuilds when the log grows, so a reader never
// sees a history shorter than the log it came from.
func TestCacheRebuildsWhenTheLogGrows(t *testing.T) {
	store := persistence.NewMemoryStore()
	id := persistence.SessionID("s-1")
	store.Seed(id, session.Event{Type: "user/message", Data: userMessage(t, "m1"), SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}})
	handle, err := store.Open(context.Background(), persistence.OpenOptions{ID: id, Access: persistence.AccessWrite})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	cache := persistence.NewCache()
	events, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := cache.Messages(events); err != nil {
		t.Fatalf("first build: %v", err)
	}
	// Append a second message and rebuild.
	if _, err := handle.Append(context.Background(), session.AppendInput{
		Type: "user/message", Data: userMessage(t, "m2"),
		SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	grown, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	messages, err := cache.Messages(grown)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("the cache must rebuild for a longer log, got %d", len(messages))
	}
}

// A rebuild that fails leaves no usable value, so the next call tries again
// rather than serving a partial history.
func TestCacheFailureLeavesNoValue(t *testing.T) {
	cache := persistence.NewCache()
	// An event whose payload cannot be read as a message fails the fold.
	broken := []session.Event{{
		Type: "user/message",
		Data: []byte(`{"not":"a message"}`),
		Seq:  0,
	}}
	if _, err := cache.Messages(broken); err == nil {
		t.Fatal("an unreadable event must fail the build")
	}
	if cache.Valid() {
		t.Fatal("a failed build must leave the cache invalid")
	}
	// A later call with a good log succeeds.
	good := []session.Event{{
		Type:      "user/message",
		Data:      userMessageValue(t),
		Seq:       0,
		SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	}}
	if _, err := cache.Messages(good); err != nil {
		t.Fatalf("a good log must build: %v", err)
	}
	if !cache.Valid() {
		t.Fatal("a successful rebuild must mark the cache valid again")
	}
}

// A missing interpreter is reported through the projection check.
func TestCheckProjectionsReportsUnknownSurfaceTypes(t *testing.T) {
	registry := session.NewRegistry("user/message")
	events := []session.Event{
		{Type: "user/message", Data: userMessageValue(t), SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		{Type: "assistant/message", Data: []byte(`{"message":{}}`)},
	}
	err := persistence.CheckProjections(events, registry)
	if !errors.Is(err, persistence.ErrMissingProjection) {
		t.Fatalf("want ErrMissingProjection, got %v", err)
	}
	if !strings.Contains(err.Error(), "assistant/message") {
		t.Fatalf("the error must name the missing type: %v", err)
	}
	// With both types registered the check passes.
	full := session.NewRegistry("user/message", "assistant/message")
	if err := persistence.CheckProjections(events, full); err != nil {
		t.Fatalf("a complete registry must pass: %v", err)
	}
	// A non-surface event needs no interpreter.
	if err := persistence.CheckProjections([]session.Event{{Type: "turn/start"}}, registry); err != nil {
		t.Fatalf("a boundary event needs no interpreter: %v", err)
	}
	if err := persistence.CheckProjections(events, nil); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a nil registry, got %v", err)
	}
}

// userMessageValue returns a user message payload for cache tests.
func userMessageValue(t *testing.T) []byte {
	t.Helper()
	return userMessage(t, "m-cache")
}
