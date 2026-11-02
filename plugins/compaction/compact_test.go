package compaction

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/compaction"
)

func history() []compaction.Entry {
	return []compaction.Entry{
		{ID: "m1", Kind: compaction.KindMessage, Text: "please read the file"},
		{ID: "c1", Kind: compaction.KindToolCall, Text: "read_file", CallID: "call-1"},
		{ID: "r1", Kind: compaction.KindToolResult, Text: "file contents", CallID: "call-1"},
		{ID: "m2", Kind: compaction.KindMessage, Text: "thanks"},
	}
}

func okProvider() SummaryProvider {
	return FuncProvider(func(region []compaction.Entry) (string, error) {
		return "summarized " + region[0].ID, nil
	})
}

func TestSummaryPhaseSequence(t *testing.T) {
	compactor := NewCompactor(okProvider())
	// [1,3) covers the pair, so it is legal.
	result, err := compactor.Compact(history(), compaction.Plan{Start: 1, End: 3, Summary: "ignored"})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !result.Applied {
		t.Fatal("result reports no replacement")
	}
	// start -> summary -> end, in that order.
	phases := compactor.Phases()
	if len(phases) != 3 || phases[0] != PhaseStart || phases[1] != PhaseSummary || phases[2] != PhaseEnd {
		t.Fatalf("phases = %v, want start,summary,end", phases)
	}
	if result.SummaryID == "" {
		t.Fatal("no summary id recorded")
	}
	// The provider's summary is what replaced the region, not the plan's text.
	for _, entry := range result.Entries {
		if entry.ID == result.SummaryID && !strings.Contains(entry.Text, "summarized") {
			t.Fatalf("summary text = %q, want the provider's output", entry.Text)
		}
	}
}

func TestFailedSummaryNeverReportsEnd(t *testing.T) {
	compactor := NewCompactor(FailingProvider{Reason: "route down"})
	result, err := compactor.Compact(history(), compaction.Plan{Start: 1, End: 3, Summary: "x"})
	if !errors.Is(err, ErrSummaryProviderFailed) {
		t.Fatalf("Compact error = %v, want ErrSummaryProviderFailed", err)
	}
	// The dangerous shape is a replacement reported while the summary never
	// arrived. End must not appear, and nothing may claim to be applied.
	if result.Applied {
		t.Fatal("result reports an applied replacement although the summary failed")
	}
	for _, phase := range compactor.Phases() {
		if phase == PhaseEnd {
			t.Fatalf("phases = %v, must not report end after a failed summary", compactor.Phases())
		}
	}
	if len(result.Entries) != len(history()) {
		t.Fatalf("entries changed although the summary failed")
	}
}

func TestEmptySummaryIsAFailure(t *testing.T) {
	compactor := NewCompactor(FuncProvider(func(region []compaction.Entry) (string, error) {
		return "   ", nil
	}))
	result, err := compactor.Compact(history(), compaction.Plan{Start: 1, End: 3, Summary: "x"})
	if !errors.Is(err, ErrSummaryProviderFailed) {
		t.Fatalf("Compact error = %v, want ErrSummaryProviderFailed for an empty summary", err)
	}
	if result.Applied {
		t.Fatal("an empty summary was treated as a replacement")
	}
}

func TestPairSplitIsRefusedBeforeAnyPhase(t *testing.T) {
	compactor := NewCompactor(okProvider())
	result, err := compactor.Compact(history(), compaction.Plan{Start: 1, End: 2, Summary: "x"})
	if !errors.Is(err, compaction.ErrPairSplit) {
		t.Fatalf("Compact error = %v, want ErrPairSplit", err)
	}
	// A refused plan must not look like a started compaction.
	if len(compactor.Phases()) != 0 {
		t.Fatalf("phases = %v, want none for a refused plan", compactor.Phases())
	}
	if result.Applied {
		t.Fatal("a refused plan reported a replacement")
	}
}

func TestCompactRefusesBadInput(t *testing.T) {
	var nilCompactor *Compactor
	if _, err := nilCompactor.Compact(history(), compaction.Plan{Start: 0, End: 2, Summary: "x"}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("nil Compact error = %v, want ErrNoProvider", err)
	}
	if _, err := NewCompactor(nil).Compact(history(), compaction.Plan{Start: 0, End: 2, Summary: "x"}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("providerless Compact error = %v, want ErrNoProvider", err)
	}
	compactor := NewCompactor(okProvider())
	if _, err := compactor.Compact(history(), compaction.Plan{Start: 0, End: 99, Summary: "x"}); !errors.Is(err, compaction.ErrInvalidPlan) {
		t.Fatalf("out-of-range Compact error = %v, want ErrInvalidPlan", err)
	}
	if _, err := compactor.Compact([]compaction.Entry{{ID: "c", Kind: compaction.KindToolCall}}, compaction.Plan{Start: 0, End: 1, Summary: "x"}); !errors.Is(err, compaction.ErrInvalidPlan) {
		t.Fatalf("invalid-entry Compact error = %v, want ErrInvalidPlan", err)
	}
	if nilCompactor.Steps() != nil || nilCompactor.Phases() != nil {
		t.Fatal("nil compactor reported steps")
	}
}

func TestSurfaceRecordsWhatASummaryReplaced(t *testing.T) {
	entries := []compaction.Entry{
		{ID: "sum-1", Kind: compaction.KindSummary, Text: "the file was read"},
		{ID: "m2", Kind: compaction.KindMessage, Text: "thanks"},
	}
	surface := Project(entries, map[string][]string{"sum-1": {"r1", "c1"}})
	// The citation is what lets a reader check a summary rather than trust it.
	cited := surface.Citations["sum-1"]
	if len(cited) != 2 || cited[0] != "c1" || cited[1] != "r1" {
		t.Fatalf("citations = %v, want sorted replaced ids", cited)
	}
	if len(surface.Entries) != 2 {
		t.Fatalf("entries = %+v, want the projected history", surface.Entries)
	}
}

func TestSurfaceDetachesFromTheCallersMap(t *testing.T) {
	replaced := []string{"c1", "r1"}
	surface := Project(nil, map[string][]string{"sum-1": replaced})
	replaced[0] = "mutated"
	if surface.Citations["sum-1"][0] == "mutated" {
		t.Fatal("a caller mutated the surface through the slice it passed")
	}
}

func TestSurfaceOfNoReplacements(t *testing.T) {
	surface := Project(history(), nil)
	if len(surface.Citations) != 0 {
		t.Fatalf("citations = %v, want empty", surface.Citations)
	}
	if len(surface.Entries) != len(history()) {
		t.Fatal("the surface lost entries")
	}
}

func TestSummariesAreListedInOrder(t *testing.T) {
	entries := []compaction.Entry{
		{ID: "sum-1", Kind: compaction.KindSummary},
		{ID: "m", Kind: compaction.KindMessage},
		{ID: "sum-2", Kind: compaction.KindSummary},
	}
	found := Summaries(entries)
	if len(found) != 2 || found[0].ID != "sum-1" || found[1].ID != "sum-2" {
		t.Fatalf("summaries = %+v, want both in order", found)
	}
	if Summaries(nil) != nil {
		t.Fatal("Summaries(nil) returned entries")
	}
}

func TestFailingProviderDefaultReason(t *testing.T) {
	if _, err := (FailingProvider{}).Summarize(nil); !errors.Is(err, ErrSummaryProviderFailed) {
		t.Fatalf("Summarize error = %v, want ErrSummaryProviderFailed", err)
	}
}

func TestSummaryIdsAreDistinctAcrossCompactions(t *testing.T) {
	compactor := NewCompactor(okProvider())
	first, err := compactor.Compact(history(), compaction.Plan{Start: 1, End: 3, Summary: "x"})
	if err != nil {
		t.Fatalf("first Compact: %v", err)
	}
	second, err := compactor.Compact(first.Entries, compaction.Plan{Start: 0, End: 1, Summary: "x"})
	if err != nil {
		t.Fatalf("second Compact: %v", err)
	}
	// Two compactions must not produce the same summary id, or a citation would
	// be ambiguous.
	if first.SummaryID == second.SummaryID {
		t.Fatalf("both compactions produced %q", first.SummaryID)
	}
}
