package preset

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Errors this package returns.
var (
	// ErrUnknownPreset reports a name the registry does not hold.
	ErrUnknownPreset = errors.New("preset: unknown preset")
	// ErrInvalidPreset reports a bundle that cannot be registered.
	ErrInvalidPreset = errors.New("preset: invalid preset")
	// ErrDuplicateContribution reports two bundles claiming the same key.
	ErrDuplicateContribution = errors.New("preset: duplicate contribution")
)

// Contribution is one key/value a bundle supplies.
type Contribution struct {
	// Key is the setting or persona key. Required.
	Key string
	// Value is the contributed value.
	Value string
}

// Preset is one named bundle.
type Preset struct {
	// Name is the bundle's stable identity. Required and unique.
	Name string
	// Description explains when a reader should choose this bundle.
	Description string
	// Settings are the bundle's key/value contributions, in insertion order.
	Settings []Contribution
	// Persona is the bundle's optional persona text.
	Persona string
}

// Validate reports whether a bundle can be registered.
func (p Preset) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: a name is required", ErrInvalidPreset)
	}
	seen := make(map[string]bool)
	for _, contribution := range p.Settings {
		if strings.TrimSpace(contribution.Key) == "" {
			return fmt.Errorf("%w: preset %q has a contribution without a key", ErrInvalidPreset, p.Name)
		}
		if seen[contribution.Key] {
			return fmt.Errorf("%w: preset %q contributes %q twice", ErrInvalidPreset, p.Name, contribution.Key)
		}
		seen[contribution.Key] = true
	}
	return nil
}

// Registry owns the registered bundles.
//
// It is a value rather than a package-level table: two registries in one process
// must not see each other's bundles, and a test must be able to build one without
// disturbing another.
type Registry struct {
	presets map[string]Preset
	order   []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{presets: make(map[string]Preset)}
}

// Register adds one bundle.
//
// It refuses a duplicate name rather than replacing: a silent replacement would
// make "which bundle ran" depend on registration order, which is exactly the
// ambiguity this package exists to remove.
func (r *Registry) Register(preset Preset) error {
	if err := preset.Validate(); err != nil {
		return err
	}
	if _, exists := r.presets[preset.Name]; exists {
		return fmt.Errorf("%w: %q is already registered", ErrInvalidPreset, preset.Name)
	}
	r.presets[preset.Name] = preset
	r.order = append(r.order, preset.Name)
	return nil
}

// Names returns the registered names in registration order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Lookup returns a bundle by name.
func (r *Registry) Lookup(name string) (Preset, bool) {
	preset, ok := r.presets[name]
	return preset, ok
}

// Applied is the result of applying a bundle.
//
// The name is carried on the result rather than left to the caller to remember,
// so a runtime printout cannot show values without also showing which bundle
// produced them.
type Applied struct {
	// Name is the bundle that was applied.
	Name string
	// Settings are the contributed values, sorted by key.
	Settings []Contribution
	// Persona is the bundle's persona text, empty when it has none.
	Persona string
}

// Apply resolves a bundle by name.
//
// An unknown name is an error, never an empty result: starting an agent with the
// wrong bundle silently would be a configuration mistake that looks like a
// working agent.
func (r *Registry) Apply(name string) (Applied, error) {
	if r == nil {
		return Applied{}, fmt.Errorf("%w: no registry", ErrUnknownPreset)
	}
	preset, ok := r.presets[name]
	if !ok {
		return Applied{}, fmt.Errorf("%w: %q", ErrUnknownPreset, name)
	}
	settings := make([]Contribution, len(preset.Settings))
	copy(settings, preset.Settings)
	sort.SliceStable(settings, func(i, j int) bool { return settings[i].Key < settings[j].Key })
	return Applied{Name: preset.Name, Settings: settings, Persona: preset.Persona}, nil
}

// Merge combines bundles in the given order.
//
// Later bundles win, and the winner is recorded. Two bundles supplying the same
// key is legal here (that is what layering means) but the provenance is kept, so
// a reader can see that a value came from the later bundle rather than the first.
func (r *Registry) Merge(names ...string) (Applied, error) {
	if r == nil {
		return Applied{}, fmt.Errorf("%w: no registry", ErrUnknownPreset)
	}
	merged := Applied{}
	for _, name := range names {
		preset, ok := r.presets[name]
		if !ok {
			return Applied{}, fmt.Errorf("%w: %q", ErrUnknownPreset, name)
		}
		for _, contribution := range preset.Settings {
			replaced := false
			for index := range merged.Settings {
				if merged.Settings[index].Key == contribution.Key {
					merged.Settings[index].Value = contribution.Value
					replaced = true
					break
				}
			}
			if !replaced {
				merged.Settings = append(merged.Settings, contribution)
			}
		}
		if preset.Persona != "" {
			merged.Persona = preset.Persona
		}
	}
	sort.SliceStable(merged.Settings, func(i, j int) bool {
		return merged.Settings[i].Key < merged.Settings[j].Key
	})
	// With several bundles a single name would be misleading, so Name is set only
	// when exactly one bundle was merged; callers use Owners to see the split.
	if len(names) == 1 {
		merged.Name = names[0]
	}
	return merged, nil
}

// Owners returns which bundle supplied each key in a merge, for diagnostics.
//
// It is recomputed rather than stored so a caller cannot mutate the map that a
// previous merge returned.
func (r *Registry) Owners(names ...string) (map[string]string, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: no registry", ErrUnknownPreset)
	}
	owners := make(map[string]string)
	for _, name := range names {
		preset, ok := r.presets[name]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownPreset, name)
		}
		for _, contribution := range preset.Settings {
			owners[contribution.Key] = preset.Name
		}
	}
	return owners, nil
}
