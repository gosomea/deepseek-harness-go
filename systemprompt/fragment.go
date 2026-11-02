package systemprompt

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrInvalidValue reports a fragment or assembly input that cannot be used.
var ErrInvalidValue = errors.New("systemprompt: invalid value")

// Source names which component contributed one fragment.
//
// The source is kept because a rendered prompt must be traceable: when a
// model behaves unexpectedly, the first question is which contributor wrote
// the instruction that caused it.
type Source string

// The fragment sources this package understands.
const (
	// SourceBase is the harness's own base instructions.
	SourceBase Source = "base"
	// SourceAgent is an agent-specific instruction block.
	SourceAgent Source = "agent"
	// SourceSkill is a contributed skill or capability description.
	SourceSkill Source = "skill"
	// SourceEnvironment is a runtime environment description.
	SourceEnvironment Source = "environment"
)

// Fragment is one piece of prompt text.
//
// Order is an explicit field rather than implied by registration sequence: two
// independently registered contributors must be able to agree on a stable
// position without coordinating, and a later registration must not silently
// move an earlier fragment.
type Fragment struct {
	// ID is a stable identifier, used for deduplication and diagnostics.
	ID string `json:"id"`
	// Source records which component contributed the fragment.
	Source Source `json:"source"`
	// Order is the fragment's position: lower values render first. Equal orders
	// are broken by ID so the result never depends on iteration order.
	Order int `json:"order"`
	// Text is the fragment's rendered content.
	Text string `json:"text"`
	// Scopes lists the Agent scope keys this fragment applies to. An empty list
	// means every scope sees it.
	Scopes []string `json:"scopes,omitempty"`
}

// Validate reports whether the fragment is usable.
func (f Fragment) Validate() error {
	if strings.TrimSpace(f.ID) == "" {
		return fmt.Errorf("%w: fragment requires an id", ErrInvalidValue)
	}
	if strings.TrimSpace(string(f.Source)) == "" {
		return fmt.Errorf("%w: fragment %q requires a source", ErrInvalidValue, f.ID)
	}
	if strings.TrimSpace(f.Text) == "" {
		return fmt.Errorf("%w: fragment %q requires text", ErrInvalidValue, f.ID)
	}
	for _, scope := range f.Scopes {
		if strings.TrimSpace(scope) == "" {
			return fmt.Errorf("%w: fragment %q has a blank scope", ErrInvalidValue, f.ID)
		}
	}
	return nil
}

// AppliesTo reports whether the fragment is visible in one Agent scope. An
// empty scope list is global.
func (f Fragment) AppliesTo(scope string) bool {
	if len(f.Scopes) == 0 {
		return true
	}
	for _, candidate := range f.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

// Set is an ordered collection of fragments.
//
// It owns its slice and refuses a duplicate ID: two fragments with the same ID
// would make the rendered prompt depend on which one a reader happened to keep.
type Set struct {
	fragments []Fragment
	byID      map[string]Fragment
}

// NewSet returns an empty fragment set.
func NewSet() *Set {
	return &Set{byID: make(map[string]Fragment)}
}

// Add inserts one fragment, refusing an invalid or duplicate one.
func (s *Set) Add(fragment Fragment) error {
	if err := fragment.Validate(); err != nil {
		return err
	}
	if s.byID == nil {
		s.byID = make(map[string]Fragment)
	}
	if _, exists := s.byID[fragment.ID]; exists {
		return fmt.Errorf("%w: fragment %q is already present", ErrInvalidValue, fragment.ID)
	}
	s.byID[fragment.ID] = fragment
	s.fragments = append(s.fragments, fragment)
	return nil
}

// Remove deletes one fragment, reporting whether it was present.
func (s *Set) Remove(id string) bool {
	if _, exists := s.byID[id]; !exists {
		return false
	}
	delete(s.byID, id)
	kept := s.fragments[:0]
	for _, fragment := range s.fragments {
		if fragment.ID != id {
			kept = append(kept, fragment)
		}
	}
	s.fragments = kept
	return true
}

// Len returns how many fragments the set holds.
func (s *Set) Len() int { return len(s.fragments) }

// Fragments returns a copy of the fragments in insertion order.
func (s *Set) Fragments() []Fragment {
	return append([]Fragment(nil), s.fragments...)
}

// Ordered returns the fragments sorted by Order and then by ID, filtered to one
// Agent scope.
//
// The filter is applied during ordering rather than afterwards so that a caller
// cannot accidentally render an unscoped set: fragments that do not apply are
// absent from the result, not merely ignored later.
func (s *Set) Ordered(scope string) []Fragment {
	visible := make([]Fragment, 0, len(s.fragments))
	for _, fragment := range s.fragments {
		if fragment.AppliesTo(scope) {
			visible = append(visible, fragment)
		}
	}
	sort.SliceStable(visible, func(i, j int) bool {
		if visible[i].Order != visible[j].Order {
			return visible[i].Order < visible[j].Order
		}
		return visible[i].ID < visible[j].ID
	})
	return visible
}
