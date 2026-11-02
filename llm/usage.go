package llm

import "fmt"

// UsageDelta accumulates token usage across the attempts of one logical call.
//
// It exists because "unknown usage" and "zero usage" are different facts that
// must not be merged: a provider that reports no accounting at all leaves the
// accumulator empty, while a provider that reports a real zero produces a
// non-nil sum. Callers that price a call ask Reported instead of testing the
// counts, which would confuse the two.
type UsageDelta struct {
	// reported records whether any usage chunk was seen.
	reported bool
	// sum is the running total of every reported usage value.
	sum Usage
}

// Add folds one reported usage value into the accumulator.
func (d *UsageDelta) Add(usage Usage) {
	d.reported = true
	d.sum.InputTokens += usage.InputTokens
	d.sum.OutputTokens += usage.OutputTokens
	d.sum.TotalTokens = addOptional(d.sum.TotalTokens, usage.TotalTokens)
	d.sum.CacheReadTokens = addOptional(d.sum.CacheReadTokens, usage.CacheReadTokens)
	d.sum.CacheWriteTokens = addOptional(d.sum.CacheWriteTokens, usage.CacheWriteTokens)
	d.sum.ReasoningTokens = addOptional(d.sum.ReasoningTokens, usage.ReasoningTokens)
}

// Reported reports whether at least one usage value was folded in. It answers
// "did the provider disclose accounting", which a zero count cannot.
func (d *UsageDelta) Reported() bool { return d.reported }

// Total returns the accumulated usage and whether anything was reported. The
// second result is what distinguishes a reported zero from an absent value.
func (d *UsageDelta) Total() (Usage, bool) { return d.sum, d.reported }

// addOptional adds two optional counters, preserving absence: an unreported
// counter stays absent instead of becoming a reported zero.
func addOptional(sum, add *int) *int {
	if add == nil {
		return sum
	}
	value := *add
	if sum != nil {
		value += *sum
	}
	return &value
}

// MergeUsage folds a sequence of usage reports, returning the sum and whether
// any report was present.
func MergeUsage(usages []Usage) (Usage, bool) {
	var delta UsageDelta
	for _, usage := range usages {
		delta.Add(usage)
	}
	return delta.Total()
}

// RequireUsage returns the accumulated usage or an error naming the missing
// accounting. It is for callers that genuinely cannot continue without it, such
// as a budget check; callers that can proceed should use Reported instead.
func RequireUsage(usages []Usage) (Usage, error) {
	total, reported := MergeUsage(usages)
	if !reported {
		return Usage{}, fmt.Errorf("%w: no usage reported", ErrInvalidValue)
	}
	return total, nil
}
