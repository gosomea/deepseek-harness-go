package compaction_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/compaction"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	plugin "github.com/gosomea/deepseek-harness-go/plugins/compaction"
)

// region returns a small valid history with one call/result pair.
func region() []compaction.Entry {
	return []compaction.Entry{
		{ID: "m1", Kind: compaction.KindMessage, Text: "please read the file"},
		{ID: "c1", Kind: compaction.KindToolCall, Text: "read_file(notes.txt)", CallID: "call-1"},
		{ID: "r1", Kind: compaction.KindToolResult, Text: "hello", CallID: "call-1"},
	}
}

// TestRouteSummarizeSucceeds checks a clean provider produces a summary and the
// request carries the provider identity and both messages.
func TestRouteSummarizeSucceeds(t *testing.T) {
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"summar", "ized"}))
	route, err := plugin.NewRoute(plugin.RouteOptions{Provider: provider, Model: "fake-model"})
	if err != nil {
		t.Fatalf("plugin.NewRoute: %v", err)
	}
	summary, err := route.Summarize(region())
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary != "summarized" {
		t.Fatalf("summary = %q, want summarized", summary)
	}
	attempts := provider.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	if attempts[0].Provider != "fake" || attempts[0].Model != "fake-model" {
		t.Fatalf("request route = %s/%s, want fake/fake-model", attempts[0].Provider, attempts[0].Model)
	}
	if len(attempts[0].Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(attempts[0].Messages))
	}
	if route.ProviderID() != "fake" {
		t.Fatalf("ProviderID = %q, want fake", route.ProviderID())
	}
}

// TestRouteBudgetRefusesBeforeSending checks an oversized region is refused and
// no attempt is made.
func TestRouteBudgetRefusesBeforeSending(t *testing.T) {
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"unused"}))
	route, err := plugin.NewRoute(plugin.RouteOptions{Provider: provider, Model: "m", MaxRegionTokens: 1})
	if err != nil {
		t.Fatalf("plugin.NewRoute: %v", err)
	}
	_, err = route.Summarize(region())
	if !errors.Is(err, plugin.ErrRouteBudget) {
		t.Fatalf("err = %v, want plugin.ErrRouteBudget", err)
	}
	if got := len(provider.Attempts()); got != 0 {
		t.Fatalf("attempts = %d, want 0 (no request on budget refusal)", got)
	}
}

// TestRouteCancelDoesNotReportSummary checks a cancelled context ends without a
// summary rather than returning a short one.
func TestRouteCancelDoesNotReportSummary(t *testing.T) {
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"never read"}))
	route, err := plugin.NewRoute(plugin.RouteOptions{Provider: provider, Model: "m"})
	if err != nil {
		t.Fatalf("plugin.NewRoute: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	summary, err := route.SummarizeContext(ctx, region())
	if err == nil {
		t.Fatalf("expected cancellation error, got summary %q", summary)
	}
	if !errors.Is(err, plugin.ErrSummaryProviderFailed) {
		t.Fatalf("err = %v, want plugin.ErrSummaryProviderFailed", err)
	}
}

// TestRouteEmptyRegion checks an empty region is refused without a request.
func TestRouteEmptyRegion(t *testing.T) {
	provider := testkit.NewFakeProvider()
	route, err := plugin.NewRoute(plugin.RouteOptions{Provider: provider, Model: "m"})
	if err != nil {
		t.Fatalf("plugin.NewRoute: %v", err)
	}
	if _, err := route.Summarize(nil); !errors.Is(err, plugin.ErrSummaryProviderFailed) {
		t.Fatalf("err = %v, want plugin.ErrSummaryProviderFailed", err)
	}
}

// TestNewRouteValidation checks the constructor refuses a missing provider or
// model, so a route that cannot work is never built silently.
func TestNewRouteValidation(t *testing.T) {
	if _, err := plugin.NewRoute(plugin.RouteOptions{Model: "m"}); !errors.Is(err, plugin.ErrRouteNotConfigured) {
		t.Fatalf("nil provider err = %v, want plugin.ErrRouteNotConfigured", err)
	}
	provider := testkit.NewFakeProvider()
	if _, err := plugin.NewRoute(plugin.RouteOptions{Provider: provider}); !errors.Is(err, plugin.ErrRouteEmptyModel) {
		t.Fatalf("empty model err = %v, want plugin.ErrRouteEmptyModel", err)
	}
}

// TestRouteSwitchReportsIdentity checks a different provider changes the
// reported identity, so a caller can tell the route changed.
func TestRouteSwitchReportsIdentity(t *testing.T) {
	first := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"a"}))
	second := testkit.NewFakeProviderWithOptions(testkit.FakeProviderID("other"), testkit.FakeProviderName("Other"))
	firstRoute, err := plugin.NewRoute(plugin.RouteOptions{Provider: first, Model: "m"})
	if err != nil {
		t.Fatalf("plugin.NewRoute first: %v", err)
	}
	secondRoute, err := plugin.NewRoute(plugin.RouteOptions{Provider: second, Model: "m"})
	if err != nil {
		t.Fatalf("plugin.NewRoute second: %v", err)
	}
	if firstRoute.ProviderID() == secondRoute.ProviderID() {
		t.Fatalf("provider identity did not change across route switch")
	}
	if secondRoute.ProviderID() != "other" {
		t.Fatalf("second ProviderID = %q, want other", secondRoute.ProviderID())
	}
}

// TestRouteProviderFailure checks a provider that refuses to start surfaces a
// summary failure rather than an empty summary.
func TestRouteProviderFailure(t *testing.T) {
	provider := testkit.NewFakeProviderWithOptions(testkit.FakeProviderFailure(errors.New("boom")))
	route, err := plugin.NewRoute(plugin.RouteOptions{Provider: provider, Model: "m"})
	if err != nil {
		t.Fatalf("plugin.NewRoute: %v", err)
	}
	_, err = route.Summarize(region())
	if !errors.Is(err, plugin.ErrSummaryProviderFailed) {
		t.Fatalf("err = %v, want plugin.ErrSummaryProviderFailed", err)
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err = %v, want it to name the refusal", err)
	}
}

// TestEstimateRegionBounds checks the estimate is bounded and deterministic.
func TestEstimateRegionBounds(t *testing.T) {
	first := plugin.EstimateRegion(region())
	second := plugin.EstimateRegion(region())
	if first != second {
		t.Fatalf("estimate not deterministic: %+v vs %+v", first, second)
	}
	if first.Upper < first.Tokens {
		t.Fatalf("upper %d < tokens %d", first.Upper, first.Tokens)
	}
	if first.Method == "" {
		t.Fatalf("estimate must name its method")
	}
}

// TestRouteSummarizeFeedsCompactor checks the route plugs into the three-phase
// flow as a SummaryProvider.
func TestRouteSummarizeFeedsCompactor(t *testing.T) {
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"compressed"}))
	route, err := plugin.NewRoute(plugin.RouteOptions{Provider: provider, Model: "m"})
	if err != nil {
		t.Fatalf("plugin.NewRoute: %v", err)
	}
	compactor := plugin.NewCompactor(route)
	// The plan's Summary is the request, not the result; the route's output is
	// what actually replaces the region. A non-empty placeholder satisfies the
	// plan validation.
	result, err := compactor.Compact(region(), compaction.Plan{Start: 0, End: 1, Summary: "requested"})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !result.Applied {
		t.Fatalf("compaction not applied")
	}
	if result.Replaced != 1 {
		t.Fatalf("replaced = %d, want 1", result.Replaced)
	}
	if got := compactor.Phases(); len(got) != 3 || got[2] != plugin.PhaseEnd {
		t.Fatalf("phases = %v, want start,summary,end", got)
	}
}

// compile-time assertion that plugin.Route satisfies SummaryProvider.
var _ plugin.SummaryProvider = (*plugin.Route)(nil)

// unused import guard for llm (kept for the assertion above to reference).
var _ = llm.RoleSystem
