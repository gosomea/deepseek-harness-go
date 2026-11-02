package compaction

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gosomea/deepseek-harness-go/capability/compaction"
)

// Errors this package returns.
var (
	// ErrSummaryProviderFailed reports a summary the provider could not produce.
	ErrSummaryProviderFailed = errors.New("compaction: summary provider failed")
	// ErrNoProvider reports a compactor without a summary provider.
	ErrNoProvider = errors.New("compaction: no summary provider")
	// ErrAlreadyStarted reports a second start on one compactor.
	ErrAlreadyStarted = errors.New("compaction: a compaction is already in progress")
)

// Phase names one observable step of a compaction.
type Phase string

// The phases this package reports, in order.
const (
	// PhaseStart is reported before the summary is requested.
	PhaseStart Phase = "start"
	// PhaseSummary is reported when the summary arrives or fails.
	PhaseSummary Phase = "summary"
	// PhaseEnd is reported only when the replacement finished.
	PhaseEnd Phase = "end"
)

// Step is one reported phase.
//
// It exists so a caller can observe the flow rather than infer it: "the summary
// failed" and "the replacement failed" are different places in the sequence.
type Step struct {
	// Phase is the step's phase.
	Phase Phase
	// Detail explains the step for diagnostics.
	Detail string
	// Err is the failure that ended the sequence, when there was one.
	Err error
}

// SummaryProvider produces a summary of a region.
//
// It is an interface rather than a concrete model call so a test can supply a
// provider that fails, and so the real route can be swapped without the flow
// changing.
type SummaryProvider interface {
	// Summarize returns a summary of the region, or an error.
	Summarize(region []compaction.Entry) (string, error)
}

// FuncProvider adapts a function to SummaryProvider.
type FuncProvider func(region []compaction.Entry) (string, error)

// Summarize returns a summary of the region.
func (f FuncProvider) Summarize(region []compaction.Entry) (string, error) { return f(region) }

// FailingProvider always fails, for tests and for a route that is not configured.
type FailingProvider struct {
	// Reason explains the failure.
	Reason string
}

// Summarize always returns an error.
func (f FailingProvider) Summarize(region []compaction.Entry) (string, error) {
	reason := f.Reason
	if reason == "" {
		reason = "no route configured"
	}
	return "", fmt.Errorf("%w: %s", ErrSummaryProviderFailed, reason)
}

// Compactor applies plans through a provider and reports each phase.
type Compactor struct {
	provider SummaryProvider
	steps    []Step
	started  bool
	nextID   int
}

// NewCompactor returns a compactor over a provider.
func NewCompactor(provider SummaryProvider) *Compactor {
	return &Compactor{provider: provider}
}

// Steps returns the phases reported so far, in order.
func (c *Compactor) Steps() []Step {
	if c == nil {
		return nil
	}
	out := make([]Step, len(c.steps))
	copy(out, c.steps)
	return out
}

// Phases returns the reported phase names, in order.
func (c *Compactor) Phases() []Phase {
	if c == nil {
		return nil
	}
	out := make([]Phase, 0, len(c.steps))
	for _, step := range c.steps {
		out = append(out, step.Phase)
	}
	return out
}

// Compact replaces a region with a summary produced by the provider.
//
// The sequence is start, summary, end. A provider failure reports start and
// summary and stops: end is never reported, the original entries come back
// unchanged, and Applied is false. That is the whole point — a caller cannot
// mistake a failed summary for a finished replacement.
//
// A boundary that would separate a tool call from its result is refused before
// any phase is reported, so the pair rule is enforced here rather than left to
// whichever region a caller happened to pick.
func (c *Compactor) Compact(entries []compaction.Entry, plan compaction.Plan) (compaction.Result, error) {
	if c == nil || c.provider == nil {
		return compaction.Result{}, fmt.Errorf("%w", ErrNoProvider)
	}
	for _, entry := range entries {
		if err := entry.Validate(); err != nil {
			return compaction.Result{}, err
		}
	}
	if err := plan.Validate(len(entries)); err != nil {
		return compaction.Result{}, err
	}
	// The pairing rule is checked before any phase is reported, so a refused plan
	// never looks like a started compaction.
	if err := compaction.CheckPairing(entries, plan.Start, plan.End); err != nil {
		return compaction.Result{Entries: append([]compaction.Entry(nil), entries...), Applied: false}, err
	}
	c.steps = append(c.steps, Step{Phase: PhaseStart, Detail: fmt.Sprintf("region [%d,%d)", plan.Start, plan.End)})

	region := append([]compaction.Entry(nil), entries[plan.Start:plan.End]...)
	summary, err := c.provider.Summarize(region)
	if err != nil {
		c.steps = append(c.steps, Step{Phase: PhaseSummary, Detail: "summary failed", Err: err})
		// The replacement did not happen. Returning the originals with Applied
		// false is the only honest answer.
		return compaction.Result{Entries: append([]compaction.Entry(nil), entries...), Applied: false}, err
	}
	if strings.TrimSpace(summary) == "" {
		emptyErr := fmt.Errorf("%w: the provider returned an empty summary", ErrSummaryProviderFailed)
		c.steps = append(c.steps, Step{Phase: PhaseSummary, Detail: "empty summary", Err: emptyErr})
		return compaction.Result{Entries: append([]compaction.Entry(nil), entries...), Applied: false}, emptyErr
	}
	c.steps = append(c.steps, Step{Phase: PhaseSummary, Detail: fmt.Sprintf("%d bytes", len(summary))})

	c.nextID++
	summaryID := fmt.Sprintf("summary-%d", c.nextID)
	result, err := compaction.Apply(entries, compaction.Plan{
		Start: plan.Start, End: plan.End, Summary: summary,
	}, summaryID)
	if err != nil {
		// The summary exists but the replacement did not: report only the phases
		// that actually happened, so end is not claimed.
		return compaction.Result{Entries: append([]compaction.Entry(nil), entries...), Applied: false}, err
	}
	c.steps = append(c.steps, Step{Phase: PhaseEnd, Detail: summaryID})
	return result, nil
}

// Surface is the request-visible view of a history.
type Surface struct {
	// Entries are what a request would carry.
	Entries []compaction.Entry
	// Citations map a summary id to the entries it stands for, so a reader can
	// see what a summary replaced rather than taking it on trust.
	Citations map[string][]string
}

// Project builds the request surface, recording what each summary replaced.
//
// The citation is recorded rather than inferred because "which entries does this
// summary stand for" is exactly what a reader needs when a summary looks wrong,
// and reconstructing it from the shortened history is impossible.
func Project(entries []compaction.Entry, replacements map[string][]string) Surface {
	surface := Surface{
		Entries:   append([]compaction.Entry(nil), entries...),
		Citations: make(map[string][]string, len(replacements)),
	}
	for summaryID, replaced := range replacements {
		copied := make([]string, len(replaced))
		copy(copied, replaced)
		sort.Strings(copied)
		surface.Citations[summaryID] = copied
	}
	return surface
}

// Summaries returns the summary entries in a history, in order.
func Summaries(entries []compaction.Entry) []compaction.Entry {
	var out []compaction.Entry
	for _, entry := range entries {
		if entry.Kind == compaction.KindSummary {
			out = append(out, entry)
		}
	}
	return out
}
