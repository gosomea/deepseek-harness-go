package testkit_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// repoRoot resolves the repository root from this test's working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func scenarioDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "parity", "cordis", "scenarios")
}

func expectedDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "parity", "cordis", "expected")
}

// TestReplayMatchesRecordedReferenceTrace pins the Go trace to traces captured
// from the fixed TypeScript reference. The recorded files are produced by
// runner-ts.mjs against the pinned build; regenerate and review them when the
// reference commit changes.
func TestReplayMatchesRecordedReferenceTrace(t *testing.T) {
	files, err := testkit.ScenarioFiles(scenarioDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no scenarios found")
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			scenario, err := testkit.LoadScenario(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := testkit.Replay(scenario)
			if err != nil {
				t.Fatal(err)
			}
			recorded, err := os.ReadFile(filepath.Join(expectedDir(t), name+".ts.txt"))
			if err != nil {
				t.Fatalf("missing recorded reference trace: %v", err)
			}
			if err := testkit.CompareTraces(scenario, string(recorded), got); err != nil {
				t.Errorf("trace diverges from the reference\n--- go\n%s\n--- reference\n%s\n%v",
					got, recorded, err)
			}
		})
	}
}

// TestLiveReferenceTraceMatchesRecorded checks the recorded traces still match
// what the reference actually produces. It is skipped when the pinned Cordis
// build or Node is unavailable, because neither ships with this repository.
func TestLiveReferenceTraceMatchesRecorded(t *testing.T) {
	lib := os.Getenv("CORDIS_REFERENCE_LIB")
	if lib == "" {
		t.Skip("set CORDIS_REFERENCE_LIB to the pinned vendor/cordis/lib/index.js to compare live")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("node unavailable: %v", err)
	}
	runner := filepath.Join(repoRoot(t), "testdata", "parity", "cordis", "runner-ts.mjs")
	for _, path := range mustScenarios(t) {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "node", runner, lib, path)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Run(); err != nil {
				t.Fatalf("reference runner failed: %v\n%s", err, stderr.String())
			}
			recorded, err := os.ReadFile(filepath.Join(expectedDir(t), name+".ts.txt"))
			if err != nil {
				t.Fatalf("missing recorded reference trace: %v", err)
			}
			if stdout.String() != string(recorded) {
				t.Errorf("recorded trace is stale for %s\n--- live\n%s\n--- recorded\n%s",
					name, stdout.String(), recorded)
			}
		})
	}
}

func mustScenarios(t *testing.T) []string {
	t.Helper()
	files, err := testkit.ScenarioFiles(scenarioDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return files
}
