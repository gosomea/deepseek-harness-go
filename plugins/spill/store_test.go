package spill

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/spill"
)

func testMeter() Meter { return Meter{Kind: MeterEstimate, TokensPerByte: 0.25} }

func TestEstimateIsDeterministicAndLabelled(t *testing.T) {
	meter := testMeter()
	content := []byte(strings.Repeat("x", 40))
	if got := meter.Estimate(content); got != 10 {
		t.Fatalf("Estimate = %d, want 10 (40 bytes at 0.25 tokens/byte)", got)
	}
	// A count without usage is labelled an estimate, never presented as measured.
	tokens, kind := meter.Count(content, nil)
	if kind != MeterEstimate || tokens != 10 {
		t.Fatalf("Count = %d/%s, want 10/estimate", tokens, kind)
	}
}

func TestUsageIsUsedAsMeasured(t *testing.T) {
	meter := testMeter()
	usage := 7
	tokens, kind := meter.Count([]byte(strings.Repeat("x", 40)), &usage)
	if kind != MeterUsage || tokens != 7 {
		t.Fatalf("Count = %d/%s, want the reported usage 7", tokens, kind)
	}
}

func TestEstimateRoundsNonEmptyContentUpToOne(t *testing.T) {
	meter := Meter{Kind: MeterEstimate, TokensPerByte: 0.25}
	// One byte times 0.25 truncates to zero; a non-empty input must still cost
	// something, or content could be included for free.
	if got := meter.Estimate([]byte("x")); got != 1 {
		t.Fatalf("Estimate(one byte) = %d, want 1", got)
	}
	if got := meter.Estimate(nil); got != 0 {
		t.Fatalf("Estimate(empty) = %d, want 0", got)
	}
}

func TestZeroRatioEstimatesNothing(t *testing.T) {
	meter := Meter{Kind: MeterEstimate}
	if got := meter.Estimate([]byte("content")); got != 0 {
		t.Fatalf("Estimate with no ratio = %d, want 0", got)
	}
}

func TestContentWithinBudgetIsIncludedVerbatim(t *testing.T) {
	budget := Budget{MaxTokens: 100, Meter: testMeter(), Store: spill.NewMemoryStore()}
	included, err := budget.Fit("small", "tool-output", []byte("short content"))
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if included.Shortened || included.Reference != "" {
		t.Fatalf("included = %+v, want verbatim content", included)
	}
	if included.Text != "short content" {
		t.Fatalf("text = %q, want the original", included.Text)
	}
}

func TestOversizedContentIsSpilledWithACitation(t *testing.T) {
	store := spill.NewMemoryStore()
	budget := Budget{MaxTokens: 40, Meter: testMeter(), Store: store}
	content := []byte(strings.Repeat("line\n", 100))
	included, err := budget.Fit("big", "tool-output", content)
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if !included.Shortened {
		t.Fatal("oversized content was not shortened")
	}
	// The citation must be present and the original must be retrievable: a bare
	// ellipsis would make discarded content look merely short.
	if included.Reference == "" || !strings.Contains(included.Text, "spilled") {
		t.Fatalf("included = %+v, want a citation naming the reference", included)
	}
	record, err := store.Resolve(included.Reference)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(record.Content) != string(content) {
		t.Fatal("the spilled original does not resolve verbatim")
	}
}

func TestShortenedContentFitsItsBudget(t *testing.T) {
	budget := Budget{MaxTokens: 40, Meter: testMeter(), Store: spill.NewMemoryStore()}
	included, err := budget.Fit("big", "tool-output", []byte(strings.Repeat("x", 1000)))
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	// The citation is reserved before choosing the prefix, so the shortened form
	// must fit the budget it was chosen for.
	if included.Tokens > budget.MaxTokens {
		t.Fatalf("shortened content costs %d tokens, budget is %d", included.Tokens, budget.MaxTokens)
	}
}

func TestOversizedContentWithoutAStoreIsRefused(t *testing.T) {
	budget := Budget{MaxTokens: 5, Meter: testMeter()}
	// A silent truncation is indistinguishable from content that was always
	// short, so a budget that cannot spill must refuse.
	if _, err := budget.Fit("big", "tool-output", []byte(strings.Repeat("x", 100))); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("Fit error = %v, want ErrOverBudget", err)
	}
}

func TestInvalidBudgetsAreRefused(t *testing.T) {
	for _, budget := range []Budget{
		{Meter: testMeter()},
		{MaxTokens: 10},
		{MaxTokens: 10, Meter: testMeter(), Store: spill.NewMemoryStore()},
	} {
		err := budget.Validate()
		if budget.MaxTokens == 10 && budget.Meter.TokensPerByte > 0 {
			if err != nil {
				t.Fatalf("valid budget rejected: %v", err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidBudget) {
			t.Fatalf("Validate(%+v) error = %v, want ErrInvalidBudget", budget, err)
		}
	}
}

func TestFitRefusesAnUnusableBudget(t *testing.T) {
	budget := Budget{MaxTokens: 0, Meter: testMeter()}
	if _, err := budget.Fit("a", "o", []byte("x")); !errors.Is(err, ErrInvalidBudget) {
		t.Fatalf("Fit error = %v, want ErrInvalidBudget", err)
	}
}

func TestRenderRefusesAShortenedEntryWithoutAReference(t *testing.T) {
	// This is the invariant that makes shortening safe: an entry that claims to
	// be shortened must name where the original went.
	_, err := Render([]Included{{ID: "a", Text: "…", Shortened: true}})
	if !errors.Is(err, ErrOverBudget) {
		t.Fatalf("Render error = %v, want ErrOverBudget", err)
	}
}

func TestRenderJoinsIncludedContent(t *testing.T) {
	text, err := Render([]Included{
		{ID: "a", Text: "first"},
		{ID: "b", Text: "second"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if text != "first"+"\n\n"+"second" {
		t.Fatalf("text = %q", text)
	}
}

func TestRenderAcceptsAShortenedEntryWithAReference(t *testing.T) {
	text, err := Render([]Included{{ID: "a", Text: "short…[ref]", Shortened: true, Reference: "a"}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(text, "ref") {
		t.Fatalf("text = %q, want the citation", text)
	}
}

func TestTotalSumsTokenCosts(t *testing.T) {
	if got := Total([]Included{{Tokens: 3}, {Tokens: 4}}); got != 7 {
		t.Fatalf("Total = %d, want 7", got)
	}
	if got := Total(nil); got != 0 {
		t.Fatalf("Total(nil) = %d, want 0", got)
	}
}

func TestCitationTooLargeForTheBudgetIsRefused(t *testing.T) {
	budget := Budget{MaxTokens: 1, Meter: testMeter(), Store: spill.NewMemoryStore()}
	// The citation alone exceeds the budget, so no prefix can be chosen.
	if _, err := budget.Fit("big", "tool-output", []byte(strings.Repeat("x", 100))); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("Fit error = %v, want ErrOverBudget", err)
	}
}

func TestSpillStoreFailureIsReported(t *testing.T) {
	budget := Budget{MaxTokens: 40, Meter: testMeter(), Store: &failingStore{}}
	// A store that refuses the spill must surface as an error, not as content
	// that claims to be retrievable.
	if _, err := budget.Fit("big", "tool-output", []byte(strings.Repeat("x", 1000))); err == nil {
		t.Fatal("Fit succeeded although the spill store refused")
	}
}

func TestRenderOfEmptyInputIsEmpty(t *testing.T) {
	text, err := Render(nil)
	if err != nil {
		t.Fatalf("Render(nil): %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
}

func TestPrefixShrinksAsTheBudgetShrinks(t *testing.T) {
	content := []byte(strings.Repeat("x", 4000))
	large := Budget{MaxTokens: 200, Meter: testMeter(), Store: spill.NewMemoryStore()}
	small := Budget{MaxTokens: 60, Meter: testMeter(), Store: spill.NewMemoryStore()}
	big, err := large.Fit("a", "o", content)
	if err != nil {
		t.Fatalf("large Fit: %v", err)
	}
	smaller, err := small.Fit("a", "o", content)
	if err != nil {
		t.Fatalf("small Fit: %v", err)
	}
	if len(smaller.Text) >= len(big.Text) {
		t.Fatalf("smaller budget kept %d bytes, larger kept %d", len(smaller.Text), len(big.Text))
	}
	// Both must still cite the original.
	if big.Reference == "" || smaller.Reference == "" {
		t.Fatal("a shortened entry cites no reference")
	}
}

// failingStore refuses every spill, standing in for a provider that is out of
// space or misconfigured.
type failingStore struct{}

func (f *failingStore) Spill(id, origin string, content []byte) (spill.Reference, error) {
	return spill.Reference{}, errors.New("spill refused")
}

func (f *failingStore) Resolve(id string) (spill.Record, error) {
	return spill.Record{}, errors.New("nothing stored")
}

func (f *failingStore) Has(id string) bool { return false }
