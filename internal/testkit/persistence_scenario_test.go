package testkit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// persistenceDir locates the persistence fixtures relative to this package.
func persistenceDir() string {
	return filepath.Join("..", "..", "testdata", "parity", "persistence", "scenarios")
}

// Every fixture on disk must be well formed.
func TestPersistenceScenariosAreWellFormed(t *testing.T) {
	entries, err := os.ReadDir(persistenceDir())
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no persistence scenarios found")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		scenario, err := testkit.LoadPersistenceScenario(filepath.Join(persistenceDir(), entry.Name()))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if scenario.ID == "" || scenario.Title == "" {
			t.Fatalf("%s: scenario needs an id and a title", entry.Name())
		}
	}
}

// The four fixtures cover the behaviours G6 names.
func TestPersistenceScenariosCoverTheNamedBehaviours(t *testing.T) {
	want := map[string]bool{
		"reopen-consistency":    false,
		"second-writer-refused": false,
		"truncated-tail":        false,
		"future-version":        false,
	}
	entries, err := os.ReadDir(persistenceDir())
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		scenario, err := testkit.LoadPersistenceScenario(filepath.Join(persistenceDir(), entry.Name()))
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

// Replaying a scenario twice must produce the same trace.
func TestPersistenceScenarioReplayIsDeterministic(t *testing.T) {
	for _, name := range []string{"reopen-consistency", "second-writer-refused", "truncated-tail", "future-version"} {
		scenario, err := testkit.LoadPersistenceScenario(filepath.Join(persistenceDir(), name+".json"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		first, err := testkit.ReplayPersistenceScenario(scenario)
		if err != nil {
			t.Fatalf("%s first replay: %v", name, err)
		}
		second, err := testkit.ReplayPersistenceScenario(scenario)
		if err != nil {
			t.Fatalf("%s second replay: %v", name, err)
		}
		if first.String() != second.String() {
			t.Fatalf("%s replay is not deterministic:\n%s\n---\n%s", name, first.String(), second.String())
		}
	}
}

// The reopen scenario must show the same history and the same outstanding work,
// which is the G6 criterion in one trace.
func TestReopenConsistencyScenario(t *testing.T) {
	scenario, err := testkit.LoadPersistenceScenario(filepath.Join(persistenceDir(), "reopen-consistency.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayPersistenceScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	for _, want := range []string{
		"events=5 version=1 writerHeld=false",
		"openTurn=true unsettled=[call-1] needsAttention=true pendingInputs=0",
		"historyRoles=user",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("trace missing %q:\n%s", want, text)
		}
	}
}

// The single-writer scenario must refuse the second writer and release on close.
func TestSecondWriterScenario(t *testing.T) {
	scenario, err := testkit.LoadPersistenceScenario(filepath.Join(persistenceDir(), "second-writer-refused.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayPersistenceScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	for _, want := range []string{"secondWriterRefused=true", "readAllowedWhileWriting=true", "releasedAfterClose=true"} {
		if !strings.Contains(text, want) {
			t.Fatalf("trace missing %q:\n%s", want, text)
		}
	}
}

// The torn-tail scenario must be repairable and preserve the committed prefix.
func TestTruncatedTailScenario(t *testing.T) {
	scenario, err := testkit.LoadPersistenceScenario(filepath.Join(persistenceDir(), "truncated-tail.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayPersistenceScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	for _, want := range []string{"repairable=true", "completeAfterRepair=3 prefixUnchanged=true"} {
		if !strings.Contains(text, want) {
			t.Fatalf("trace missing %q:\n%s", want, text)
		}
	}
}

// The future-version scenario must refuse the open with its own error category
// and leave the file alone.
func TestFutureVersionScenario(t *testing.T) {
	scenario, err := testkit.LoadPersistenceScenario(filepath.Join(persistenceDir(), "future-version.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayPersistenceScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	for _, want := range []string{"openRefused=true", "errorKind=unsupported-version", "fileUnchanged=true"} {
		if !strings.Contains(text, want) {
			t.Fatalf("trace missing %q:\n%s", want, text)
		}
	}
}

// A scenario that cannot be replayed is refused rather than half-executed.
func TestPersistenceScenarioValidation(t *testing.T) {
	if _, err := testkit.LoadPersistenceScenario(filepath.Join(persistenceDir(), "absent.json")); err == nil {
		t.Fatal("a missing fixture must be an error")
	}
	yes := true
	cases := []testkit.PersistenceScenario{
		{},
		{ID: "no-events"},
		{ID: "no-type", Events: []testkit.ScenarioEvent{{Data: map[string]any{"a": 1}}}},
		{ID: "bad-surface", Events: []testkit.ScenarioEvent{{Type: "turn/start", Surface: "invented"}}},
		{ID: "bad-damage", Events: []testkit.ScenarioEvent{{Type: "turn/start"}}, Damage: &testkit.ScenarioDamage{Kind: "invented"}},
		{ID: "message-no-role", Events: []testkit.ScenarioEvent{{Type: "user/message", Message: &testkit.ScenarioMessage{ID: "m"}}}},
	}
	for _, scenario := range cases {
		if err := scenario.Validate(); err == nil {
			t.Fatalf("scenario %q must be refused", scenario.ID)
		}
	}
	_ = yes
}

// A scenario with an unreadable message role is refused during replay.
func TestPersistenceScenarioRefusesAnUnknownRole(t *testing.T) {
	scenario := testkit.PersistenceScenario{
		ID: "bad-role",
		Events: []testkit.ScenarioEvent{{
			Type: "system/message", Message: &testkit.ScenarioMessage{ID: "m", Role: "wizard", Text: "x"},
		}},
	}
	if err := scenario.Validate(); err != nil {
		t.Fatalf("the scenario is structurally valid: %v", err)
	}
	if _, err := testkit.ReplayPersistenceScenario(scenario); err == nil {
		t.Fatal("an unsupported role must be reported")
	}
}

// The fixture directory helper resolves under the repository root.
func TestPersistenceScenarioDir(t *testing.T) {
	if got := testkit.PersistenceScenarioDir("/repo"); !strings.HasSuffix(got, filepath.Join("testdata", "parity", "persistence", "scenarios")) {
		t.Fatalf("unexpected path %q", got)
	}
}

// A damaged middle record is refused, and the trace names the position so a
// reader can go look at it.
func TestMiddleCorruptionScenario(t *testing.T) {
	scenario, err := testkit.LoadPersistenceScenario(filepath.Join(persistenceDir(), "middle-corruption.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayPersistenceScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	if !strings.Contains(text, "repairable=false") {
		t.Fatalf("middle damage must not be repairable:\n%s", text)
	}
	if !strings.Contains(text, "checksum mismatch") {
		t.Fatalf("the trace must name the cause:\n%s", text)
	}
}

// A scenario with an assistant message exercises the nested-payload path, and the
// derived history must still contain both roles.
func TestScenarioWithNestedAssistantMessage(t *testing.T) {
	scenario := testkit.PersistenceScenario{
		ID:    "nested-assistant",
		Title: "assistant 消息走嵌套载荷",
		Events: []testkit.ScenarioEvent{
			{Type: "user/message", Message: &testkit.ScenarioMessage{ID: "m1", Role: "user", Text: "ask"}, Surface: "append"},
			{Type: "assistant/message", Message: &testkit.ScenarioMessage{ID: "a1", Role: "assistant", Text: "answer"}, Surface: "append"},
		},
	}
	trace, err := testkit.ReplayPersistenceScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !strings.Contains(trace.String(), "historyRoles=user,assistant") {
		t.Fatalf("the nested assistant message must fold:\n%s", trace.String())
	}
}

// Loading a fixture that is not valid JSON is refused with the path in the error.
func TestLoadPersistenceScenarioRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := testkit.LoadPersistenceScenario(path); err == nil {
		t.Fatal("broken JSON must be refused")
	}
}

// A scenario with no damage takes the plain reopen path; one with a replace
// surface exercises the surface metadata round-trip.
func TestScenarioSurfaceReplaceAndNoDamage(t *testing.T) {
	scenario := testkit.PersistenceScenario{
		ID:    "replace-surface",
		Title: "replace surface 的元数据在重开后仍然有效",
		Events: []testkit.ScenarioEvent{
			{Type: "user/message", Message: &testkit.ScenarioMessage{ID: "m1", Role: "user", Text: "ask"}, Surface: "append"},
			// A replace must cite the node it shadows; the log refuses one that
			// does not, so the fixture states the sequence.
			{Type: "user/message", Message: &testkit.ScenarioMessage{ID: "m2", Role: "user", Text: "ask again"}, Surface: "replace", Shadows: []int{0}},
		},
	}
	trace, err := testkit.ReplayPersistenceScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// No damage means the reopen path runs and reports the state.
	if !strings.Contains(trace.String(), "openTurn=") {
		t.Fatalf("the reopen path must report the resumed state:\n%s", trace.String())
	}
}

// A scenario whose event data is present but not a message takes the flat-payload
// path, and a bad surface value is refused by the validator before replay.
func TestScenarioFlatAndInvalidSurface(t *testing.T) {
	flat := testkit.PersistenceScenario{
		ID:     "flat-payload",
		Title:  "平坦 payload 的边界事件",
		Events: []testkit.ScenarioEvent{{Type: "turn/start", Data: map[string]any{"turn": 0, "input": "x"}}},
	}
	trace, err := testkit.ReplayPersistenceScenario(flat)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !strings.Contains(trace.String(), "events=1") {
		t.Fatalf("the flat event must be stored:\n%s", trace.String())
	}
	// A scenario with no data and no message still stores an empty object.
	empty := testkit.PersistenceScenario{
		ID: "no-payload", Events: []testkit.ScenarioEvent{{Type: "turn/start"}},
	}
	if _, err := testkit.ReplayPersistenceScenario(empty); err != nil {
		t.Fatalf("an event with no fields must store an empty object: %v", err)
	}
	// An invalid surface never reaches the store: it is refused by validation.
	invalid := testkit.PersistenceScenario{
		ID: "bad-surface", Events: []testkit.ScenarioEvent{{Type: "turn/start", Surface: "invented"}},
	}
	if _, err := testkit.ReplayPersistenceScenario(invalid); err == nil {
		t.Fatal("an unknown surface must be refused before replay")
	}
	// A nested event type other than user/message is wrapped, which an assistant
	// message with no text still exercises.
	if _, err := testkit.ReplayPersistenceScenario(testkit.PersistenceScenario{
		ID:     "assistant-empty",
		Events: []testkit.ScenarioEvent{{Type: "assistant/message", Message: &testkit.ScenarioMessage{ID: "a1", Role: "assistant", Text: ""}, Surface: "append"}},
	}); err != nil {
		t.Fatalf("an assistant message with empty text must still nest: %v", err)
	}
}
