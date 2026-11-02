package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
)

func userRequest(t *testing.T, provider string) llm.Request {
	t.Helper()
	message, err := llm.NewUserMessage("hello")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	return llm.Request{Provider: provider, Model: "fake-1", Messages: []llm.Message{message}}
}

func TestProviderRegistryRegistersLooksUpAndListsInOrder(t *testing.T) {
	registry := llm.NewProviderRegistry()
	second := testkit.NewFakeProviderWithOptions(testkit.FakeProviderID("zeta"), testkit.FakeProviderName("Zeta"))
	first := testkit.NewFakeProviderWithOptions(testkit.FakeProviderID("alpha"), testkit.FakeProviderName("Alpha"))
	for _, provider := range []llm.Provider{second, first} {
		if err := registry.Register(provider); err != nil {
			t.Fatalf("register %s: %v", provider.Info().ID, err)
		}
	}
	infos := registry.List()
	if len(infos) != 2 || infos[0].ID != "alpha" || infos[1].ID != "zeta" {
		t.Fatalf("List must be sorted by route key, got %+v", infos)
	}
	provider, ok, err := registry.Lookup("alpha")
	if err != nil || !ok || provider.Info().ID != "alpha" {
		t.Fatalf("lookup alpha: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := registry.Lookup("missing"); ok {
		t.Fatal("unknown route must not resolve")
	}
	if !registry.Unregister("alpha") {
		t.Fatal("unregister of a present route must report true")
	}
	if registry.Unregister("alpha") {
		t.Fatal("unregister of an absent route must report false")
	}
}

func TestProviderRegistryRefusesDuplicateNilAndEmptyID(t *testing.T) {
	registry := llm.NewProviderRegistry()
	provider := testkit.NewFakeProviderWithOptions(testkit.FakeProviderID("dup"))
	if err := registry.Register(provider); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := registry.Register(provider); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("duplicate register: want ErrInvalidValue, got %v", err)
	}
	if err := registry.Register(nil); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("nil register: want ErrInvalidValue, got %v", err)
	}
	empty := testkit.NewFakeProviderWithOptions(testkit.FakeProviderID(""))
	if err := registry.Register(empty); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("empty id register: want ErrInvalidValue, got %v", err)
	}
}

func TestGenerateRefusesUnknownRouteAndInvalidRequest(t *testing.T) {
	registry := llm.NewProviderRegistry()
	if err := registry.Register(testkit.NewFakeProviderWithOptions(testkit.FakeProviderID("fake"))); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := registry.Generate(context.Background(), userRequest(t, "missing")); !errors.Is(err, llm.ErrProviderNotFound) {
		t.Fatalf("unknown route: want ErrProviderNotFound, got %v", err)
	}
	invalid := llm.Request{Provider: "fake"}
	if _, err := registry.Generate(context.Background(), invalid); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("invalid request: want ErrInvalidValue, got %v", err)
	}
}

// A route that does not declare streaming must refuse the request before any
// attempt starts, instead of silently producing a degenerate stream.
func TestGenerateRefusesUndeclaredCapabilityBeforeStarting(t *testing.T) {
	registry := llm.NewProviderRegistry()
	provider := testkit.NewFakeProviderWithOptions(
		testkit.FakeProviderID("text-only"),
		testkit.FakeProviderCapabilities(llm.CapabilityUsage),
	)
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, err := registry.Generate(context.Background(), userRequest(t, "text-only"))
	var unsupported *llm.UnsupportedCapabilityError
	if !errors.As(err, &unsupported) {
		t.Fatalf("want UnsupportedCapabilityError, got %v", err)
	}
	if unsupported.Capability != llm.CapabilityStreaming || unsupported.Provider != "text-only" {
		t.Fatalf("unexpected unsupported error %+v", unsupported)
	}
	if got := len(provider.Attempts()); got != 0 {
		t.Fatalf("a refused request must not reach the provider, got %d attempts", got)
	}
}

// Close must release every provider exactly once and join their errors, and a
// closed registry must refuse later registration rather than racing shutdown.
func TestRegistryCloseReleasesProvidersAndRefusesLateRegistration(t *testing.T) {
	registry := llm.NewProviderRegistry()
	failing := testkit.NewFakeProviderWithOptions(
		testkit.FakeProviderID("failing"),
		testkit.FakeProviderCloseError(errors.New("release failed")),
	)
	ok := testkit.NewFakeProviderWithOptions(testkit.FakeProviderID("ok"))
	for _, provider := range []llm.Provider{failing, ok} {
		if err := registry.Register(provider); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	err := registry.Close()
	if err == nil || err.Error() != "release failed" {
		t.Fatalf("want the joined release error, got %v", err)
	}
	if !failing.Closed() || !ok.Closed() {
		t.Fatal("every provider implementing Close must be released")
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("second Close must be a no-op, got %v", err)
	}
	if err := registry.Register(testkit.NewFakeProviderWithOptions(testkit.FakeProviderID("late"))); !errors.Is(err, llm.ErrProviderClosed) {
		t.Fatalf("late register: want ErrProviderClosed, got %v", err)
	}
	if _, _, err := registry.Lookup("ok"); !errors.Is(err, llm.ErrProviderClosed) {
		t.Fatalf("lookup after close: want ErrProviderClosed, got %v", err)
	}
}

func TestGenerateReportsProviderStartupFailure(t *testing.T) {
	registry := llm.NewProviderRegistry()
	startup := errors.New("dial failed")
	provider := testkit.NewFakeProviderWithOptions(
		testkit.FakeProviderID("broken"),
		testkit.FakeProviderFailure(startup),
	)
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := registry.Generate(context.Background(), userRequest(t, "broken")); !errors.Is(err, startup) {
		t.Fatalf("want the startup failure, got %v", err)
	}
}

// Cancelling before the attempt starts must not produce a stream: a consumer
// must be able to rely on cancellation being observed rather than deferred.
func TestGenerateHonoursCancellationBeforeStarting(t *testing.T) {
	registry := llm.NewProviderRegistry()
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"hi"}))
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := registry.Generate(ctx, userRequest(t, "fake")); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestRequestCloneIsolatesMessageContent(t *testing.T) {
	request := userRequest(t, "fake")
	clone := request.Clone()
	clone.Messages[0].Content[0] = llm.TextBlock{Text: "mutated"}
	if llm.ContentText(request.Messages[0].Content) != "hello" {
		t.Fatal("Clone must not share content slices with the original")
	}
	cap := 5
	request.MaxOutputTokens = &cap
	clone = request.Clone()
	*clone.MaxOutputTokens = 9
	if *request.MaxOutputTokens != 5 {
		t.Fatal("Clone must not share the optional integer")
	}
}
