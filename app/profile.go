package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

var (
	// ErrInvalidProfile reports a profile declaration the composer cannot apply.
	ErrInvalidProfile = errors.New("app: invalid profile")
	// ErrRequiredEntryFailed reports a required entry that did not become ready.
	ErrRequiredEntryFailed = errors.New("app: required entry failed")
)

// Profile is a named composition: an ordered bundle list, the profile's own
// patches, and the entries that must become ready.
//
// Layers apply in this order: each named bundle in Bundles order, then the
// profile's own Patches. A later layer overrides an earlier layer's fields on the
// same entry, so the profile is the most specific layer and therefore the one
// that wins.
type Profile struct {
	// Name identifies the profile in diagnostics.
	Name string `json:"name"`
	// Bundles lists bundle names in application order; each must be registered.
	Bundles []string `json:"bundles,omitempty"`
	// Patches are the profile's own overlays, applied after every bundle layer.
	Patches []Patch `json:"patches,omitempty"`
	// Required lists entry ids that must become ready. The form "id:name" also
	// asserts the entry's plugin name, so a rename is not silently accepted.
	Required []string `json:"required,omitempty"`
}

// ParseProfile decodes a profile declaration. Unknown fields are rejected so a
// typo fails at load instead of silently dropping a layer.
func ParseProfile(data []byte) (*Profile, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var profile Profile
	if err := decoder.Decode(&profile); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidProfile, err)
	}
	if strings.TrimSpace(profile.Name) == "" {
		return nil, fmt.Errorf("%w: profile has no name", ErrInvalidProfile)
	}
	for _, name := range profile.Bundles {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%w: profile %q lists an empty bundle name", ErrInvalidProfile, profile.Name)
		}
	}
	for _, requirement := range profile.Required {
		if strings.TrimSpace(requirement) == "" {
			return nil, fmt.Errorf("%w: profile %q lists an empty required entry", ErrInvalidProfile, profile.Name)
		}
	}
	return &profile, nil
}

// Resolve turns a profile into ordered composition layers by looking up every
// named bundle in the catalog. It reports an unknown bundle before any layer is
// applied, so a profile never composes with a silently missing layer.
func Resolve(profile *Profile, catalog *Catalog) ([]Layer, error) {
	if profile == nil {
		return nil, fmt.Errorf("%w: profile is nil", ErrInvalidProfile)
	}
	if catalog == nil {
		return nil, fmt.Errorf("%w: catalog is nil", ErrInvalidProfile)
	}
	layers := make([]Layer, 0, len(profile.Bundles)+1)
	for _, name := range profile.Bundles {
		bundle, err := catalog.Lookup(name)
		if err != nil {
			return nil, fmt.Errorf("profile %q: %w", profile.Name, err)
		}
		layers = append(layers, Layer{Source: "bundle/" + name, Patches: bundle.Patches})
	}
	if len(profile.Patches) > 0 {
		layers = append(layers, Layer{Source: "profile/" + profile.Name, Patches: profile.Patches})
	}
	return layers, nil
}

// Requirement is one parsed required-entry rule.
type Requirement struct {
	// ID is the entry path that must become ready.
	ID string
	// Name, when set, is the plugin name the entry must have.
	Name string
}

// ParseRequirement parses one required-entry rule. The accepted forms are "id"
// and "id:name"; the latter pins the plugin name as well.
func ParseRequirement(value string) (Requirement, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Requirement{}, fmt.Errorf("%w: empty requirement", ErrInvalidProfile)
	}
	requirement := Requirement{ID: trimmed}
	if index := strings.LastIndex(trimmed, loader.EntrySeparator); index >= 0 {
		// A path separator also addresses nested entries, so the name assertion is
		// only recognised when the text after the last separator names no group path.
		candidate := trimmed[index+len(loader.EntrySeparator):]
		if candidate != "" && !strings.Contains(candidate, loader.EntrySeparator) {
			requirement.ID = trimmed[:index]
			requirement.Name = candidate
		}
	}
	return requirement, nil
}

// ComposeProfile resolves a profile and composes its layers into one entry list.
// It returns the composed entries together with the skipped-patch diagnostics the
// composition produced.
func ComposeProfile(profile *Profile, catalog *Catalog) (ComposeResult, error) {
	layers, err := Resolve(profile, catalog)
	if err != nil {
		return ComposeResult{}, err
	}
	return Compose(layers), nil
}

// Ready is the outcome of starting a composed profile.
type Ready struct {
	// Profile is the profile name that was composed.
	Profile string
	// Tree is the running tree, or nil when composition failed before mounting.
	Tree *loader.Tree
	// Composition holds the skipped-patch diagnostics; a caller decides whether a
	// skipped overlay is fatal.
	Composition ComposeResult
	// Active lists entry ids that reached an active instance.
	Active []string
	// Pending lists entries still waiting for a dependency.
	Pending []string
	// Failed lists required entries that did not become ready. A non-empty list
	// blocks readiness.
	Failed []Failure
	// Siblings lists entries that are not required and did not become ready. They
	// are reported so a partial profile is visible, but they do not block
	// readiness.
	Siblings []Failure
	// Missing lists required entries that the profile did not compose at all.
	Missing []string
}

// Failure records one entry that did not become ready.
type Failure struct {
	// ID is the entry path.
	ID string
	// Reason is the instance error or lifecycle state that blocked readiness.
	Reason error
}

// Err reports the required entries that prevented readiness. A non-required
// failure does not appear here: siblings may fail without making the profile
// unusable, which is the difference the required list exists to draw.
func (r Ready) Err() error {
	var errs []error
	for _, failure := range r.Failed {
		errs = append(errs, fmt.Errorf("%w: %s: %w", ErrRequiredEntryFailed, failure.ID, failure.Reason))
	}
	for _, id := range r.Missing {
		errs = append(errs, fmt.Errorf("%w: %s is not composed", ErrRequiredEntryFailed, id))
	}
	return errors.Join(errs...)
}

// Mount composes, mounts and reports readiness for one profile against a bundle
// catalog and a plugin catalog.
//
// Every composed entry is mounted, including entries that are not required.
// Resolution and activation failures are reported differently: a declaration that
// cannot resolve (an unknown plugin name or a rejected configuration) fails the
// whole composition before anything mounts, while a plugin that resolves but
// fails to start is recorded per entry. A required entry that does not become
// ready blocks the result; a sibling that does not become ready is reported in
// Siblings without making the profile unusable. That split is the difference the
// required list exists to draw.
func Mount(ctx context.Context, profile *Profile, catalog *Catalog, plugins *loader.Catalog) (Ready, error) {
	if profile == nil {
		return Ready{}, fmt.Errorf("%w: profile is nil", ErrInvalidProfile)
	}
	if catalog == nil {
		return Ready{}, fmt.Errorf("%w: catalog is nil", ErrInvalidProfile)
	}
	if plugins == nil {
		return Ready{}, fmt.Errorf("%w: plugin catalog is nil", ErrInvalidProfile)
	}
	composed, err := ComposeProfile(profile, catalog)
	if err != nil {
		return Ready{}, err
	}

	required, err := parseRequirements(profile.Required)
	if err != nil {
		return Ready{}, err
	}
	requirements := map[string]Requirement{}
	for _, requirement := range required {
		requirements[requirement.ID] = requirement
	}

	// Resolve every declaration first: an unresolvable one is a composition error,
	// so a bad overlay never leaves a partially mounted tree behind.
	for _, entry := range composed.Entries {
		if _, err := loader.Resolve(&loader.Document{Entries: []loader.Options{entry}}, plugins); err != nil {
			return Ready{Profile: profile.Name, Composition: composed}, err
		}
	}

	tree := loader.NewTree(plugins)
	result := Ready{Profile: profile.Name, Tree: tree, Composition: composed}

	// Mount one entry at a time so a startup failure is attributable to its entry
	// instead of aborting the whole tree.
	mountFailures := map[string]error{}
	if err := mountEntries(tree, composed.Entries, mountFailures); err != nil {
		_ = tree.Close(ctx)
		return Ready{Profile: profile.Name, Composition: composed}, err
	}

	// An entry whose plugin failed to start is never published, so it is absent
	// from the tree. Both that case and a published-but-unready entry must be
	// classified, and both are reported by path.
	declared := map[string]bool{}
	for _, path := range tree.Paths() {
		declared[path] = true
	}

	for _, requirement := range required {
		if !declared[requirement.ID] {
			// Either the entry never composed, or its plugin failed to start. The
			// two are different failures and are reported differently.
			if mountErr, failed := mountFailures[requirement.ID]; failed {
				result.Failed = append(result.Failed, Failure{requirement.ID, mountErr})
			} else {
				result.Missing = append(result.Missing, requirement.ID)
			}
			continue
		}
		entry, ok := tree.Entry(requirement.ID)
		if !ok {
			result.Missing = append(result.Missing, requirement.ID)
			continue
		}
		if requirement.Name != "" && entry.Name() != requirement.Name {
			result.Failed = append(result.Failed, Failure{requirement.ID,
				fmt.Errorf("required entry name is %q, expected %q", entry.Name(), requirement.Name)})
		}
	}

	// Report every composed entry by outcome. Readiness is decided by state, and
	// whether an unready entry blocks the result depends only on whether it is
	// required.
	requiredSet := map[string]bool{}
	for _, requirement := range required {
		requiredSet[requirement.ID] = true
	}
	for _, path := range tree.Paths() {
		view, ok := tree.View(path)
		if !ok || view.Group {
			// A group carries no instance; its children decide readiness.
			continue
		}
		reason := failureFor(view, nil)
		if reason == nil {
			result.Active = append(result.Active, path)
			continue
		}
		if view.State == cordis.Pending && view.Err == nil {
			result.Pending = append(result.Pending, path)
		}
		if requiredSet[path] {
			result.Failed = append(result.Failed, Failure{path, reason})
		} else {
			result.Siblings = append(result.Siblings, Failure{path, reason})
		}
	}
	// A non-required entry whose plugin failed to start is absent from the tree,
	// so it is reported here instead of by the loop above.
	for _, entry := range composed.Entries {
		if entry.ID == "" || declared[entry.ID] || requiredSet[entry.ID] {
			continue
		}
		if mountErr, failed := mountFailures[entry.ID]; failed {
			result.Siblings = append(result.Siblings, Failure{entry.ID, mountErr})
		}
	}

	sort.Strings(result.Active)
	sort.Strings(result.Pending)
	sort.Strings(result.Missing)
	return result, result.Err()
}

// mountEntries mounts each composed top-level entry on its own, recording a
// startup failure against the entry path instead of aborting the mount.
func mountEntries(tree *loader.Tree, entries []loader.Options, failures map[string]error) error {
	for _, entry := range entries {
		document := loader.Document{Entries: []loader.Options{entry}}
		if err := tree.Mount(&document); err != nil {
			if entry.ID == "" {
				return err
			}
			failures[entry.ID] = err
		}
	}
	return nil
}

// failureFor reports why one entry is not ready, or nil when it is.
func failureFor(view loader.EntryView, mountErr error) error {
	if mountErr != nil {
		return mountErr
	}
	if view.Group {
		// A group entry carries no instance, so its children decide readiness.
		return nil
	}
	if !view.Runnable {
		return fmt.Errorf("entry is disabled or has no plugin")
	}
	if view.Err != nil {
		return view.Err
	}
	switch view.State {
	case cordis.Active:
		return nil
	case cordis.Pending:
		return fmt.Errorf("entry is pending on an unavailable dependency")
	default:
		return fmt.Errorf("entry is %s", view.State)
	}
}

// parseRequirements parses every required-entry rule, rejecting duplicates so a
// profile cannot state two different expectations for one entry.
func parseRequirements(values []string) ([]Requirement, error) {
	result := make([]Requirement, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		requirement, err := ParseRequirement(value)
		if err != nil {
			return nil, err
		}
		if seen[requirement.ID] {
			return nil, fmt.Errorf("%w: entry %q is required twice", ErrInvalidProfile, requirement.ID)
		}
		seen[requirement.ID] = true
		result = append(result, requirement)
	}
	return result, nil
}

// Close releases a started profile. A profile that never mounted has no tree.
func (r Ready) Close(ctx context.Context) error {
	if r.Tree == nil {
		return nil
	}
	return r.Tree.Close(ctx)
}
