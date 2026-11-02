package llm_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
)

// A registry is shared by concurrent producers and consumers, so registration
// and lookup must be safe under the race detector.
func TestRegistryIsSafeUnderConcurrentUse(t *testing.T) {
	registry := llm.NewProviderRegistry()
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			name := string(rune('a' + index))
			_ = registry.Register(testkit.NewFakeProviderWithOptions(testkit.FakeProviderID(name)))
			_, _, _ = registry.Lookup(name)
			_ = registry.List()
		}(index)
	}
	group.Wait()
	if got := len(registry.List()); got != 8 {
		t.Fatalf("want 8 registered routes, got %d", got)
	}
}

// Providers are released in a single pass, and the unregister path must not
// leave a route resolvable.
func TestRegistryUnregisterRemovesTheRoute(t *testing.T) {
	registry := llm.NewProviderRegistry()
	provider := testkit.NewFakeProviderWithOptions(testkit.FakeProviderID("gone"))
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !registry.Unregister("gone") {
		t.Fatal("unregister of a present route must report true")
	}
	if _, err := registry.Generate(context.Background(), userRequest(t, "gone")); !errors.Is(err, llm.ErrProviderNotFound) {
		t.Fatalf("want ErrProviderNotFound after unregister, got %v", err)
	}
}

// Generate returns the provider's stream unchanged when the route is reachable.
func TestRegistryGenerateForwardsToTheProvider(t *testing.T) {
	registry := llm.NewProviderRegistry()
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	stream, err := registry.Generate(context.Background(), userRequest(t, "fake"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	chunks, err := testkit.Drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if chunks[0].Type != llm.ChunkBlockStart {
		t.Fatalf("unexpected first chunk %s", chunks[0].Type)
	}
	if len(provider.Attempts()) != 1 {
		t.Fatalf("want one attempt, got %d", len(provider.Attempts()))
	}
}
