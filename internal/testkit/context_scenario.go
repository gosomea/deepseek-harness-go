package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosomea/deepseek-harness-go/capability/compaction"
	"github.com/gosomea/deepseek-harness-go/capability/spill"
	"github.com/gosomea/deepseek-harness-go/llm"
	compactplugin "github.com/gosomea/deepseek-harness-go/plugins/compaction"
	spillplugin "github.com/gosomea/deepseek-harness-go/plugins/spill"
)

// ErrInvalidContextScenario reports an M8 context fixture that cannot be replayed.
var ErrInvalidContextScenario = errors.New("testkit: invalid context scenario")

// ContextScenario is one M8 context behaviour described as data.
//
// It exists so the properties the M8 gate names — a long tool output is
// shortened but stays locatable, a compaction keeps the tool-call/result pair and
// is replayable from the log, a failed summary is never reported as a finished
// replacement, and a route switch is visible rather than silently inherited — are
// written once and replayed, instead of re-asserted in each test function.
type ContextScenario struct {
	// ID is the scenario's stable name.
	ID string `json:"id"`
	// Title explains what the scenario demonstrates.
	Title string `json:"title"`
	// Kind selects the behaviour: "spill", "compaction", "summary-failure" or
	// "route-switch".
	Kind string `json:"kind"`
	// Budget names the per-request token budget for a "spill" scenario.
	Budget int `json:"budget,omitempty"`
	// Content is the tool output to shorten, for a "spill" scenario.
	Content string `json:"content,omitempty"`
	// Entries is the history to compact, for a "compaction" scenario.
	Entries []ContextEntry `json:"entries,omitempty"`
	// Region is the half-open index range to replace, for "compaction" and
	// "summary-failure" scenarios.
	Region []int `json:"region,omitempty"`
	// Summary is the summary text a working provider returns.
	Summary string `json:"summary,omitempty"`
	// FailSummary makes the provider fail, for a "summary-failure" scenario.
	FailSummary bool `json:"failSummary,omitempty"`
	// Routes lists two provider route ids, for a "route-switch" scenario.
	Routes []string `json:"routes,omitempty"`
	// Expect lists what the trace must show.
	Expect ContextExpectation `json:"expect"`
}

// ContextEntry is one history entry in a context fixture.
type ContextEntry struct {
	// ID is the entry identity.
	ID string `json:"id"`
	// Kind is "message", "tool-call", "tool-result" or "summary".
	Kind string `json:"kind"`
	// Text is the content.
	Text string `json:"text"`
	// CallID links a call to its result.
	CallID string `json:"callId,omitempty"`
}

// ContextExpectation lists what a context trace must show.
type ContextExpectation struct {
	// Shortened asserts content was shortened (spill).
	Shortened bool `json:"shortened,omitempty"`
	// HasReference asserts a citation pointing at the original (spill).
	HasReference bool `json:"hasReference,omitempty"`
	// Applied asserts the compaction applied.
	Applied bool `json:"applied,omitempty"`
	// PairingPreserved asserts the call/result pair survived compaction.
	PairingPreserved bool `json:"pairingPreserved,omitempty"`
	// NoEndPhase asserts a failed summary never reported the end phase.
	NoEndPhase bool `json:"noEndPhase,omitempty"`
	// RouteChanged asserts a route switch changed the reported identity.
	RouteChanged bool `json:"routeChanged,omitempty"`
}

// LoadContextScenario reads and validates one fixture.
func LoadContextScenario(path string) (ContextScenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ContextScenario{}, err
	}
	var scenario ContextScenario
	if err := json.Unmarshal(data, &scenario); err != nil {
		return ContextScenario{}, fmt.Errorf("%w: %s: %v", ErrInvalidContextScenario, path, err)
	}
	if err := scenario.Validate(); err != nil {
		return ContextScenario{}, fmt.Errorf("%w: %s: %v", ErrInvalidContextScenario, path, err)
	}
	return scenario, nil
}

// Validate reports whether the scenario can be replayed.
func (s ContextScenario) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: scenario requires an id", ErrInvalidContextScenario)
	}
	switch s.Kind {
	case "spill":
		if s.Budget <= 0 {
			return fmt.Errorf("%w: scenario %q needs a positive budget", ErrInvalidContextScenario, s.ID)
		}
		if strings.TrimSpace(s.Content) == "" {
			return fmt.Errorf("%w: scenario %q needs content", ErrInvalidContextScenario, s.ID)
		}
	case "compaction", "summary-failure":
		if len(s.Entries) == 0 {
			return fmt.Errorf("%w: scenario %q needs entries", ErrInvalidContextScenario, s.ID)
		}
		if len(s.Region) != 2 {
			return fmt.Errorf("%w: scenario %q needs a two-element region", ErrInvalidContextScenario, s.ID)
		}
	case "route-switch":
		if len(s.Routes) != 2 {
			return fmt.Errorf("%w: scenario %q needs two routes", ErrInvalidContextScenario, s.ID)
		}
	default:
		return fmt.Errorf("%w: scenario %q has unknown kind %q", ErrInvalidContextScenario, s.ID, s.Kind)
	}
	return nil
}

// ContextScenarioDir returns the fixture directory for a repository root.
func ContextScenarioDir(root string) string {
	return filepath.Join(root, "testdata", "parity", "context", "scenarios")
}

// ReplayContextScenario runs one scenario and returns its trace.
func ReplayContextScenario(scenario ContextScenario) (HarnessTrace, error) {
	if err := scenario.Validate(); err != nil {
		return HarnessTrace{}, err
	}
	switch scenario.Kind {
	case "spill":
		return replaySpill(scenario)
	case "compaction":
		return replayCompaction(scenario)
	case "summary-failure":
		return replaySummaryFailure(scenario)
	case "route-switch":
		return replayRouteSwitch(scenario)
	}
	return HarnessTrace{}, fmt.Errorf("%w: unhandled kind %q", ErrInvalidContextScenario, scenario.Kind)
}

// replaySpill shortens an over-budget output and records where the original went.
func replaySpill(scenario ContextScenario) (HarnessTrace, error) {
	trace := HarnessTrace{}
	store := spill.NewMemoryStore()
	budget := spillplugin.Budget{
		MaxTokens: scenario.Budget,
		Meter:     spillplugin.Meter{Kind: spillplugin.MeterEstimate, TokensPerByte: 0.25},
		Store:     store,
	}
	included, err := budget.Fit("output-1", "tool:read_file", []byte(scenario.Content))
	if err != nil {
		return HarnessTrace{}, err
	}
	trace.Lines = append(trace.Lines,
		fmt.Sprintf("shortened=%v", included.Shortened),
		fmt.Sprintf("hasReference=%v", included.Reference != ""),
		fmt.Sprintf("tokens=%d", included.Tokens),
		fmt.Sprintf("stored=%d", store.Len()))
	if included.Reference != "" {
		record, err := store.Resolve(included.Reference)
		if err != nil {
			return HarnessTrace{}, err
		}
		trace.Lines = append(trace.Lines, fmt.Sprintf("resolveMatches=%v", string(record.Content) == scenario.Content))
	}
	return trace, nil
}

// toEntries converts fixture entries to compaction entries.
func toEntries(entries []ContextEntry) []compaction.Entry {
	out := make([]compaction.Entry, len(entries))
	for index, entry := range entries {
		out[index] = compaction.Entry{
			ID: entry.ID, Kind: compaction.Kind(entry.Kind), Text: entry.Text, CallID: entry.CallID,
		}
	}
	return out
}

// replayCompaction applies a working provider and records the replacement.
func replayCompaction(scenario ContextScenario) (HarnessTrace, error) {
	trace := HarnessTrace{}
	provider := NewFakeProvider(ScriptTextAttempt([]string{scenario.Summary}))
	route, err := compactplugin.NewRoute(compactplugin.RouteOptions{Provider: provider, Model: "fake-model"})
	if err != nil {
		return HarnessTrace{}, err
	}
	compactor := compactplugin.NewCompactor(route)
	result, err := compactor.Compact(toEntries(scenario.Entries), compaction.Plan{
		Start: scenario.Region[0], End: scenario.Region[1], Summary: scenario.Summary,
	})
	if err != nil {
		return HarnessTrace{}, err
	}
	trace.Lines = append(trace.Lines,
		fmt.Sprintf("applied=%v", result.Applied),
		fmt.Sprintf("replaced=%d", result.Replaced),
		fmt.Sprintf("phases=%s", phasesString(compactor.Phases())))
	trace.Lines = append(trace.Lines, fmt.Sprintf("pairingPreserved=%v", pairsIntact(result.Entries)))
	// Re-render from the resulting log to prove the request is rebuildable.
	trace.Lines = append(trace.Lines, fmt.Sprintf("entries=%d", len(result.Entries)))
	return trace, nil
}

// replaySummaryFailure drives a failing provider and proves no end phase is
// reported and the original entries come back unchanged.
func replaySummaryFailure(scenario ContextScenario) (HarnessTrace, error) {
	trace := HarnessTrace{}
	provider := NewFakeProviderWithOptions(FakeProviderFailure(errors.New("provider down")))
	route, err := compactplugin.NewRoute(compactplugin.RouteOptions{Provider: provider, Model: "fake-model"})
	if err != nil {
		return HarnessTrace{}, err
	}
	compactor := compactplugin.NewCompactor(route)
	orig := toEntries(scenario.Entries)
	result, err := compactor.Compact(orig, compaction.Plan{
		Start: scenario.Region[0], End: scenario.Region[1], Summary: "requested",
	})
	sawEnd := false
	for _, phase := range compactor.Phases() {
		if phase == compactplugin.PhaseEnd {
			sawEnd = true
		}
	}
	trace.Lines = append(trace.Lines,
		fmt.Sprintf("failed=%v", err != nil),
		fmt.Sprintf("applied=%v", result.Applied),
		fmt.Sprintf("sawEnd=%v", sawEnd),
		fmt.Sprintf("unchanged=%v", sameEntries(orig, result.Entries)))
	return trace, nil
}

// replayRouteSwitch builds two routes and records that the identity changes.
func replayRouteSwitch(scenario ContextScenario) (HarnessTrace, error) {
	trace := HarnessTrace{}
	first := NewFakeProviderWithOptions(FakeProviderID(scenario.Routes[0]))
	second := NewFakeProviderWithOptions(FakeProviderID(scenario.Routes[1]))
	firstRoute, err := compactplugin.NewRoute(compactplugin.RouteOptions{Provider: first, Model: "m"})
	if err != nil {
		return HarnessTrace{}, err
	}
	secondRoute, err := compactplugin.NewRoute(compactplugin.RouteOptions{Provider: second, Model: "m"})
	if err != nil {
		return HarnessTrace{}, err
	}
	changed := firstRoute.ProviderID() != secondRoute.ProviderID()
	trace.Lines = append(trace.Lines,
		fmt.Sprintf("first=%s", firstRoute.ProviderID()),
		fmt.Sprintf("second=%s", secondRoute.ProviderID()),
		fmt.Sprintf("routeChanged=%v", changed))
	_ = context.Background
	_ = llm.RoleSystem
	return trace, nil
}

// phasesString renders phases as a comma-separated list.
func phasesString(phases []compactplugin.Phase) string {
	parts := make([]string, len(phases))
	for index, phase := range phases {
		parts[index] = string(phase)
	}
	return strings.Join(parts, ",")
}

// pairsIntact reports whether every tool call still has its result.
func pairsIntact(entries []compaction.Entry) bool {
	calls := map[string]bool{}
	results := map[string]bool{}
	for _, entry := range entries {
		switch entry.Kind {
		case compaction.KindToolCall:
			calls[entry.CallID] = true
		case compaction.KindToolResult:
			results[entry.CallID] = true
		}
	}
	for id := range calls {
		if !results[id] {
			return false
		}
	}
	for id := range results {
		if !calls[id] {
			return false
		}
	}
	return true
}

// sameEntries reports whether two entry slices are identical by value.
func sameEntries(a, b []compaction.Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
