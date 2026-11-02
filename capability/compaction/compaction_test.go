package compaction

import (
	"errors"
	"testing"
)

// history builds a call/result pair surrounded by messages, which is the shape
// every pairing rule has to cope with.
func history() []Entry {
	return []Entry{
		{ID: "m1", Kind: KindMessage, Text: "please read the file"},
		{ID: "c1", Kind: KindToolCall, Text: "read_file", CallID: "call-1"},
		{ID: "r1", Kind: KindToolResult, Text: "file contents", CallID: "call-1"},
		{ID: "m2", Kind: KindMessage, Text: "thanks"},
	}
}

func TestEntryValidation(t *testing.T) {
	valid := []Entry{
		{ID: "m", Kind: KindMessage, Text: "text"},
		{ID: "s", Kind: KindSummary, Text: "summary"},
		{ID: "c", Kind: KindToolCall, CallID: "x"},
		{ID: "r", Kind: KindToolResult, CallID: "x"},
	}
	for _, entry := range valid {
		if err := entry.Validate(); err != nil {
			t.Fatalf("Validate(%+v) = %v, want nil", entry, err)
		}
	}
	invalid := []Entry{
		{Kind: KindMessage},
		{ID: "m", Kind: KindMessage, CallID: "x"},
		{ID: "c", Kind: KindToolCall},
		{ID: "c", Kind: Kind("mystery")},
	}
	for _, entry := range invalid {
		if err := entry.Validate(); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("Validate(%+v) = %v, want ErrInvalidPlan", entry, err)
		}
	}
}

func TestApplyReplacesARegionContainingAWholePair(t *testing.T) {
	entries := history()
	// [1,3) covers call and result together, which is legal.
	result, err := Apply(entries, Plan{Start: 1, End: 3, Summary: "the file was read"}, "sum-1")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !result.Applied || result.Replaced != 2 || result.SummaryID != "sum-1" {
		t.Fatalf("result = %+v, want an applied replacement of 2 entries", result)
	}
	if len(result.Entries) != 3 || result.Entries[1].Kind != KindSummary {
		t.Fatalf("entries = %+v, want the summary in the middle", result.Entries)
	}
}

func TestBoundarySplittingAPairIsRefused(t *testing.T) {
	entries := history()
	// [1,2) would take the call and leave the result behind: a dangling call.
	if _, err := Apply(entries, Plan{Start: 1, End: 2, Summary: "x"}, "sum-1"); !errors.Is(err, ErrPairSplit) {
		t.Fatalf("Apply error = %v, want ErrPairSplit", err)
	}
	// [2,3) would take the result and leave the call behind.
	if _, err := Apply(entries, Plan{Start: 2, End: 3, Summary: "x"}, "sum-1"); !errors.Is(err, ErrPairSplit) {
		t.Fatalf("Apply error = %v, want ErrPairSplit", err)
	}
}

func TestRefusedPlanChangesNothing(t *testing.T) {
	entries := history()
	result, err := Apply(entries, Plan{Start: 1, End: 2, Summary: "x"}, "sum-1")
	if err == nil {
		t.Fatal("Apply succeeded although the boundary splits a pair")
	}
	// A partial replacement is the dangling state the rule exists to prevent.
	if result.Applied {
		t.Fatal("result reports an applied replacement after a refusal")
	}
	if len(result.Entries) != len(entries) {
		t.Fatalf("entries changed: %d vs %d", len(result.Entries), len(entries))
	}
}

func TestPairingCheckReportsBothEdges(t *testing.T) {
	entries := history()
	if err := CheckPairing(entries, 1, 3); err != nil {
		t.Fatalf("CheckPairing(1,3) = %v, want nil", err)
	}
	if err := CheckPairing(entries, 1, 2); !errors.Is(err, ErrPairSplit) {
		t.Fatalf("CheckPairing(1,2) = %v, want ErrPairSplit on the start edge", err)
	}
	if err := CheckPairing(entries, 2, 3); !errors.Is(err, ErrPairSplit) {
		t.Fatalf("CheckPairing(2,3) = %v, want ErrPairSplit on the end edge", err)
	}
}

func TestPlanValidation(t *testing.T) {
	if err := (Plan{Start: -1, End: 1, Summary: "x"}).Validate(4); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("negative start = %v, want ErrInvalidPlan", err)
	}
	if err := (Plan{Start: 3, End: 4, Summary: "x"}).Validate(4); err != nil {
		t.Fatalf("legal region = %v, want nil", err)
	}
	if err := (Plan{Start: 2, End: 1, Summary: "x"}).Validate(4); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("inverted region = %v, want ErrInvalidPlan", err)
	}
	if err := (Plan{Start: 0, End: 5, Summary: "x"}).Validate(4); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("out of range = %v, want ErrInvalidPlan", err)
	}
	if err := (Plan{Start: 0, End: 1, Summary: "  "}).Validate(4); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("empty summary = %v, want ErrInvalidPlan", err)
	}
	if err := (Plan{Start: 0, End: 1, Summary: "x"}).Validate(4); err != nil {
		t.Fatalf("valid plan = %v, want nil", err)
	}
}

func TestApplyRefusesBadInput(t *testing.T) {
	entries := history()
	if _, err := Apply(entries, Plan{Start: 0, End: 2, Summary: "x"}, "  "); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("empty summary id = %v, want ErrInvalidPlan", err)
	}
	broken := []Entry{{ID: "c", Kind: KindToolCall}}
	if _, err := Apply(broken, Plan{Start: 0, End: 1, Summary: "x"}, "s"); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("invalid entry = %v, want ErrInvalidPlan", err)
	}
}

func TestLegalBandsNeverSplitAPair(t *testing.T) {
	bands := LegalBands(history())
	// Three maximal runs that never cut the pair: the leading message, the pair
	// itself, and the trailing message.
	if len(bands) != 3 {
		t.Fatalf("bands = %+v, want three bands", bands)
	}
	if bands[0].Start != 0 || bands[0].End != 1 || bands[0].Pairs != 0 {
		t.Fatalf("first band = %+v, want [0,1) with no pairs", bands[0])
	}
	if bands[1].Start != 1 || bands[1].End != 3 || bands[1].Pairs != 1 {
		t.Fatalf("second band = %+v, want the pair [1,3)", bands[1])
	}
	if bands[2].Start != 3 || bands[2].End != 4 || bands[2].Pairs != 0 {
		t.Fatalf("third band = %+v, want [3,4) with no pairs", bands[2])
	}
	// Every band must be acceptable to the pairing check.
	for _, band := range bands {
		if err := CheckPairing(history(), band.Start, band.End); err != nil {
			t.Fatalf("band %+v refused: %v", band, err)
		}
	}
}

func TestLegalBandsOfNoPairsIsOneBand(t *testing.T) {
	entries := []Entry{{ID: "m1", Kind: KindMessage}, {ID: "m2", Kind: KindMessage}}
	bands := LegalBands(entries)
	if len(bands) != 1 || bands[0].Start != 0 || bands[0].End != 2 {
		t.Fatalf("bands = %+v, want one whole-history band", bands)
	}
}

func TestApplyDetachesFromTheCallersSlice(t *testing.T) {
	entries := history()
	result, err := Apply(entries, Plan{Start: 0, End: 1, Summary: "x"}, "sum-1")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	result.Entries[0].Text = "mutated"
	again, err := Apply(entries, Plan{Start: 0, End: 1, Summary: "x"}, "sum-1")
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if again.Entries[0].Text == "mutated" {
		t.Fatal("a caller mutated the history through the returned slice")
	}
}
