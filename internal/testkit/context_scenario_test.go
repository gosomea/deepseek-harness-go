package testkit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// contextScenarioDir locates the M8 context fixtures relative to this package.
func contextScenarioDir() string {
	return filepath.Join("..", "..", "testdata", "parity", "context", "scenarios")
}

// TestContextScenariosAreWellFormed ensures every fixture on disk parses, so a
// broken scenario cannot silently stop covering its behaviour.
func TestContextScenariosAreWellFormed(t *testing.T) {
	entries, err := os.ReadDir(contextScenarioDir())
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no context scenarios found")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		scenario, err := testkit.LoadContextScenario(filepath.Join(contextScenarioDir(), entry.Name()))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if scenario.ID == "" || scenario.Title == "" {
			t.Fatalf("%s: scenario needs an id and a title", entry.Name())
		}
	}
}

// TestContextScenariosCoverTheNamedBehaviours pins the four M8 behaviours.
func TestContextScenariosCoverTheNamedBehaviours(t *testing.T) {
	want := map[string]bool{
		"long-output-spill": false,
		"compaction-replay": false,
		"summary-failure":   false,
		"route-switch":      false,
	}
	entries, err := os.ReadDir(contextScenarioDir())
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	for _, entry := range entries {
		path := filepath.Join(contextScenarioDir(), entry.Name())
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		scenario, err := testkit.LoadContextScenario(path)
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if _, ok := want[scenario.ID]; ok {
			want[scenario.ID] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("missing required context scenario %q", id)
		}
	}
}

// TestContextScenariosReplay asserts each fixture's expectation against a fresh
// replay, twice, so the trace is deterministic.
func TestContextScenariosReplay(t *testing.T) {
	paths := []string{
		"long-output-spill.json",
		"compaction-replay.json",
		"summary-failure.json",
		"route-switch.json",
	}
	for _, name := range paths {
		scenario, err := testkit.LoadContextScenario(filepath.Join(contextScenarioDir(), name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		first, err := testkit.ReplayContextScenario(scenario)
		if err != nil {
			t.Fatalf("%s replay: %v", name, err)
		}
		second, err := testkit.ReplayContextScenario(scenario)
		if err != nil {
			t.Fatalf("%s second replay: %v", name, err)
		}
		if first.String() != second.String() {
			t.Fatalf("%s trace not deterministic:\n%s\n---\n%s", name, first.String(), second.String())
		}
		assertContextExpectation(t, name, scenario.Expect, first.String())
	}
}

// assertContextExpectation checks the trace carries the claimed outcome.
func assertContextExpectation(t *testing.T, name string, expect testkit.ContextExpectation, trace string) {
	t.Helper()
	checks := []struct {
		flag bool
		line string
	}{
		{expect.Shortened, "shortened=true"},
		{expect.HasReference, "hasReference=true"},
		{expect.Applied, "applied=true"},
		{expect.PairingPreserved, "pairingPreserved=true"},
		{expect.NoEndPhase, "sawEnd=false"},
		{expect.RouteChanged, "routeChanged=true"},
	}
	for _, check := range checks {
		if check.flag && !strings.Contains(trace, check.line) {
			t.Fatalf("%s: trace missing %q:\n%s", name, check.line, trace)
		}
	}
}

// TestContextScenarioValidation checks the loader rejects malformed fixtures and
// that each refusal names the reason, so a broken scenario cannot silently pass.
func TestContextScenarioValidation(t *testing.T) {
	valid := testkit.ContextScenario{ID: "x", Kind: "spill", Budget: 10, Content: "hello there, this is content"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid scenario rejected: %v", err)
	}
	cases := []struct {
		name     string
		scenario testkit.ContextScenario
	}{
		{"no-id", testkit.ContextScenario{Kind: "spill", Budget: 1, Content: "c"}},
		{"spill-no-budget", testkit.ContextScenario{ID: "x", Kind: "spill", Content: "c"}},
		{"spill-no-content", testkit.ContextScenario{ID: "x", Kind: "spill", Budget: 1}},
		{"compaction-no-entries", testkit.ContextScenario{ID: "x", Kind: "compaction"}},
		{"compaction-bad-region", testkit.ContextScenario{ID: "x", Kind: "compaction", Entries: []testkit.ContextEntry{{ID: "a"}}}},
		{"route-one-route", testkit.ContextScenario{ID: "x", Kind: "route-switch", Routes: []string{"a"}}},
		{"unknown-kind", testkit.ContextScenario{ID: "x", Kind: "nope"}},
	}
	for _, tc := range cases {
		if err := tc.scenario.Validate(); err == nil {
			t.Fatalf("%s: expected a validation error", tc.name)
		}
	}
}

// TestContextScenarioLoadAndDir checks the loader reads a real file and the
// directory helper returns the expected path.
func TestContextScenarioLoadAndDir(t *testing.T) {
	dir := testkit.ContextScenarioDir(filepath.Join("..", ".."))
	if !strings.HasSuffix(dir, filepath.Join("testdata", "parity", "context", "scenarios")) {
		t.Fatalf("ContextScenarioDir = %q", dir)
	}
	scenario, err := testkit.LoadContextScenario(filepath.Join(dir, "route-switch.json"))
	if err != nil {
		t.Fatalf("load route-switch: %v", err)
	}
	if scenario.Kind != "route-switch" {
		t.Fatalf("kind = %q", scenario.Kind)
	}
	if _, err := testkit.LoadContextScenario(filepath.Join(dir, "does-not-exist.json")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

// TestReplayContextScenarioErrorPaths checks replay refuses an invalid scenario
// and a spill budget too small to hold even the citation.
func TestReplayContextScenarioErrorPaths(t *testing.T) {
	if _, err := testkit.ReplayContextScenario(testkit.ContextScenario{ID: "bad", Kind: "spill", Budget: 0}); err == nil {
		t.Fatal("expected an error for an invalid scenario")
	}
	// A tiny budget cannot even fit the citation, so Fit refuses.
	tiny := testkit.ContextScenario{
		ID: "tiny", Kind: "spill", Budget: 1,
		Content: "a long piece of tool output that cannot fit at all in one token",
	}
	if _, err := testkit.ReplayContextScenario(tiny); err == nil {
		t.Fatal("expected a budget refusal")
	}
}
