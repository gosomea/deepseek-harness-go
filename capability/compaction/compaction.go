// Package compaction defines the compaction seam: a region of history is
// replaced by a shorter form, and the replacement is reportable.
//
// The package exists because replacing history has two failure modes that look
// like success. The first is a boundary that lands between a tool call and its
// result, leaving a call whose answer is gone. The second is a summary that failed
// while the caller was told the replacement finished. Both are expressed here as
// rules rather than conventions, so a provider cannot satisfy the interface while
// breaking them.
//
// A tool call and its result are therefore treated as one indivisible pair: any
// boundary that would separate them is refused before anything is written.
package compaction

import (
	"errors"
	"fmt"
	"strings"
)

// Errors this package returns.
var (
	// ErrInvalidPlan reports a plan that cannot be applied.
	ErrInvalidPlan = errors.New("compaction: invalid plan")
	// ErrPairSplit reports a boundary that would separate a tool call from its
	// result.
	ErrPairSplit = errors.New("compaction: boundary splits a tool call from its result")
	// ErrSummaryFailed reports a summary that did not succeed.
	ErrSummaryFailed = errors.New("compaction: summary failed")
	// ErrUnknownEntry reports an entry id the log does not hold.
	ErrUnknownEntry = errors.New("compaction: unknown entry")
)

// Kind is an entry's role in history.
type Kind string

// The entry kinds this seam names.
const (
	// KindMessage is ordinary conversation content.
	KindMessage Kind = "message"
	// KindToolCall is a model request to run a tool.
	KindToolCall Kind = "tool-call"
	// KindToolResult is a tool's answer to a call.
	KindToolResult Kind = "tool-result"
	// KindSummary is a previously produced summary.
	KindSummary Kind = "summary"
)

// Entry is one item of history.
//
// CallID links a tool call to its result. It is empty for messages and summaries.
type Entry struct {
	// ID is the entry's stable identity. Required and unique.
	ID string
	// Kind is the entry's role.
	Kind Kind
	// Text is the entry's content.
	Text string
	// CallID links a call to its result.
	CallID string
}

// Validate reports whether an entry is usable.
func (e Entry) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("%w: an entry id is required", ErrInvalidPlan)
	}
	switch e.Kind {
	case KindMessage, KindSummary:
		if e.CallID != "" {
			return fmt.Errorf("%w: entry %q is a %s but names a call", ErrInvalidPlan, e.ID, e.Kind)
		}
	case KindToolCall, KindToolResult:
		if strings.TrimSpace(e.CallID) == "" {
			return fmt.Errorf("%w: entry %q is a %s without a call id", ErrInvalidPlan, e.ID, e.Kind)
		}
	default:
		return fmt.Errorf("%w: entry %q has unknown kind %q", ErrInvalidPlan, e.ID, e.Kind)
	}
	return nil
}

// Plan describes replacing a contiguous region of history.
//
// The region is a half-open index range into the entries given to Apply. It is
// expressed as indexes rather than ids so a caller cannot accidentally name a
// region that skips an entry, and so each boundary of the region can be checked
// against the entries that touch it.
type Plan struct {
	// Start is the first replaced index, inclusive.
	Start int
	// End is the last replaced index, exclusive.
	End int
	// Summary is the replacement text.
	Summary string
}

// Validate reports whether a plan can be applied to a history of length n.
func (p Plan) Validate(n int) error {
	if p.Start < 0 || p.End > n || p.Start >= p.End {
		return fmt.Errorf("%w: region [%d,%d) is not inside a history of %d", ErrInvalidPlan, p.Start, p.End, n)
	}
	if strings.TrimSpace(p.Summary) == "" {
		return fmt.Errorf("%w: a summary is required", ErrInvalidPlan)
	}
	return nil
}

// CheckPairing reports whether a boundary would separate a call from its result.
//
// A cut between a call and its result leaves a request whose answer is gone, so
// it is refused before anything is written. The region has two candidate
// boundaries, and each is checked: the start boundary between Start-1 and Start,
// and the end boundary between End-1 and End.
func CheckPairing(entries []Entry, start, end int) error {
	if start < 0 || end > len(entries) || start >= end {
		return fmt.Errorf("%w: region [%d,%d) is not inside a history of %d", ErrInvalidPlan, start, end, len(entries))
	}
	// The start boundary.
	if start > 0 && entries[start-1].CallID != "" && entries[start-1].CallID == entries[start].CallID {
		return fmt.Errorf("%w: the start boundary falls between %q and %q (call %q)",
			ErrPairSplit, entries[start-1].ID, entries[start].ID, entries[start].CallID)
	}
	// The end boundary.
	if end < len(entries) && entries[end-1].CallID != "" && entries[end-1].CallID == entries[end].CallID {
		return fmt.Errorf("%w: the end boundary falls between %q and %q (call %q)",
			ErrPairSplit, entries[end-1].ID, entries[end].ID, entries[end].CallID)
	}
	return nil
}

// Result is what a compaction produced.
//
// Applied reports whether the replacement actually happened. A caller that
// received an error must not treat the entries as replaced, and a caller that
// received Applied=false must not report a replacement either.
type Result struct {
	// Entries are the history after compaction.
	Entries []Entry
	// Replaced is the number of entries the summary stands for.
	Replaced int
	// Applied reports whether the replacement happened.
	Applied bool
	// SummaryID is the id of the produced summary entry, when applied.
	SummaryID string
}

// Apply replaces a region with a summary.
//
// The pairing rule is checked first, and a plan that fails it changes nothing:
// the returned entries are the originals and Applied is false. This ordering
// matters because a partial replacement is the very dangling-call state the rule
// exists to prevent.
func Apply(entries []Entry, plan Plan, summaryID string) (Result, error) {
	for _, entry := range entries {
		if err := entry.Validate(); err != nil {
			return Result{}, err
		}
	}
	if err := plan.Validate(len(entries)); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(summaryID) == "" {
		return Result{}, fmt.Errorf("%w: a summary id is required", ErrInvalidPlan)
	}
	if err := CheckPairing(entries, plan.Start, plan.End); err != nil {
		return Result{Entries: cloneEntries(entries), Applied: false}, err
	}
	summary := Entry{ID: summaryID, Kind: KindSummary, Text: plan.Summary}
	out := make([]Entry, 0, len(entries)-(plan.End-plan.Start)+1)
	out = append(out, cloneEntries(entries[:plan.Start])...)
	out = append(out, summary)
	out = append(out, cloneEntries(entries[plan.End:])...)
	return Result{
		Entries:   out,
		Replaced:  plan.End - plan.Start,
		Applied:   true,
		SummaryID: summaryID,
	}, nil
}

// cloneEntries returns a detached copy so a caller cannot mutate a committed
// history through the slice it passed.
func cloneEntries(entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	return out
}

// Band describes a region a plan may legally cover.
//
// It exists so a caller can see the choices before making one: the seams that are
// legal are exactly the ones that do not fall between a call and its result.
type Band struct {
	// Start is the first index of the band, inclusive.
	Start int
	// End is the last index of the band, exclusive.
	End int
	// Pairs is how many call/result pairs the band contains.
	Pairs int
}

// LegalBands returns the maximal regions that do not split a pair.
//
// It walks the history and closes a band whenever a pair would be cut, so a
// caller picking a band cannot pick an illegal one by accident.
func LegalBands(entries []Entry) []Band {
	var bands []Band
	start := 0
	for index := 0; index < len(entries); index++ {
		if entries[index].CallID == "" {
			continue
		}
		// Close the run of non-pair entries before this pair.
		if start < index {
			bands = append(bands, Band{Start: start, End: index, Pairs: countPairs(entries[start:index])})
		}
		// The pair itself is a band: it contains its call and result together.
		pairEnd := index
		for pairEnd+1 < len(entries) && entries[pairEnd+1].CallID == entries[index].CallID {
			pairEnd++
		}
		bands = append(bands, Band{Start: index, End: pairEnd + 1, Pairs: 1})
		index = pairEnd
		start = pairEnd + 1
	}
	if start < len(entries) {
		bands = append(bands, Band{Start: start, End: len(entries), Pairs: countPairs(entries[start:])})
	}
	return bands
}

// countPairs counts distinct call ids in a region.
func countPairs(entries []Entry) int {
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.CallID != "" {
			seen[entry.CallID] = true
		}
	}
	return len(seen)
}
