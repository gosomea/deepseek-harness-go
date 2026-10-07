package testkit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

func TestLoadScenarioRejectsMalformedInput(t *testing.T) {
	dir := t.TempDir()
	cases := []struct{ name, body string }{
		{"bad-json.json", "{not json"},
		{"no-id.json", `{"title":"x","steps":[{"op":"settle"}]}`},
		{"no-steps.json", `{"id":"x","title":"x"}`},
	}
	for _, testCase := range cases {
		path := filepath.Join(dir, testCase.name)
		if err := os.WriteFile(path, []byte(testCase.body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := testkit.LoadScenario(path); err == nil {
			t.Errorf("%s should be rejected", testCase.name)
		}
	}
	if _, err := testkit.LoadScenario(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("a missing file should be rejected")
	}
}

func TestScenarioFilesReturnsOnlyJSON(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.json", "b.json", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := testkit.ScenarioFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 scenario files, got %v", files)
	}
	for _, file := range files {
		if filepath.Ext(file) != ".json" {
			t.Errorf("unexpected file %s", file)
		}
	}
}

// A scenario that asks for an impossible operation must fail the replay rather
// than silently producing a short trace that could pass comparison.
func TestReplayRejectsUnknownOperations(t *testing.T) {
	cases := []struct {
		name string
		step testkit.Step
	}{
		{"unknown-op", testkit.Step{Op: "teleport"}},
		{"dispose-unknown-node", testkit.Step{Op: "dispose", As: "ghost"}},
		{"update-unknown-node", testkit.Step{Op: "update", As: "ghost"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			scenario := &testkit.Scenario{ID: testCase.name, Steps: []testkit.Step{testCase.step}}
			if _, err := testkit.Replay(scenario); err == nil {
				t.Fatal("replay should reject the step")
			}
		})
	}
}

func TestReplayReportsInvalidConfig(t *testing.T) {
	// A config that is valid JSON but not an object is still a legal Go value,
	// so registration succeeds and the trace records the active node. The
	// interesting failure is a validator actually rejecting an update.
	scenario := &testkit.Scenario{ID: "scalar-config", Steps: []testkit.Step{
		{Op: "register", As: "x", Name: "x", Config: []byte("123")},
		{Op: "observe", Note: "registered"},
	}}
	trace, err := testkit.Replay(scenario)
	if err != nil {
		t.Fatalf("a scalar config is a valid Go value: %v", err)
	}
	if !strings.Contains(trace, "state x=active") {
		t.Fatalf("trace should record the active node, got:\n%s", trace)
	}

	// A validator that rejects the config must produce a rejected update, and
	// the running activation must survive it.
	rejecting := &testkit.Scenario{ID: "rejected-update", Steps: []testkit.Step{
		{Op: "register", As: "x", Name: "x", Validate: true, Config: []byte(`{"n":"good-1"}`)},
		{Op: "update", As: "x", Config: []byte(`{"n":"bad"}`)},
		{Op: "observe", Note: "afterReject"},
	}}
	trace, err = testkit.Replay(rejecting)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trace, "update x=rejected") || !strings.Contains(trace, "state x=active") {
		t.Fatalf("rejected update must leave the activation running, got:\n%s", trace)
	}
}

func TestCompareTracesRejectsIncompleteScenarioDeclaration(t *testing.T) {
	scenario := &testkit.Scenario{
		ID:    "incomplete",
		Steps: []testkit.Step{{Op: "settle"}},
		Divergences: []testkit.Divergence{{
			Go:        nil,
			Reference: []string{"x"},
			Reason:    "missing go side",
		}},
	}
	err := testkit.CompareTraces(scenario, "x\n", "\n")
	if err == nil || !strings.Contains(err.Error(), "is incomplete") {
		t.Fatalf("an incomplete divergence must be rejected, got %v", err)
	}
}
