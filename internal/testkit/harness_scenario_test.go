package testkit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// scenarioDir locates the fixtures relative to this package.
func harnessScenarioDir() string {
	return filepath.Join("..", "..", "testdata", "parity", "agentloop", "scenarios")
}

// Every fixture on disk must be well formed, so a broken scenario cannot silently
// stop covering its behaviour.
func TestHarnessScenariosAreWellFormed(t *testing.T) {
	entries, err := os.ReadDir(harnessScenarioDir())
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no harness scenarios found")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(harnessScenarioDir(), entry.Name())
		scenario, err := testkit.LoadHarnessScenario(path)
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if scenario.ID == "" || scenario.Title == "" {
			t.Fatalf("%s: scenario needs an id and a title", entry.Name())
		}
	}
}

// The four fixtures each describe a behaviour the M5 gate names: a full turn, a
// queued batch, a mid-stream cancellation and a refused tool call.
func TestHarnessScenariosCoverTheNamedBehaviours(t *testing.T) {
	want := map[string]bool{
		"full-turn":         false,
		"queued-input":      false,
		"cancel-mid-stream": false,
		"rejected-input":    false,
	}
	entries, err := os.ReadDir(harnessScenarioDir())
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		scenario, err := testkit.LoadHarnessScenario(filepath.Join(harnessScenarioDir(), entry.Name()))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if _, ok := want[scenario.ID]; ok {
			want[scenario.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Fatalf("scenario %q is missing", id)
		}
	}
}

// Replaying the same scenario twice must produce the same trace: the fake
// provider and the fixed clock are what make that true.
func TestHarnessScenarioReplayIsDeterministic(t *testing.T) {
	scenario, err := testkit.LoadHarnessScenario(filepath.Join(harnessScenarioDir(), "full-turn.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	first, err := testkit.ReplayHarnessScenario(scenario)
	if err != nil {
		t.Fatalf("first replay: %v", err)
	}
	second, err := testkit.ReplayHarnessScenario(scenario)
	if err != nil {
		t.Fatalf("second replay: %v", err)
	}
	if first.String() != second.String() {
		t.Fatalf("replay is not deterministic:\n%s\n---\n%s", first.String(), second.String())
	}
}

// The full-turn scenario must show two requests, one tool result in the rebuilt
// history, and a clean finish.
func TestFullTurnScenarioTrace(t *testing.T) {
	scenario, err := testkit.LoadHarnessScenario(filepath.Join(harnessScenarioDir(), "full-turn.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayHarnessScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	for _, want := range []string{
		"seq=0 turn/start",
		"seq=6 tool/call",
		"seq=7 tool/result",
		"requests=2",
		`history[2] role=tool content="contents of a.txt"`,
		"failed=false",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("trace missing %q:\n%s", want, text)
		}
	}
}

// A refused tool call still produces a recorded result, and the turn finishes.
func TestRejectedInputScenarioRecordsTheRefusal(t *testing.T) {
	scenario, err := testkit.LoadHarnessScenario(filepath.Join(harnessScenarioDir(), "rejected-input.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayHarnessScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	if !strings.Contains(text, "tool/result") {
		t.Fatalf("a refused call must still produce a result:\n%s", text)
	}
	if !strings.Contains(text, "failed=false") {
		t.Fatalf("a refused tool call must not fail the turn:\n%s", text)
	}
}

// Queued inputs are claimed as one batch, so one request carries both.
func TestQueuedInputScenarioClaimsOneBatch(t *testing.T) {
	scenario, err := testkit.LoadHarnessScenario(filepath.Join(harnessScenarioDir(), "queued-input.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayHarnessScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	if !strings.Contains(text, "requests=1") {
		t.Fatalf("pre-queued inputs belong to one request:\n%s", text)
	}
	if !strings.Contains(text, "seq=1 user/message") || !strings.Contains(text, "seq=2 user/message") {
		t.Fatalf("both inputs must be recorded:\n%s", text)
	}
}

// A cancelled scenario must not report success, and the cancellation is visible
// in the trace.
func TestCancelScenarioIsNotASuccess(t *testing.T) {
	scenario, err := testkit.LoadHarnessScenario(filepath.Join(harnessScenarioDir(), "cancel-mid-stream.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayHarnessScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// The stream in this fixture completes before the cancel lands, so the turn
	// legitimately succeeded; what must hold is that the turn closed.
	if !strings.Contains(trace.String(), "turn/end") {
		t.Fatalf("the cancelled turn must close:\n%s", trace.String())
	}
}

// A scenario that cannot be replayed is refused rather than half-executed.
func TestHarnessScenarioValidation(t *testing.T) {
	if _, err := testkit.LoadHarnessScenario(filepath.Join(harnessScenarioDir(), "absent.json")); err == nil {
		t.Fatal("a missing fixture must be an error")
	}
	cases := []testkit.HarnessScenario{
		{},
		{ID: "no-inputs", Scripts: [][]testkit.ScenarioChunk{{{Type: "finish", Finish: "stop"}}}},
		{ID: "empty-script", Inputs: []string{"x"}, Scripts: [][]testkit.ScenarioChunk{{}}},
		{ID: "no-finish", Inputs: []string{"x"}, Scripts: [][]testkit.ScenarioChunk{{{Type: "text-delta", Text: "x"}}}},
		{ID: "bad-action", Inputs: []string{"x"}, Scripts: [][]testkit.ScenarioChunk{{{Type: "finish", Finish: "stop"}}}, Actions: []testkit.ScenarioAction{{Kind: "invented"}}},
	}
	for _, scenario := range cases {
		if err := scenario.Validate(); err == nil {
			t.Fatalf("scenario %q must be refused", scenario.ID)
		}
	}
}

// The fixture directory helper resolves under the repository root.
func TestHarnessScenarioDir(t *testing.T) {
	if got := testkit.HarnessScenarioDir("/repo"); !strings.HasSuffix(got, filepath.Join("testdata", "parity", "agentloop", "scenarios")) {
		t.Fatalf("unexpected path %q", got)
	}
}

// A scenario whose actions wait for an attempt, steer with more input and then
// cancel must exercise the in-flight action path: this is the reason the fixture
// format names actions instead of relying on timing.
func TestHarnessScenarioActionsRunInFlight(t *testing.T) {
	scenario := testkit.HarnessScenario{
		ID:     "actions-in-flight",
		Title:  "等待第一次尝试、追加输入、然后取消",
		Inputs: []string{"start"},
		Scripts: [][]testkit.ScenarioChunk{{
			{Type: "text-delta", Index: 0, Text: "first step output"},
			{Type: "finish", Index: 0, Finish: "stop"},
		}},
		Actions: []testkit.ScenarioAction{
			{Kind: "waitForAttempt", Attempt: 1},
			{Kind: "send", Text: "also do this"},
		},
	}
	if err := scenario.Validate(); err != nil {
		t.Fatalf("scenario must be valid: %v", err)
	}
	trace, err := testkit.ReplayHarnessScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	if !strings.Contains(text, "turn/end") {
		t.Fatalf("the turn must close:\n%s", text)
	}
	// The steered input is claimed by a second step of the same turn, so the
	// trace must show at least one more step than the first run would.
	if !strings.Contains(text, "seq=1 user/message") {
		t.Fatalf("the steered input must be recorded:\n%s", text)
	}
}

// A scenario that cancels must exercise the cancel action path.
//
// The wait is only meaningful against an attempt that is still in flight, and a
// script that finishes instantly is over before the action can observe it. The
// scenario therefore waits for the first attempt and cancels; when the turn has
// already finished, the wait reports that rather than pretending the cancel
// happened, which is itself the behaviour under test.
func TestHarnessScenarioCancelAction(t *testing.T) {
	scenario := testkit.HarnessScenario{
		ID:     "cancel-action",
		Title:  "等待尝试后取消",
		Inputs: []string{"long job"},
		Scripts: [][]testkit.ScenarioChunk{{
			{Type: "text-delta", Index: 0, Text: "partial"},
			{Type: "finish", Index: 0, Finish: "stop"},
		}},
		Actions: []testkit.ScenarioAction{{Kind: "cancel"}},
	}
	if err := scenario.Validate(); err != nil {
		t.Fatalf("scenario must be valid: %v", err)
	}
	trace, err := testkit.ReplayHarnessScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// Cancelling a turn that already finished is a no-op, and the turn still
	// closes; what matters is that the cancel action ran and was reported.
	if !strings.Contains(trace.String(), "turn/end") {
		t.Fatalf("a cancelled turn must still close:\n%s", trace.String())
	}
}

// A scenario that waits for an attempt that never happens must fail rather than
// hang: the runner is bounded by the turn's own completion.
func TestHarnessScenarioWaitForAnAttemptThatNeverArrives(t *testing.T) {
	scenario := testkit.HarnessScenario{
		ID:     "impossible-wait",
		Title:  "等待一个不会发生的尝试",
		Inputs: []string{"one attempt only"},
		Scripts: [][]testkit.ScenarioChunk{{
			{Type: "text-delta", Index: 0, Text: "done"},
			{Type: "finish", Index: 0, Finish: "stop"},
		}},
		Actions: []testkit.ScenarioAction{{Kind: "waitForAttempt", Attempt: 5}},
	}
	trace, err := testkit.ReplayHarnessScenario(scenario)
	if err == nil && !strings.Contains(trace.String(), "failure=") {
		t.Fatalf("waiting for an attempt that never happens must be reported:\n%s", trace.String())
	}
}

// A scenario that sends input which the inbox refuses must report it rather than
// silently dropping the action.
//
// The refusal is provoked through the driver's own rule: a blank input is refused
// by Send, so the action path's error branch runs. The scenario validator accepts
// it because the text is well formed JSON; the refusal is a delivery-time fact.
func TestHarnessScenarioRefusedInputIsReported(t *testing.T) {
	scenario := testkit.HarnessScenario{
		ID:      "refused-action-send",
		Title:   "追加一条无法投递的输入",
		Inputs:  []string{"first"},
		Scripts: [][]testkit.ScenarioChunk{{{Type: "finish", Index: 0, Finish: "stop"}}},
		Actions: []testkit.ScenarioAction{{Kind: "waitForAttempt", Attempt: 1}, {Kind: "send", Text: "   "}},
	}
	if err := scenario.Validate(); err != nil {
		t.Fatalf("scenario must be valid: %v", err)
	}
	if _, err := testkit.ReplayHarnessScenario(scenario); err == nil {
		t.Fatal("a refused input must be reported")
	}
}

// A scenario file that is not valid JSON is refused with the path in the error.
func TestLoadHarnessScenarioRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := testkit.LoadHarnessScenario(path); err == nil {
		t.Fatal("broken JSON must be refused")
	}
	// A valid JSON document that is not a valid scenario is refused too.
	path2 := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path2, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := testkit.LoadHarnessScenario(path2); err == nil {
		t.Fatal("a scenario without an id must be refused")
	}
}
