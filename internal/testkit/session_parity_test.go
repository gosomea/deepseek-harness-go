package testkit

import (
	"encoding/json"
	"github.com/gosomea/deepseek-harness-go/llm"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sessionParityDir is the shared scenario directory, relative to this package.
const sessionParityDir = "../../testdata/parity/session"

// referencePackagesEnv names the environment variable that points at the fixed
// reference checkout's `packages` directory.
const referencePackagesEnv = "DSH_PACKAGES"

// TestSessionParityMatchesRecordedReferenceTraces compares the Go session surface
// against recorded reference traces.
//
// This is the verification that "same log, same history" holds across two
// implementations rather than only within one. The scenarios, the trace format and
// the recorded traces are shared artifacts; a mismatch is a real behavioural
// difference and must be either fixed or declared in the scenario.
func TestSessionParityMatchesRecordedReferenceTraces(t *testing.T) {
	files, err := SessionScenarioFiles(filepath.Join(sessionParityDir, "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no session parity scenarios found")
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			scenario, err := LoadSessionScenario(path)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(sessionParityDir, "expected", name+".ts.txt"))
			if err != nil {
				t.Fatalf("missing recorded reference trace: %v", err)
			}
			got, err := ReplaySessionScenario(scenario)
			if err != nil {
				t.Fatalf("replay failed: %v", err)
			}
			if err := CompareSessionTraces(scenario, string(want), got); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestLiveReferenceSessionSurfaceMatchesRecorded replays the scenarios against the
// real reference build and compares them to the recorded traces.
//
// It is skipped when the fixed checkout or Node is unavailable, which is reported
// as unverified rather than as a pass: the recorded traces still prove the two
// implementations agreed when they were recorded.
func TestLiveReferenceSessionSurfaceMatchesRecorded(t *testing.T) {
	packages := os.Getenv(referencePackagesEnv)
	if packages == "" {
		t.Skipf("%s is not set; live reference verification skipped, recorded traces still used", referencePackagesEnv)
	}
	if _, err := os.Stat(filepath.Join(packages, "core", "session", "lib", "index.js")); err != nil {
		t.Skipf("fixed reference build not present: %v", err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; live reference verification skipped")
	}
	files, err := SessionScenarioFiles(filepath.Join(sessionParityDir, "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			runner := filepath.Join(sessionParityDir, "runner-ts.mjs")
			command := exec.Command(node, runner, packages, path)
			live, err := command.Output()
			if err != nil {
				var exit *exec.ExitError
				if ok := asExitError(err, &exit); ok {
					t.Fatalf("reference runner failed: %v\n%s", err, exit.Stderr)
				}
				t.Fatalf("reference runner failed: %v", err)
			}
			want, err := os.ReadFile(filepath.Join(sessionParityDir, "expected", name+".ts.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(live) != string(want) {
				t.Fatalf("recorded trace no longer matches the live reference\nrecorded:\n%s\nlive:\n%s", want, live)
			}
		})
	}
}

// asExitError reports whether err is an *exec.ExitError, filling target.
func asExitError(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}

// TestSessionScenariosAreWellFormed pins the fixture contract: every scenario has
// an id, at least one event, dense positions unless it declares a rejection, and a
// recorded reference trace. A malformed scenario would make a parity claim empty.
func TestSessionScenariosAreWellFormed(t *testing.T) {
	files, err := SessionScenarioFiles(filepath.Join(sessionParityDir, "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no session parity scenarios found")
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		scenario, err := LoadSessionScenario(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if scenario.ID != name {
			t.Fatalf("%s: id %q does not match the file name", name, scenario.ID)
		}
		if scenario.Title == "" {
			t.Fatalf("%s: scenario needs a title", name)
		}
		if !scenario.ExpectFoldError {
			for index, event := range scenario.Events {
				if event.Seq != uint64(index) {
					t.Fatalf("%s: event %d has seq %d; a foldable scenario must be dense from 0",
						name, index, event.Seq)
				}
			}
		}
		for index, divergence := range scenario.Divergences {
			if len(divergence.Go) == 0 || len(divergence.Reference) == 0 || divergence.Reason == "" {
				t.Fatalf("%s: divergence %d needs both sides and a reason", name, index+1)
			}
		}
		if _, err := os.Stat(filepath.Join(sessionParityDir, "expected", name+".ts.txt")); err != nil {
			t.Fatalf("%s: no recorded reference trace: %v", name, err)
		}
	}
}

// TestSessionScenarioDecoderRejectsIncompleteFixtures pins that the loader refuses a
// fixture that could not describe a behaviour.
func TestSessionScenarioDecoderRejectsIncompleteFixtures(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"no-events.json": `{"id":"no-events","title":"x","events":[]}`,
		"no-id.json":     `{"title":"x","events":[{"type":"turn/start","seq":0}]}`,
		"not-json.json":  `{`,
		"sparse.json":    `{"id":"sparse","title":"x","events":[{"type":"turn/start","seq":3}]}`,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSessionScenario(path); err == nil {
			t.Fatalf("%s: LoadSessionScenario() = nil, want an error", name)
		}
	}
	if _, err := LoadSessionScenario(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("LoadSessionScenario(missing) = nil, want an error")
	}
	// A malformed surface op decodes but is refused when the scenario is replayed:
	// the loader checks structure, and the fold checks meaning.
	path := filepath.Join(dir, "live-bad-surface.json")
	if err := os.WriteFile(path, []byte(`{"id":"live-bad-surface","title":"x","events":[
		{"type":"user/message","seq":0,"data":{"x":1},"surfaceOp":{"op":7}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	scenario, err := LoadSessionScenario(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReplaySessionScenario(scenario); err == nil {
		t.Fatal("ReplaySessionScenario() = nil, want the malformed surface op refused")
	}
}

// TestSessionParityScenarioFilesAreSorted pins the stable order the parity loop
// relies on, so a failure report is reproducible.
func TestSessionParityScenarioFilesAreSorted(t *testing.T) {
	files, err := SessionScenarioFiles(filepath.Join(sessionParityDir, "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(files); index++ {
		if files[index-1] >= files[index] {
			t.Fatalf("files are not sorted: %q then %q", files[index-1], files[index])
		}
	}
	// A directory with no scenarios yields an empty list rather than an error.
	empty, err := SessionScenarioFiles(t.TempDir())
	if err != nil || len(empty) != 0 {
		t.Fatalf("SessionScenarioFiles(empty) = %v, %v, want an empty list", empty, err)
	}
}

// TestSessionScenarioDecodingCoversSurfaceOps pins the two surface encodings the
// fixtures use, so a scenario cannot silently lose its placement.
func TestSessionScenarioDecodingCoversSurfaceOps(t *testing.T) {
	appendScenario := SessionScenarioEvent{Type: "user/message", Seq: 0, SurfaceOp: json.RawMessage(`"append"`)}
	event, err := toSessionEvent(appendScenario)
	if err != nil {
		t.Fatalf("toSessionEvent(append) error = %v", err)
	}
	if event.SurfaceOp == nil || !event.SurfaceOp.IsAppend() {
		t.Fatalf("SurfaceOp = %+v, want an append", event.SurfaceOp)
	}
	replaceScenario := SessionScenarioEvent{Type: "user/message", Seq: 4,
		SurfaceOp:       json.RawMessage(`{"op":"replace","startSeq":0,"endSeq":2}`),
		SourceEventSeqs: []uint64{0, 2}}
	event, err = toSessionEvent(replaceScenario)
	if err != nil {
		t.Fatalf("toSessionEvent(replace) error = %v", err)
	}
	if event.SurfaceOp == nil || !event.SurfaceOp.IsReplace() {
		t.Fatalf("SurfaceOp = %+v, want a replacement", event.SurfaceOp)
	}
	if event.SurfaceOp.StartSeq != 0 || event.SurfaceOp.EndSeq != 2 {
		t.Fatalf("SurfaceOp = %+v, want the declared range", event.SurfaceOp)
	}
	if len(event.SourceEventSeqs) != 2 {
		t.Fatalf("SourceEventSeqs = %v, want both citations", event.SourceEventSeqs)
	}
	// An unknown operation survives decoding and is refused by the fold.
	unknown := SessionScenarioEvent{Type: "user/message", Seq: 0, Data: json.RawMessage(`{}`),
		SurfaceOp: json.RawMessage(`{"op":"truncate","startSeq":0,"endSeq":0}`)}
	event, err = toSessionEvent(unknown)
	if err != nil {
		t.Fatalf("toSessionEvent(unknown) error = %v", err)
	}
	if event.SurfaceOp == nil || event.SurfaceOp.Op != "truncate" {
		t.Fatalf("SurfaceOp = %+v, want the unknown operation preserved for the fold to refuse", event.SurfaceOp)
	}
}

// TestContentKindsKeepVocabularyVisible pins that the trace records block kinds, so
// a change in content vocabulary shows up rather than hiding behind equal text.
func TestContentKindsKeepVocabularyVisible(t *testing.T) {
	message, err := llm.NewAssistantMessage("fake", "fake-1", []llm.ContentBlock{
		llm.ReasoningBlock{Text: "think"},
		llm.TextBlock{Text: "visible"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := contentText(message.Content); got != "visible" {
		t.Fatalf("contentText() = %q, want only visible text", got)
	}
	kinds := contentKinds(message.Content)
	if strings.Join(kinds, ",") != "reasoning,text" {
		t.Fatalf("contentKinds() = %v, want both kinds in order", kinds)
	}
	if contentText(nil) != "" {
		t.Fatalf("contentText(nil) = %q, want empty", contentText(nil))
	}
	if quoteText("a\"b") != `"a\"b"` {
		t.Fatalf("quoteText() = %s, want an escaped quoted string", quoteText("a\"b"))
	}
	if formatSeqs(nil) != "[]" || formatUint(7) != "7" {
		t.Fatal("format helpers did not render the expected shape")
	}
}
