package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gosomea/deepseek-harness-go/loader"
)

var (
	// ErrUnknownBundle reports a profile naming a bundle the host did not register.
	ErrUnknownBundle = errors.New("app: unknown bundle")
	// ErrInvalidBundle reports a bundle declaration the composer cannot apply.
	ErrInvalidBundle = errors.New("app: invalid bundle")
	// ErrPatchSkipped reports a patch that matched no entry, or whose name did not
	// match its target. The layer still applies; the skipped patch is reported so a
	// typo in an overlay cannot pass unnoticed.
	ErrPatchSkipped = errors.New("app: patch skipped")
)

// Patch is one overlay operation on an entry list. A patch carrying Insert adds
// entries; otherwise it overrides the fields it names on the entry addressed by
// ID. A nil field is left unchanged, so a layer states only what it changes.
type Patch struct {
	// ID addresses the entry this patch changes. It is required unless Insert is set.
	ID string `json:"id,omitempty"`
	// Name, when set, must equal the target's current name; a mismatch skips the
	// patch instead of applying it to an entry the layer did not mean to touch.
	Name *string `json:"name,omitempty"`
	// Insert holds entries appended to the target group, or to the root when ID is
	// empty.
	Insert []loader.Options `json:"insert,omitempty"`
	// Config replaces the target's raw configuration payload when non-nil.
	Config *json.RawMessage `json:"config,omitempty"`
	// Inject replaces the target's dependency list when non-nil.
	Inject *loader.Inject `json:"inject,omitempty"`
	// Disabled sets the target's disabled flag when non-nil.
	Disabled *bool `json:"disabled,omitempty"`
}

// Bundle is a named, ordered group of patches. Bundles are registered by the host
// as ordinary Go values; nothing here loads code at run time.
type Bundle struct {
	// Name identifies the bundle in a profile's layer list.
	Name string
	// Patches apply in list order when the bundle's layer is composed.
	Patches []Patch
}

// Catalog holds the bundles a host registered, in registration order.
type Catalog struct {
	order  []string
	byName map[string]Bundle
}

// NewCatalog creates an empty bundle catalog.
func NewCatalog() *Catalog { return &Catalog{byName: map[string]Bundle{}} }

// Register adds a bundle. An empty name or a duplicate name is rejected: a
// profile addresses layers by name, so ambiguity there would make the composed
// tree depend on registration order.
func (c *Catalog) Register(bundle Bundle) error {
	name := strings.TrimSpace(bundle.Name)
	if name == "" {
		return fmt.Errorf("%w: bundle name is empty", ErrInvalidBundle)
	}
	if c.byName == nil {
		c.byName = map[string]Bundle{}
	}
	if _, exists := c.byName[name]; exists {
		return fmt.Errorf("%w: bundle %q is already registered", ErrInvalidBundle, name)
	}
	c.byName[name] = bundle
	c.order = append(c.order, name)
	return nil
}

// Lookup returns a registered bundle.
func (c *Catalog) Lookup(name string) (Bundle, error) {
	bundle, ok := c.byName[name]
	if !ok {
		return Bundle{}, fmt.Errorf("%w: %q", ErrUnknownBundle, name)
	}
	return bundle, nil
}

// Names lists registered bundle names in registration order.
func (c *Catalog) Names() []string { return append([]string(nil), c.order...) }

// Layer is one composition layer: a bundle's patches, or a profile's own
// patches. Source labels the layer in diagnostics.
type Layer struct {
	// Source names where the layer came from, for diagnostics.
	Source string
	// Patches apply in order within this layer.
	Patches []Patch
}

// ComposeResult is a composed entry list together with the diagnostics produced
// while composing it.
type ComposeResult struct {
	// Entries are the composed top-level entries, in declaration order.
	Entries []loader.Options
	// Skipped lists patches that matched nothing or a mismatched name, in order.
	Skipped []SkippedPatch
}

// Err joins every skipped-patch diagnostic so a caller can reject a composition
// that silently dropped an overlay.
func (r ComposeResult) Err() error {
	errs := make([]error, 0, len(r.Skipped))
	for _, skipped := range r.Skipped {
		errs = append(errs, skipped.Err())
	}
	return errors.Join(errs...)
}

// SkippedPatch records one patch that did not apply, with the reason.
type SkippedPatch struct {
	// Source is the layer the patch came from.
	Source string
	// ID is the entry the patch addressed.
	ID string
	// Reason explains why the patch did not apply.
	Reason string
}

// Err reports the skipped patch as an error wrapping ErrPatchSkipped.
func (s SkippedPatch) Err() error {
	return fmt.Errorf("%w: layer %q patch %q: %s", ErrPatchSkipped, s.Source, s.ID, s.Reason)
}

// node is the mutable in-memory form of one entry during composition.
//
// A group's children live here as real Go values. Decoding a group's payload into
// a fresh slice for every lookup would make a patch to a nested entry write into a
// throwaway copy, so a later layer's override of a nested entry would appear to
// succeed while changing nothing.
type node struct {
	opts     loader.Options
	children []*node
}

// decode turns a declared entry list into mutable nodes.
func decode(entries []loader.Options) ([]*node, error) {
	nodes := make([]*node, 0, len(entries))
	for _, entry := range entries {
		current := &node{opts: entry}
		if entry.IsGroup() && len(entry.Config) > 0 {
			var declared []loader.Options
			if err := json.Unmarshal(entry.Config, &declared); err != nil {
				return nil, fmt.Errorf("%w: group %q children: %s", ErrInvalidBundle, entry.ID, err)
			}
			children, err := decode(declared)
			if err != nil {
				return nil, err
			}
			current.children = children
		}
		nodes = append(nodes, current)
	}
	return nodes, nil
}

// encode renders nodes back into a declared entry list, re-encoding each group's
// children so every patched nested entry reaches the result.
func encode(nodes []*node) ([]loader.Options, error) {
	entries := make([]loader.Options, 0, len(nodes))
	for _, current := range nodes {
		entry := current.opts
		if current.opts.IsGroup() {
			children, err := encode(current.children)
			if err != nil {
				return nil, err
			}
			encoded, err := json.Marshal(children)
			if err != nil {
				return nil, fmt.Errorf("%w: group %q children: %s", ErrInvalidBundle, entry.ID, err)
			}
			entry.Config = encoded
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// cloneNodes returns a deep copy of one node list, so patching a layer never
// mutates the list it started from.
func cloneNodes(nodes []*node) []*node {
	if nodes == nil {
		return nil
	}
	cloned := make([]*node, 0, len(nodes))
	for _, current := range nodes {
		copyOf := &node{opts: current.opts}
		copyOf.opts.Inject = append(loader.Inject(nil), current.opts.Inject...)
		if current.opts.Config != nil {
			copyOf.opts.Config = append(json.RawMessage(nil), current.opts.Config...)
		}
		if current.opts.Disabled != nil {
			value := *current.opts.Disabled
			copyOf.opts.Disabled = &value
		}
		if current.opts.Group != nil {
			value := *current.opts.Group
			copyOf.opts.Group = &value
		}
		copyOf.children = cloneNodes(current.children)
		cloned = append(cloned, copyOf)
	}
	return cloned
}

// Compose applies every layer in order over an empty entry list.
//
// Each layer starts from the previous layer's result, so a later layer overrides
// an earlier layer's field on the same entry. The input is never mutated:
// re-applying a layer from the same starting point yields the same tree, which is
// what makes an overlay reproducible rather than order-dependent by accident.
func Compose(layers []Layer) ComposeResult {
	result := ComposeResult{}
	root := []*node{}
	for _, layer := range layers {
		next, err := composeLayer(layer, root, &result)
		if err != nil {
			// A structurally invalid group payload cannot be composed further; the
			// layer is reported and the previous tree is kept.
			result.Skipped = append(result.Skipped, SkippedPatch{layer.Source, "", err.Error()})
			continue
		}
		root = next
	}
	entries, err := encode(root)
	if err != nil {
		result.Skipped = append(result.Skipped, SkippedPatch{"", "", err.Error()})
		return result
	}
	result.Entries = entries
	return result
}

// composeLayer applies one layer's patches over a detached copy of root.
func composeLayer(layer Layer, root []*node, result *ComposeResult) ([]*node, error) {
	composed := cloneNodes(root)
	for _, patch := range layer.Patches {
		if len(patch.Insert) > 0 {
			inserted, err := decode(cloneOptions(patch.Insert))
			if err != nil {
				result.Skipped = append(result.Skipped, SkippedPatch{layer.Source, patch.ID, err.Error()})
				continue
			}
			if patch.ID == "" {
				composed = append(composed, inserted...)
				continue
			}
			target, ok := findGroup(composed, patch.ID)
			if !ok {
				result.Skipped = append(result.Skipped, SkippedPatch{layer.Source, patch.ID, "insert target is not a group entry"})
				continue
			}
			target.children = append(target.children, inserted...)
			continue
		}
		if patch.ID == "" {
			result.Skipped = append(result.Skipped, SkippedPatch{layer.Source, "", "id is required for a non-insert patch"})
			continue
		}
		target, ok := findEntry(composed, patch.ID)
		if !ok {
			result.Skipped = append(result.Skipped, SkippedPatch{layer.Source, patch.ID, "entry not found"})
			continue
		}
		if patch.Name != nil && *patch.Name != target.opts.Name {
			result.Skipped = append(result.Skipped, SkippedPatch{layer.Source, patch.ID,
				fmt.Sprintf("name mismatch: expected %q, found %q", *patch.Name, target.opts.Name)})
			continue
		}
		if patch.Config != nil {
			target.opts.Config = append(json.RawMessage(nil), (*patch.Config)...)
		}
		if patch.Inject != nil {
			target.opts.Inject = append(loader.Inject(nil), (*patch.Inject)...)
		}
		if patch.Disabled != nil {
			value := *patch.Disabled
			target.opts.Disabled = &value
		}
	}
	return composed, nil
}

// cloneOptions returns a copy of one declared entry list.
func cloneOptions(entries []loader.Options) []loader.Options {
	if entries == nil {
		return nil
	}
	cloned := make([]loader.Options, 0, len(entries))
	for _, entry := range entries {
		copyOfEntry := entry
		copyOfEntry.Inject = append(loader.Inject(nil), entry.Inject...)
		if entry.Config != nil {
			copyOfEntry.Config = append(json.RawMessage(nil), entry.Config...)
		}
		cloned = append(cloned, copyOfEntry)
	}
	return cloned
}

// findEntry returns the entry addressed by a patch id. The reference indexes every
// declared entry, nested ones included, by its local id, so an id resolves first
// as a full path and then as a local id anywhere in the tree.
func findEntry(nodes []*node, id string) (*node, bool) {
	if target, ok := findByPath(nodes, id); ok {
		return target, true
	}
	return findByLocalID(nodes, id)
}

// findByLocalID returns the first entry whose local id matches, depth first.
func findByLocalID(nodes []*node, id string) (*node, bool) {
	for _, current := range nodes {
		if current.opts.ID == id {
			return current, true
		}
	}
	for _, current := range nodes {
		if target, ok := findByLocalID(current.children, id); ok {
			return target, true
		}
	}
	return nil, false
}

// findByPath returns the entry addressed by a full path.
func findByPath(nodes []*node, path string) (*node, bool) {
	segments := strings.Split(path, loader.EntrySeparator)
	current := nodes
	for index, segment := range segments {
		var found *node
		for _, candidate := range current {
			if candidate.opts.ID == segment {
				found = candidate
				break
			}
		}
		if found == nil {
			return nil, false
		}
		if index == len(segments)-1 {
			return found, true
		}
		current = found.children
	}
	return nil, false
}

// findGroup returns the group entry addressed by a path or local id.
func findGroup(nodes []*node, id string) (*node, bool) {
	target, ok := findEntry(nodes, id)
	if !ok || !target.opts.IsGroup() {
		return nil, false
	}
	return target, true
}

// SortSkipped orders skipped-patch diagnostics for stable reporting.
func SortSkipped(skipped []SkippedPatch) []SkippedPatch {
	ordered := append([]SkippedPatch(nil), skipped...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Source != ordered[j].Source {
			return ordered[i].Source < ordered[j].Source
		}
		return ordered[i].ID < ordered[j].ID
	})
	return ordered
}
