package skills

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Errors this package returns.
var (
	// ErrInvalidSkill reports a skill that cannot be registered.
	ErrInvalidSkill = errors.New("skills: invalid skill")
	// ErrDuplicateSkill reports a name that is already registered.
	ErrDuplicateSkill = errors.New("skills: duplicate skill")
	// ErrUnknownSkill reports a name the catalogue does not hold.
	ErrUnknownSkill = errors.New("skills: unknown skill")
	// ErrNotEnabled reports an operation on a skill the agent never enabled.
	ErrNotEnabled = errors.New("skills: skill is not enabled")
	// ErrMissingDependency reports an enabled skill whose dependency is not.
	ErrMissingDependency = errors.New("skills: missing dependency")
)

// Skill is one discoverable skill.
//
// Description and Body are separate on purpose: the description is what a
// catalogue lists (cheap, always visible), while the body is what enters a
// request (expensive, only when enabled). Collapsing them would make the
// catalogue cost what a request costs.
type Skill struct {
	// Name is the skill's stable identity. Required and unique.
	Name string
	// Description is the one-line summary a catalogue lists.
	Description string
	// Body is the content that enters a request once enabled.
	Body string
	// DependsOn names skills that must be enabled alongside this one.
	DependsOn []string
}

// Validate reports whether a skill can be registered.
func (s Skill) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: a name is required", ErrInvalidSkill)
	}
	if strings.TrimSpace(s.Description) == "" {
		return fmt.Errorf("%w: skill %q requires a description", ErrInvalidSkill, s.Name)
	}
	if strings.TrimSpace(s.Body) == "" {
		return fmt.Errorf("%w: skill %q requires a body", ErrInvalidSkill, s.Name)
	}
	seen := make(map[string]bool)
	for _, dependency := range s.DependsOn {
		if strings.TrimSpace(dependency) == "" {
			return fmt.Errorf("%w: skill %q has an empty dependency", ErrInvalidSkill, s.Name)
		}
		if dependency == s.Name {
			return fmt.Errorf("%w: skill %q depends on itself", ErrInvalidSkill, s.Name)
		}
		if seen[dependency] {
			return fmt.Errorf("%w: skill %q names %q twice", ErrInvalidSkill, s.Name, dependency)
		}
		seen[dependency] = true
	}
	return nil
}

// Entry is one catalogue row: what discovery found, without its body.
//
// It carries no Body field at all, so a caller cannot accidentally put catalogue
// content into a request: the type it is given does not contain the content.
type Entry struct {
	// Name is the skill's identity.
	Name string
	// Description is the summary visible before loading.
	Description string
	// DependsOn is the declared dependency list.
	DependsOn []string
}

// Catalogue owns the discoverable skills.
type Catalogue struct {
	skills map[string]Skill
	order  []string
}

// NewCatalogue returns an empty catalogue.
func NewCatalogue() *Catalogue {
	return &Catalogue{skills: make(map[string]Skill)}
}

// Register adds one skill.
//
// It refuses a duplicate name rather than replacing: a silent replacement would
// make "which skill ran" depend on registration order.
func (c *Catalogue) Register(skill Skill) error {
	if err := skill.Validate(); err != nil {
		return err
	}
	if _, exists := c.skills[skill.Name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateSkill, skill.Name)
	}
	c.skills[skill.Name] = skill
	c.order = append(c.order, skill.Name)
	return nil
}

// Discover returns the catalogue as entries, sorted by name.
//
// Sorting rather than preserving registration order makes the list a function of
// its content, so two builds that registered in different orders list the same
// things in the same order.
func (c *Catalogue) Discover() []Entry {
	if c == nil {
		return nil
	}
	entries := make([]Entry, 0, len(c.order))
	for _, name := range c.order {
		skill := c.skills[name]
		dependencies := make([]string, len(skill.DependsOn))
		copy(dependencies, skill.DependsOn)
		entries = append(entries, Entry{
			Name:        skill.Name,
			Description: skill.Description,
			DependsOn:   dependencies,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// Lookup returns a skill by name.
func (c *Catalogue) Lookup(name string) (Skill, bool) {
	if c == nil {
		return Skill{}, false
	}
	skill, ok := c.skills[name]
	return skill, ok
}

// Names returns the registered names in registration order.
func (c *Catalogue) Names() []string {
	if c == nil {
		return nil
	}
	out := make([]string, len(c.order))
	copy(out, c.order)
	return out
}
