package testkit

import (
	"fmt"
	"strings"
)

// CompareTraces compares a Go trace against a reference trace, applying the
// scenario's declared divergences.
//
// Declared divergences are matched in sequence order. At a mismatch the longest
// declaration whose Go block and reference block both start at the current
// position is applied (see matchDivergence). Every declaration must be used, so
// the list cannot rot into a blanket allow-list, and an undeclared difference
// fails with the first differing line.
func CompareTraces(scenario *Scenario, reference, got string) error {
	if err := validateDivergences(scenario); err != nil {
		return err
	}
	want := trimTrailingEmpty(strings.Split(reference, "\n"))
	have := trimTrailingEmpty(strings.Split(got, "\n"))
	used := make([]bool, len(scenario.Divergences))

	for index, referenceIndex := 0, 0; ; {
		switch {
		case index == len(have) && referenceIndex == len(want):
			return checkAllDivergencesUsed(scenario, used)
		case index == len(have) || referenceIndex == len(want):
			return fmt.Errorf("scenario %s: trace length differs at Go line %d / reference line %d\n--- reference\n%s\n--- go\n%s",
				scenario.ID, index+1, referenceIndex+1, reference, got)
		}
		if have[index] == want[referenceIndex] {
			index++
			referenceIndex++
			continue
		}
		divergence, ok := matchDivergence(scenario, have, index, want, referenceIndex)
		if !ok {
			return fmt.Errorf("scenario %s: undeclared difference at Go line %d / reference line %d\n  reference: %s\n  go:        %s",
				scenario.ID, index+1, referenceIndex+1, want[referenceIndex], have[index])
		}
		used[divergence] = true
		index += len(scenario.Divergences[divergence].Go)
		referenceIndex += len(scenario.Divergences[divergence].Reference)
	}
}

// matchDivergence reports the index of the declaration matching the current
// position, preferring the longest applicable Go block.
//
// Declarations may share a prefix. Taking the shortest would consume part of a
// longer block and then report a difference a few lines later, blaming the
// wrong location.
func matchDivergence(scenario *Scenario, have []string, index int, want []string, referenceIndex int) (int, bool) {
	best, bestLength := -1, -1
	for position, divergence := range scenario.Divergences {
		if len(divergence.Go) <= bestLength {
			continue
		}
		if !blockMatches(have, index, divergence.Go) {
			continue
		}
		if !blockMatches(want, referenceIndex, divergence.Reference) {
			continue
		}
		best, bestLength = position, len(divergence.Go)
	}
	return best, best >= 0
}

func blockMatches(lines []string, start int, block []string) bool {
	if len(block) == 0 || start+len(block) > len(lines) {
		return false
	}
	for offset, line := range block {
		if lines[start+offset] != line {
			return false
		}
	}
	return true
}

// validateDivergences rejects declarations that cannot serve as evidence: one
// missing a side, or lacking a written reason, is not a reviewed statement
// about a difference.
func validateDivergences(scenario *Scenario) error {
	for index, divergence := range scenario.Divergences {
		if len(divergence.Go) == 0 || len(divergence.Reference) == 0 {
			return fmt.Errorf("scenario %s: divergence %d is incomplete; it needs both a Go block and a reference block",
				scenario.ID, index+1)
		}
		if divergence.Reason == "" {
			return fmt.Errorf("scenario %s: divergence %d has no reason", scenario.ID, index+1)
		}
	}
	return nil
}

func checkAllDivergencesUsed(scenario *Scenario, used []bool) error {
	for index, divergence := range scenario.Divergences {
		if used[index] {
			continue
		}
		return fmt.Errorf("scenario %s: declared divergence %d never occurred (go %v vs reference %v); reason given: %s",
			scenario.ID, index+1, divergence.Go, divergence.Reference, divergence.Reason)
	}
	return nil
}

func trimTrailingEmpty(lines []string) []string {
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
