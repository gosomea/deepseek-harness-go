package skills

import (
	"fmt"
	"sort"
	"strings"
)

// Loaded is one enabled skill's content, ready to enter a request.
type Loaded struct {
	// Name is the skill's identity; a request cites this.
	Name string
	// Body is the content that enters the request.
	Body string
}

// Loader tracks which skills an agent enabled.
//
// It is a value rather than a package-level set: two agents in one process must
// not share an enabled set, and a test must be able to enable without
// disturbing another.
type Loader struct {
	catalogue *Catalogue
	enabled   map[string]bool
	order     []string
}

// NewLoader returns a loader over a catalogue.
func NewLoader(catalogue *Catalogue) *Loader {
	return &Loader{catalogue: catalogue, enabled: make(map[string]bool)}
}

// Load enables one skill and, transitively, its dependencies.
//
// Dependencies are enabled rather than assumed present: the alternative is a
// request that cites a skill whose prerequisite was never loaded. Enabling an
// already-enabled skill is a no-op, so a repeated load does not double the
// content.
func (l *Loader) Load(name string) error {
	if l == nil || l.catalogue == nil {
		return fmt.Errorf("%w: no catalogue", ErrUnknownSkill)
	}
	skill, ok := l.catalogue.Lookup(name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSkill, name)
	}
	// Resolve dependencies depth-first, checking for cycles as the stack grows.
	visiting := make(map[string]bool)
	var enable func(string) error
	enable = func(current string) error {
		if l.enabled[current] {
			return nil
		}
		if visiting[current] {
			return fmt.Errorf("%w: %q is part of a dependency cycle", ErrInvalidSkill, current)
		}
		visiting[current] = true
		defer delete(visiting, current)
		resolved, ok := l.catalogue.Lookup(current)
		if !ok {
			return fmt.Errorf("%w: %q is required by %q", ErrMissingDependency, current, name)
		}
		for _, dependency := range resolved.DependsOn {
			if err := enable(dependency); err != nil {
				return err
			}
		}
		l.enabled[current] = true
		l.order = append(l.order, current)
		return nil
	}
	if err := enable(skill.Name); err != nil {
		return err
	}
	return nil
}

// Unload disables a skill and, transitively, anything that depends on it.
//
// Disabling a dependent too is deliberate: leaving a dependent enabled after its
// prerequisite is gone would produce a request whose content contradicts its own
// declared dependencies. Unloading a skill that is not enabled is an error, not a
// no-op, so a caller cannot believe it removed content it never added.
func (l *Loader) Unload(name string) error {
	if l == nil || l.catalogue == nil {
		return fmt.Errorf("%w: no catalogue", ErrUnknownSkill)
	}
	if _, ok := l.catalogue.Lookup(name); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSkill, name)
	}
	if !l.enabled[name] {
		return fmt.Errorf("%w: %q", ErrNotEnabled, name)
	}
	// Collect the skill and everything that depends on it, directly or not.
	toRemove := map[string]bool{name: true}
	for changed := true; changed; {
		changed = false
		for candidate := range l.enabled {
			if toRemove[candidate] {
				continue
			}
			skill := l.catalogue.skills[candidate]
			for _, dependency := range skill.DependsOn {
				if toRemove[dependency] {
					toRemove[candidate] = true
					changed = true
					break
				}
			}
		}
	}
	remaining := l.order[:0]
	for _, candidate := range l.order {
		if !toRemove[candidate] {
			remaining = append(remaining, candidate)
		}
	}
	l.order = remaining
	for candidate := range toRemove {
		delete(l.enabled, candidate)
	}
	return nil
}

// Enabled reports whether a skill is enabled.
func (l *Loader) Enabled(name string) bool {
	if l == nil {
		return false
	}
	return l.enabled[name]
}

// Loaded returns the enabled skills in load order, with their bodies.
//
// Only enabled skills appear. This is the function a request renderer calls, and
// it cannot return a skill the agent never enabled — the catalogue is not
// consulted for content here.
func (l *Loader) Loaded() []Loaded {
	if l == nil {
		return nil
	}
	out := make([]Loaded, 0, len(l.order))
	for _, name := range l.order {
		skill := l.catalogue.skills[name]
		out = append(out, Loaded{Name: skill.Name, Body: skill.Body})
	}
	return out
}

// Render returns the enabled content as request text.
//
// The result is a pure function of the enabled set: the same enabled set renders
// the same bytes, which is what lets a test compare two renders. An unloaded
// skill leaves no residue, because its body is read from the catalogue at render
// time rather than cached here.
func (l *Loader) Render() string {
	loaded := l.Loaded()
	var builder strings.Builder
	for index, item := range loaded {
		if index > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(item.Body)
	}
	return builder.String()
}

// EnabledNames returns the enabled names in load order.
func (l *Loader) EnabledNames() []string {
	if l == nil {
		return nil
	}
	out := make([]string, len(l.order))
	copy(out, l.order)
	return out
}

// DependencyClosure returns the names enabling name would pull in, sorted.
//
// It is separate from Load so a caller can report what a load would cost before
// doing it.
func (l *Loader) DependencyClosure(name string) ([]string, error) {
	if l == nil || l.catalogue == nil {
		return nil, fmt.Errorf("%w: no catalogue", ErrUnknownSkill)
	}
	skill, ok := l.catalogue.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSkill, name)
	}
	closure := make(map[string]bool)
	var walk func(string) error
	walk = func(current string) error {
		if closure[current] {
			return nil
		}
		resolved, ok := l.catalogue.Lookup(current)
		if !ok {
			return fmt.Errorf("%w: %q", ErrMissingDependency, current)
		}
		closure[current] = true
		for _, dependency := range resolved.DependsOn {
			if err := walk(dependency); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(skill.Name); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(closure))
	for candidate := range closure {
		names = append(names, candidate)
	}
	sort.Strings(names)
	return names, nil
}
