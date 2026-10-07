package testkit_test

import (
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// The comparator guards the parity suite, so a comparator that cannot reject a
// difference would make the whole suite vacuous. These cases assert rejection
// as well as acceptance.
func TestCompareTracesRejectsUndeclaredDifferences(t *testing.T) {
	scenario := &testkit.Scenario{ID: "guard", Steps: []testkit.Step{{Op: "settle"}}}
	if err := testkit.CompareTraces(scenario, "a\nb\n", "a\nb\n"); err != nil {
		t.Fatalf("identical traces should pass: %v", err)
	}
	err := testkit.CompareTraces(scenario, "a\nb\n", "a\nc\n")
	if err == nil || !strings.Contains(err.Error(), "undeclared difference") {
		t.Fatalf("undeclared difference must be rejected, got %v", err)
	}
	if err := testkit.CompareTraces(scenario, "a\nb\n", "a\n"); err == nil {
		t.Fatal("a shorter Go trace must be rejected")
	}
}

func TestCompareTracesAppliesOnlyUsedDivergences(t *testing.T) {
	good := &testkit.Scenario{
		ID:    "uses",
		Steps: []testkit.Step{{Op: "settle"}},
		Divergences: []testkit.Divergence{{
			Go:        []string{"go-only"},
			Reference: []string{"ref-only"},
			Reason:    "documented",
		}},
	}
	if err := testkit.CompareTraces(good, "ref-only\n", "go-only\n"); err != nil {
		t.Fatalf("declared divergence should be accepted: %v", err)
	}

	// A declaration that never occurs must fail, otherwise it could be used to
	// blanket-allow unrelated differences.
	stale := &testkit.Scenario{
		ID:    "stale",
		Steps: []testkit.Step{{Op: "settle"}},
		Divergences: []testkit.Divergence{{
			Go:        []string{"never"},
			Reference: []string{"never"},
			Reason:    "stale",
		}},
	}
	err := testkit.CompareTraces(stale, "a\n", "a\n")
	if err == nil || !strings.Contains(err.Error(), "never occurred") {
		t.Fatalf("a stale divergence must be rejected, got %v", err)
	}

	// A declaration without a reason is not reviewed evidence.
	unjustified := &testkit.Scenario{
		ID:    "noreason",
		Steps: []testkit.Step{{Op: "settle"}},
		Divergences: []testkit.Divergence{{
			Go:        []string{"go-only"},
			Reference: []string{"ref-only"},
		}},
	}
	if err := testkit.CompareTraces(unjustified, "ref-only\n", "go-only\n"); err == nil {
		t.Fatal("a divergence without a reason must be rejected")
	}
}
