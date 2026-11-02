package testkit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// cliScenarioDir locates the CLI fixtures relative to this package.
func cliScenarioDir() string {
	return filepath.Join("..", "..", "testdata", "parity", "cli", "scenarios")
}

// Every fixture on disk must be well formed.
func TestCLIScenariosAreWellFormed(t *testing.T) {
	entries, err := os.ReadDir(cliScenarioDir())
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no CLI scenarios found")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		scenario, err := testkit.LoadCLIScenario(filepath.Join(cliScenarioDir(), entry.Name()))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if scenario.ID == "" || scenario.Title == "" {
			t.Fatalf("%s: scenario needs an id and a title", entry.Name())
		}
	}
}

// The four fixtures cover the behaviours the M7 gate names.
func TestCLIScenariosCoverTheNamedBehaviours(t *testing.T) {
	want := map[string]bool{
		"read-write-refuse":     false,
		"headless-no-responder": false,
		"read-only-command":     false,
		"exit-cleanup":          false,
	}
	entries, err := os.ReadDir(cliScenarioDir())
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		scenario, err := testkit.LoadCLIScenario(filepath.Join(cliScenarioDir(), entry.Name()))
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
func TestCLIScenarioReplayIsDeterministic(t *testing.T) {
	for _, name := range []string{"read-write-refuse", "headless-no-responder", "read-only-command", "exit-cleanup"} {
		scenario, err := testkit.LoadCLIScenario(filepath.Join(cliScenarioDir(), name+".json"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		first, err := testkit.ReplayCLIScenario(scenario)
		if err != nil {
			t.Fatalf("%s first replay: %v", name, err)
		}
		second, err := testkit.ReplayCLIScenario(scenario)
		if err != nil {
			t.Fatalf("%s second replay: %v", name, err)
		}
		if first.String() != second.String() {
			t.Fatalf("%s replay is not deterministic:\n%s\n---\n%s", name, first.String(), second.String())
		}
	}
}

// The refusal scenario is the point of the whole gate: the denied write must not
// change the file, and the allowed write must.
func TestReadWriteRefuseScenario(t *testing.T) {
	scenario, err := testkit.LoadCLIScenario(filepath.Join(cliScenarioDir(), "read-write-refuse.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayCLIScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	for _, want := range []string{
		"readsSucceed=1 deniedWrites=1 allowedWrites=1",
		"fileAfterDenial=true",
		"fileAfterAllowance=true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("trace missing %q:\n%s", want, text)
		}
	}
	// The denied WRITE must appear before the allowed WRITE. The assertion names
	// the tool rather than matching "denied=false", because a read is admitted
	// without approval and would otherwise look like the first allowance.
	deniedWrite := strings.Index(text, "tool[2]=edit_file callId=call-2 outcome=rejected denied=true")
	allowedWrite := strings.Index(text, "tool[3]=edit_file callId=call-3 outcome=allowed-once denied=false")
	if deniedWrite < 0 || allowedWrite < 0 || deniedWrite > allowedWrite {
		t.Fatalf("the denied write must precede the allowed write:\n%s", text)
	}
	// A read is admitted without approval, which is the reason the assertion above
	// cannot simply look for the first "denied=false".
	if !strings.Contains(text, "tool[1]=read_file callId=call-1 outcome=rejected denied=false") {
		t.Fatalf("a read must not be denied:\n%s", text)
	}
}

// A headless run with no responder must not authorize anything.
func TestHeadlessNoResponderScenario(t *testing.T) {
	scenario, err := testkit.LoadCLIScenario(filepath.Join(cliScenarioDir(), "headless-no-responder.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayCLIScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	if !strings.Contains(text, "deniedWrites=1") || !strings.Contains(text, "allowedWrites=0") {
		t.Fatalf("no responder must not authorize:\n%s", text)
	}
	if !strings.Contains(text, "fileAfterDenial=true") {
		t.Fatalf("the file must be unchanged:\n%s", text)
	}
}

// A read-only policy refuses a command and names the mode it needed.
func TestReadOnlyCommandScenario(t *testing.T) {
	scenario, err := testkit.LoadCLIScenario(filepath.Join(cliScenarioDir(), "read-only-command.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayCLIScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	if !strings.Contains(text, "commandDenied=1") {
		t.Fatalf("the command must be denied under read-only:\n%s", text)
	}
	// The refusal names the wider mode, so an operator knows what to change.
	if !strings.Contains(text, "denialNamesRequiredMode=true") {
		t.Fatalf("the denial must name the required mode:\n%s", text)
	}
}

// The cleanup scenario shows a released session can be reopened.
func TestExitCleanupScenario(t *testing.T) {
	scenario, err := testkit.LoadCLIScenario(filepath.Join(cliScenarioDir(), "exit-cleanup.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trace, err := testkit.ReplayCLIScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !strings.Contains(trace.String(), "secondRunCanOpen=true") {
		t.Fatalf("a released world must be reopenable:\n%s", trace.String())
	}
}

// A scenario that cannot be replayed is refused rather than half-executed.
func TestCLIScenarioValidation(t *testing.T) {
	if _, err := testkit.LoadCLIScenario(filepath.Join(cliScenarioDir(), "absent.json")); err == nil {
		t.Fatal("a missing fixture must be an error")
	}
	cases := []testkit.CLIScenario{
		{},
		{ID: "no-steps"},
		{ID: "bad-kind", Steps: []testkit.CLIStep{{Kind: "invented"}}},
		{ID: "seed-no-path", Steps: []testkit.CLIStep{{Kind: "seed"}}},
		{ID: "tool-no-name", Steps: []testkit.CLIStep{{Kind: "tool"}}},
		{ID: "bad-outcome", Steps: []testkit.CLIStep{{Kind: "tool", Name: "read_file", Outcome: "maybe"}}},
		{ID: "bad-policy", Steps: []testkit.CLIStep{{Kind: "tool", Name: "read_file"}}, Policy: "invented"},
	}
	for _, scenario := range cases {
		if err := scenario.Validate(); err == nil {
			t.Fatalf("scenario %q must be refused", scenario.ID)
		}
	}
}

// The fixture directory helper resolves under the repository root.
func TestCLIScenarioDir(t *testing.T) {
	if got := testkit.CLIScenarioDir("/repo"); !strings.HasSuffix(got, filepath.Join("testdata", "parity", "cli", "scenarios")) {
		t.Fatalf("unexpected path %q", got)
	}
}

// A scenario whose steps a test builds directly exercises the paths the fixtures
// do not: a cancelled approval, a read of a missing file, and a long content
// line that has to be clipped in the trace.
func TestCLIScenarioEdgePaths(t *testing.T) {
	// A cancelled approval denies without writing.
	cancelled := testkit.CLIScenario{
		ID:    "cancelled-approval",
		Title: "取消的审批不授权",
		Steps: []testkit.CLIStep{
			{Kind: "seed", Path: "notes.txt", Content: "original"},
			{Kind: "tool", Name: "edit_file", CallID: "call-1",
				Arguments: map[string]any{"path": "notes.txt", "content": "changed"}, Outcome: "cancelled"},
		},
		Expect: testkit.CLIExpectation{DeniedWrites: 1, FileAfterDenial: "original"},
	}
	trace, err := testkit.ReplayCLIScenario(cancelled)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !strings.Contains(trace.String(), "deniedWrites=1") {
		t.Fatalf("a cancelled approval must deny:\n%s", trace.String())
	}
	if !strings.Contains(trace.String(), "fileAfterDenial=true") {
		t.Fatalf("the file must be unchanged:\n%s", trace.String())
	}

	// A read of a missing file is an error, and the scenario reports it.
	missing := testkit.CLIScenario{
		ID:    "missing-read",
		Title: "读取不存在的文件",
		Steps: []testkit.CLIStep{
			{Kind: "tool", Name: "read_file", CallID: "call-1", Arguments: map[string]any{"path": "absent.txt"}},
		},
	}
	if _, err := testkit.ReplayCLIScenario(missing); err == nil {
		t.Fatal("reading a missing file must be reported")
	}

	// A long answer is clipped in the trace, so a trace stays readable.
	long := testkit.CLIScenario{
		ID:    "long-content",
		Title: "长内容在 trace 中被裁剪",
		Steps: []testkit.CLIStep{
			{Kind: "seed", Path: "long.txt", Content: strings.Repeat("x", 200)},
			{Kind: "tool", Name: "read_file", CallID: "call-1", Arguments: map[string]any{"path": "long.txt"}},
		},
		Expect: testkit.CLIExpectation{ReadsSucceed: 1},
	}
	longTrace, err := testkit.ReplayCLIScenario(long)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !strings.Contains(longTrace.String(), "…") {
		t.Fatalf("long content must be clipped:\n%s", longTrace.String())
	}
}

// A scenario that names no path for a denial-time check is refused rather than
// silently skipping the assertion.
func TestCLIScenarioDenialCheckNeedsAPath(t *testing.T) {
	scenario := testkit.CLIScenario{
		ID:    "denial-without-path",
		Title: "拒绝检查缺少路径",
		Steps: []testkit.CLIStep{
			{Kind: "tool", Name: "edit_file", CallID: "call-1", Arguments: map[string]any{}, Outcome: "rejected"},
		},
		Expect: testkit.CLIExpectation{FileAfterDenial: "anything"},
	}
	// Without a path the step is refused by argument validation, which is the
	// honest outcome: the assertion could not have been evaluated.
	if _, err := testkit.ReplayCLIScenario(scenario); err == nil {
		t.Fatal("a denial check without a path must be reported")
	}
}

// A scenario exercising the environment-backed history assertion, a second run,
// and a policy default takes the remaining branches.
func TestCLIScenarioHistoryAndSecondRun(t *testing.T) {
	history := 2
	scenario := testkit.CLIScenario{
		ID:    "history-and-second-run",
		Title: "重开与历史计数",
		Steps: []testkit.CLIStep{
			{Kind: "seed", Path: "notes.txt", Content: "x"},
		},
		Expect: testkit.CLIExpectation{SecondRunCanOpen: true, HistoryMessages: &history},
	}
	trace, err := testkit.ReplayCLIScenario(scenario)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	text := trace.String()
	if !strings.Contains(text, "secondRunCanOpen=true") || !strings.Contains(text, "historyMessages=2") {
		t.Fatalf("unexpected trace:\n%s", text)
	}
}
