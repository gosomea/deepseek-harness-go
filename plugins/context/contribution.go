package context

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Kind names where a contribution came from.
//
// The kind is recorded rather than inferred because an operator reading a
// request wants to know why a line is present, and inferring it from the text
// later would be a guess.
type Kind string

// The contribution kinds this package records.
const (
	// KindInstruction is operator or profile guidance for the model.
	KindInstruction Kind = "instruction"
	// KindReference is a pointer to material the request may reach, such as a
	// file or an earlier session. The pointer is the fact; the content is
	// resolved at render time.
	KindReference Kind = "reference"
	// KindTime is a recorded clock value the model is told about.
	KindTime Kind = "time"
)

// Source names the owner that produced a contribution.
type Source string

// The sources this package accepts, in the order they render.
const (
	// SourceBase is the always-present guidance a harness ships.
	SourceBase Source = "base"
	// SourceProfile is operator or profile configuration.
	SourceProfile Source = "profile"
	// SourceSession is the conversation itself.
	SourceSession Source = "session"
	// SourceEnvironment is the execution world the agent runs in.
	SourceEnvironment Source = "environment"
)

// Errors this package returns.
var (
	// ErrInvalidContribution reports a contribution that cannot be recorded.
	ErrInvalidContribution = errors.New("context: invalid contribution")
	// ErrDuplicateContribution reports a contribution ID that is already owned.
	ErrDuplicateContribution = errors.New("context: duplicate contribution id")
	// ErrNotRecorded reports a render that would introduce content the log does
	// not carry.
	ErrNotRecorded = errors.New("context: contribution was not recorded")
)

// renderOrder fixes the sequence of sources in a rendered request. It is a slice
// rather than a map so that the order is a stated fact, not iteration luck.
var renderOrder = []Source{SourceBase, SourceProfile, SourceEnvironment, SourceSession}

// sourceRank returns a source's position, or -1 when unknown.
func sourceRank(source Source) int {
	for index, candidate := range renderOrder {
		if candidate == source {
			return index
		}
	}
	return -1
}

// Reference is a request-time access path held by a contribution.
//
// It is deliberately not the referenced content: the path may point at a file
// that changes, or at an earlier session that is read lazily. Keeping the
// reference and the content apart is what lets a reader tell "what the request
// cited" from "what happened to be on disk when it rendered".
type Reference struct {
	// Path is the location the request cites. Required.
	Path string
	// Note explains why the request cites it. Optional.
	Note string
}

// Contribution is one recorded reason the model saw something.
//
// Every field needed to rebuild the request is here: nothing about a
// contribution is recovered from ambient state at render time.
type Contribution struct {
	// ID is the contribution's stable identity within the log. Required and
	// unique; it is what a later surface replacement cites.
	ID string
	// Kind is where the contribution came from.
	Kind Kind
	// Source is the owner that produced it; it decides render position.
	Source Source
	// Text is the contribution's content. Required for instruction and time
	// kinds; refused for a reference, which carries no inline content.
	Text string
	// Reference is the access path, for KindReference. Nil otherwise.
	Reference *Reference
	// RecordedAt is the moment the contribution entered the log.
	RecordedAt time.Time
	// Scopes restricts the contribution to agents whose scope matches. An empty
	// slice means every agent in the session.
	Scopes []string
}

// Validate reports whether a contribution can be recorded.
//
// It is a method rather than a constructor check so a caller can validate a value
// it built itself, and so the log-side writer and the renderer agree on exactly
// one rule.
func (c Contribution) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("%w: an id is required", ErrInvalidContribution)
	}
	switch c.Kind {
	case KindInstruction, KindTime:
		if strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("%w: %s %q requires text", ErrInvalidContribution, c.Kind, c.ID)
		}
		if c.Reference != nil {
			return fmt.Errorf("%w: %s %q must not carry a reference", ErrInvalidContribution, c.Kind, c.ID)
		}
	case KindReference:
		if c.Reference == nil {
			return fmt.Errorf("%w: reference %q requires an access path", ErrInvalidContribution, c.ID)
		}
		if strings.TrimSpace(c.Reference.Path) == "" {
			return fmt.Errorf("%w: reference %q has an empty path", ErrInvalidContribution, c.ID)
		}
		if c.Text != "" {
			return fmt.Errorf("%w: reference %q must not carry inline text", ErrInvalidContribution, c.ID)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidContribution, c.Kind)
	}
	if sourceRank(c.Source) < 0 {
		return fmt.Errorf("%w: unknown source %q", ErrInvalidContribution, c.Source)
	}
	return nil
}

// Set owns the contributions recorded for one render, in insertion order.
//
// It is not safe for concurrent use; the session log serializes appends, and a
// renderer works on one snapshot at a time.
type Set struct {
	byID  map[string]Contribution
	order []string
}

// NewSet returns an empty contribution set.
func NewSet() *Set {
	return &Set{byID: make(map[string]Contribution)}
}

// Add records one contribution.
//
// It refuses a duplicate id rather than replacing: a silent overwrite would make
// "which contribution the model saw" depend on call order, which is exactly the
// property this package exists to remove.
func (s *Set) Add(contribution Contribution) error {
	if err := contribution.Validate(); err != nil {
		return err
	}
	if _, exists := s.byID[contribution.ID]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateContribution, contribution.ID)
	}
	s.byID[contribution.ID] = contribution
	s.order = append(s.order, contribution.ID)
	return nil
}

// Len returns how many contributions the set owns.
func (s *Set) Len() int { return len(s.order) }

// Get returns a contribution by id.
func (s *Set) Get(id string) (Contribution, bool) {
	contribution, ok := s.byID[id]
	return contribution, ok
}

// IDs returns the contribution ids in insertion order.
func (s *Set) IDs() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Ordered returns the contributions that apply to scope, in render order.
//
// The result is a pure function of the set: it sorts by source position, then by
// insertion order. Two renders of the same set therefore agree, which is what
// makes a byte comparison meaningful.
func (s *Set) Ordered(scope string) []Contribution {
	type ranked struct {
		contribution Contribution
		rank         int
		position     int
	}
	var rankedContributions []ranked
	for position, id := range s.order {
		contribution := s.byID[id]
		if !appliesTo(contribution, scope) {
			continue
		}
		rankedContributions = append(rankedContributions, ranked{
			contribution: contribution,
			rank:         sourceRank(contribution.Source),
			position:     position,
		})
	}
	sort.SliceStable(rankedContributions, func(i, j int) bool {
		if rankedContributions[i].rank != rankedContributions[j].rank {
			return rankedContributions[i].rank < rankedContributions[j].rank
		}
		return rankedContributions[i].position < rankedContributions[j].position
	})
	out := make([]Contribution, 0, len(rankedContributions))
	for _, item := range rankedContributions {
		out = append(out, item.contribution)
	}
	return out
}

// appliesTo reports whether a contribution is visible in a scope.
//
// An empty scope list means "every agent": a contribution that named no scope did
// not restrict itself, and reading that as "visible nowhere" would silently drop
// guidance.
func appliesTo(contribution Contribution, scope string) bool {
	if len(contribution.Scopes) == 0 {
		return true
	}
	for _, candidate := range contribution.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}
