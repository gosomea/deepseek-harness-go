package protocol

import (
	"fmt"
	"strings"
)

// WireCompaction summarizes a compaction surface without exposing internal types.
//
// It carries only the values a remote client needs — whether a replacement
// happened, how many entries it stood for, and the summary id — never the
// capability/compaction Entry type.
type WireCompaction struct {
	// Applied reports whether a replacement happened.
	Applied bool `json:"applied"`
	// Replaced is how many entries the summary stands for.
	Replaced int `json:"replaced"`
	// SummaryID is the produced summary's id.
	SummaryID string `json:"summaryId,omitempty"`
}

// WireSubagent describes a child Agent without exposing the agent package type.
type WireSubagent struct {
	// Child is the child id.
	Child string `json:"child"`
	// Parent is the parent id.
	Parent string `json:"parent"`
	// Mode is spawn or fork.
	Mode string `json:"mode"`
	// InheritThrough is the parent event count inherited, for a fork.
	InheritThrough int `json:"inheritThrough,omitempty"`
	// Depth is the child's depth below the root.
	Depth int `json:"depth"`
}

// WireTask describes one durable job without exposing the jobs store type.
type WireTask struct {
	// ID is the job id.
	ID string `json:"id"`
	// Kind is the job kind.
	Kind string `json:"kind"`
	// Status is the job state name.
	Status string `json:"status"`
	// Attempts is how many times it has run.
	Attempts int `json:"attempts"`
}

// GuardWireFields reports whether any field value names an internal type.
//
// The protocol boundary must not carry a raw internal type by name: a value like
// "github.com/.../capability/compaction.Entry" or "*agent.Owner" means a producer
// leaked an internal handle into a DTO. This guard makes that a checkable error
// rather than a review note.
func GuardWireFields(values map[string]string) error {
	for key, value := range values {
		if leaked(value) {
			return fmt.Errorf("%w: field %q names an internal type: %q", ErrInvalidEnvelope, key, value)
		}
	}
	return nil
}

// leaked reports whether a string looks like an internal Go type reference.
func leaked(value string) bool {
	if strings.HasPrefix(value, "*") {
		return true
	}
	for _, marker := range []string{"github.com/gosomea/deepseek-harness-go/", "trpc.group/"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	// A bare "pkg.Type" reference to a known internal package.
	for _, pkg := range []string{"agent.", "session.", "capability.", "agentloop."} {
		if strings.Contains(value, pkg) {
			return true
		}
	}
	return false
}
