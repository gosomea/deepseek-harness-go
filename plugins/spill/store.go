package spill

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gosomea/deepseek-harness-go/capability/spill"
)

// Errors this package returns.
var (
	// ErrOverBudget reports content that cannot be fitted and cannot be spilled.
	ErrOverBudget = errors.New("spill: content does not fit the budget")
	// ErrNoStore reports a budget that has nowhere to spill.
	ErrNoStore = errors.New("spill: no spill store configured")
	// ErrInvalidBudget reports a budget that cannot be used.
	ErrInvalidBudget = errors.New("spill: invalid budget")
)

// MeterKind names where a token count came from.
//
// The kind is recorded rather than inferred because "this was measured by the
// provider" and "this was estimated here" carry different trust, and a report
// that lost the distinction would overstate what is known.
type MeterKind string

// The meter kinds this package distinguishes.
const (
	// MeterUsage is a count the provider reported.
	MeterUsage MeterKind = "usage"
	// MeterEstimate is a count this build computed.
	MeterEstimate MeterKind = "estimate"
)

// Meter counts tokens, and says how.
type Meter struct {
	// Kind is where counts come from.
	Kind MeterKind
	// TokensPerByte is the estimator's ratio, used when Kind is MeterEstimate.
	TokensPerByte float64
}

// Estimate returns a deterministic token estimate for content.
//
// The estimator is deliberately simple and stated as a ratio rather than a model
// tokenizer: a test can then assert an exact number, and the error bound is
// something a reader can reason about. It never claims to be a measurement.
func (m Meter) Estimate(content []byte) int {
	if m.TokensPerByte <= 0 {
		return 0
	}
	tokens := int(float64(len(content)) * m.TokensPerByte)
	if tokens == 0 && len(content) > 0 {
		tokens = 1
	}
	return tokens
}

// Count returns the token count for content, labelled with its source.
//
// A usage count is used as-is when supplied; otherwise the estimate is returned
// and labelled as an estimate. The label is the point: the number alone cannot
// tell a reader which it is.
func (m Meter) Count(content []byte, usage *int) (int, MeterKind) {
	if usage != nil {
		return *usage, MeterUsage
	}
	return m.Estimate(content), MeterEstimate
}

// Budget is a per-request content budget.
type Budget struct {
	// MaxTokens is the ceiling for included content. Required and positive.
	MaxTokens int
	// Meter counts the content.
	Meter Meter
	// Store receives spilled content. Required when content can overflow.
	Store spill.Store
}

// Validate reports whether a budget can be used.
func (b Budget) Validate() error {
	if b.MaxTokens <= 0 {
		return fmt.Errorf("%w: MaxTokens must be positive", ErrInvalidBudget)
	}
	if b.Meter.TokensPerByte <= 0 {
		return fmt.Errorf("%w: the meter has no ratio", ErrInvalidBudget)
	}
	return nil
}

// Included is one piece of content after a budget decision.
type Included struct {
	// ID names the content.
	ID string
	// Text is what the request carries. For shortened content it is the prefix
	// plus a citation line, never a bare ellipsis.
	Text string
	// Tokens is the estimated cost of Text.
	Tokens int
	// Shortened reports whether the content was reduced.
	Shortened bool
	// Reference finds the original, when content was spilled. Empty otherwise.
	Reference string
}

// Fit includes content under a budget, spilling what does not fit.
//
// The result always says whether content was shortened and, if so, where the
// original went. A budget that cannot spill and cannot fit refuses rather than
// silently truncating, because a silent truncation is indistinguishable from
// content that was always that short.
func (b Budget) Fit(id, origin string, content []byte) (Included, error) {
	if err := b.Validate(); err != nil {
		return Included{}, err
	}
	total := b.Meter.Estimate(content)
	if total <= b.MaxTokens {
		return Included{ID: id, Text: string(content), Tokens: total}, nil
	}
	if b.Store == nil {
		return Included{}, fmt.Errorf("%w: %q needs %d tokens, budget is %d, and no store is configured",
			ErrOverBudget, id, total, b.MaxTokens)
	}
	// The citation itself costs tokens, so it is reserved before choosing a
	// prefix length. Reserving afterwards would let the shortened form exceed the
	// budget it was chosen for.
	citation := fmt.Sprintf("[spilled %s: original %d bytes, reference %s]", id, len(content), id)
	citationTokens := b.Meter.Estimate([]byte(citation))
	prefixBudget := b.MaxTokens - citationTokens
	if prefixBudget < 0 {
		return Included{}, fmt.Errorf("%w: %q cannot fit even the citation in %d tokens",
			ErrOverBudget, id, b.MaxTokens)
	}
	// Work out how many bytes fit, then cut on that boundary.
	keep := 0
	if b.Meter.TokensPerByte > 0 {
		keep = int(float64(prefixBudget) / b.Meter.TokensPerByte)
	}
	if keep > len(content) {
		keep = len(content)
	}
	if keep < 0 {
		keep = 0
	}
	reference, err := b.Store.Spill(id, origin, content)
	if err != nil {
		return Included{}, err
	}
	text := string(content[:keep]) + "\n" + citation
	return Included{
		ID:        id,
		Text:      text,
		Tokens:    b.Meter.Estimate([]byte(text)),
		Shortened: true,
		Reference: reference.ID,
	}, nil
}

// Render joins included content into request text.
//
// It refuses to render a shortened entry without a reference: that combination
// would present discarded content as if it were merely short. The check is here
// rather than in Fit so a caller that assembled entries by hand is also caught.
func Render(included []Included) (string, error) {
	var builder strings.Builder
	for index, item := range included {
		if item.Shortened && strings.TrimSpace(item.Reference) == "" {
			return "", fmt.Errorf("%w: %q is shortened but cites no reference", ErrOverBudget, item.ID)
		}
		if index > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(item.Text)
	}
	return builder.String(), nil
}

// Total returns the summed token cost of included content.
func Total(included []Included) int {
	sum := 0
	for _, item := range included {
		sum += item.Tokens
	}
	return sum
}
