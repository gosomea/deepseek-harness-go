package testkit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// A divergence whose Go block is a strict prefix of another declaration must
// still match the correct one; the comparator tries declarations in order and
// must not settle for a partial match.
func TestCompareTracesMatchesLongestApplicableBlock(t *testing.T) {
	long := &testkit.Scenario{
		ID:    "overlap-long",
		Steps: []testkit.Step{{Op: "settle"}},
		Divergences: []testkit.Divergence{
			{Go: []string{"g1", "g2"}, Reference: []string{"r1", "r2"}, Reason: "long"},
		},
	}
	if err := testkit.CompareTraces(long, "r1\nr2\n", "g1\ng2\n"); err != nil {
		t.Fatalf("expected the two-line declaration to apply: %v", err)
	}

	// A shorter declaration must not consume part of a longer block when both
	// could start here; only the longer one leaves the trace consistent.
	both := &testkit.Scenario{
		ID:    "overlap",
		Steps: []testkit.Step{{Op: "settle"}},
		Divergences: []testkit.Divergence{
			{Go: []string{"g1"}, Reference: []string{"r1"}, Reason: "short"},
			{Go: []string{"g1", "g2"}, Reference: []string{"r1", "r2"}, Reason: "long"},
		},
	}
	// The short declaration remains unused, which the comparator must report.
	err := testkit.CompareTraces(both, "r1\nr2\n", "g1\ng2\n")
	if err == nil || !strings.Contains(err.Error(), "never occurred") {
		t.Fatalf("the unused short declaration must be reported, got %v", err)
	}

	// Where only the short block fits, the long declaration must not be forced.
	short := &testkit.Scenario{
		ID:    "overlap-short",
		Steps: []testkit.Step{{Op: "settle"}},
		Divergences: []testkit.Divergence{
			{Go: []string{"g1"}, Reference: []string{"r1"}, Reason: "short"},
		},
	}
	if err := testkit.CompareTraces(short, "r1\ntail\n", "g1\ntail\n"); err != nil {
		t.Fatalf("single-line declaration should apply: %v", err)
	}
}

// A scenario with a settle step that produces no error must not emit a
// spurious error line; only a failing teardown reports one.
func TestSettleWithoutFailureIsSilent(t *testing.T) {
	scenario := &testkit.Scenario{ID: "clean", Steps: []testkit.Step{
		{Op: "register", As: "x", Name: "x", Cleanups: []string{"one"}},
		{Op: "settle"},
		{Op: "observe", Note: "done"},
	}}
	trace, err := testkit.Replay(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(trace, "cleanup-failed") {
		t.Fatalf("a clean teardown must not report a cleanup failure:\n%s", trace)
	}
}

// A validator that panics on a non-object config is reported, not swallowed.
func TestValidateParityConfigRejectsNonObject(t *testing.T) {
	scenario := &testkit.Scenario{ID: "scalar-validator", Steps: []testkit.Step{
		{Op: "register", As: "x", Name: "x", Validate: true, Config: []byte(`"scalar"`)},
	}}
	if _, err := testkit.Replay(scenario); err == nil {
		t.Fatal("a validator must reject a non-object config")
	}
}

// ScenarioFiles must report an unreadable directory rather than an empty list,
// so a misconfigured test cannot silently compare nothing.
func TestScenarioFilesReportsMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	files, err := testkit.ScenarioFiles(filepath.Join(dir, "absent"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("a missing directory should yield no scenarios, got %v", files)
	}
	if _, err := testkit.LoadScenario(filepath.Join(dir, "absent", "x.json")); err == nil {
		t.Fatal("loading from a missing directory must fail")
	}
}

// A closed root must render its nodes as disposed, exercising the state
// rendering used by every observe step.
func TestObserveRendersDisposedState(t *testing.T) {
	scenario := &testkit.Scenario{ID: "closed", Steps: []testkit.Step{
		{Op: "register", As: "x", Name: "x"},
		{Op: "close"},
		{Op: "observe", Note: "afterClose"},
	}}
	trace, err := testkit.Replay(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trace, "state x=disposed") || !strings.Contains(trace, "close ok") {
		t.Fatalf("closing must dispose nodes, got:\n%s", trace)
	}
}

func TestReplayRejectsMalformedConfigJSON(t *testing.T) {
	scenario := &testkit.Scenario{ID: "bad-json-config", Steps: []testkit.Step{
		{Op: "register", As: "x", Name: "x", Config: []byte("{")},
	}}
	if _, err := testkit.Replay(scenario); err == nil {
		t.Fatal("malformed config JSON must fail the replay")
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "x")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
