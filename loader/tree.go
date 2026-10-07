package loader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// EntrySeparator joins a nested entry's local id to its enclosing group's id,
// so one path addresses an entry anywhere in the tree.
const EntrySeparator = ":"

var (
	// ErrTreeClosed rejects mounting into a tree that was already closed. A
	// closed tree never publishes a new entry.
	ErrTreeClosed = errors.New("loader: tree is closed")
	// ErrEntryNotFound reports that no entry matches the requested path.
	ErrEntryNotFound = errors.New("loader: entry not found")
)

// Tree mounts configured entries into a plugin tree. It owns one cordis root
// context, the entries mounted into it and the binding from each entry path to
// the plugin instance that path created.
//
// Tree is safe for concurrent lookup. Load and Close serialize against each
// other, so a close during a load cannot publish the loading entry.
type Tree struct {
	// loadMu serializes Load and Mount so two loads cannot interleave their
	// declarations. Holding it does not block lookups.
	loadMu sync.Mutex
	// mu guards the mounting maps and the closed flag.
	mu sync.Mutex

	catalog *Catalog
	ctx     *cordis.Context
	root    *Group

	entries map[string]*Entry
	groups  map[string]*Group
	fibers  map[string]*cordis.Fiber
	owners  map[uint64]string
	order   []string

	closed bool
}

// NewTree creates an empty tree with its own root context. The returned tree
// owns that context: closing the tree closes the root and waits for every
// mounted entry to release what it acquired.
func NewTree(catalog *Catalog, options ...cordis.Option) *Tree {
	tree := &Tree{
		catalog: catalog,
		ctx:     cordis.New(options...),
		entries: map[string]*Entry{},
		groups:  map[string]*Group{},
		fibers:  map[string]*cordis.Fiber{},
		owners:  map[uint64]string{},
	}
	tree.root = &Group{tree: tree}
	tree.groups[""] = tree.root
	return tree
}

// Context returns the root context every mounted entry lives under.
func (t *Tree) Context() *cordis.Context { return t.ctx }

// Root returns the root group. It is never nil.
func (t *Tree) Root() *Group { return t.root }

// Closed reports whether the tree was closed.
func (t *Tree) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// Load parses a document and mounts every entry it declares. Mounting an entry
// registers its plugin with the tree's root context, so the plugin's Apply runs
// and may acquire resources; Close releases them.
//
// A group entry's raw config holds its children, so one document can nest
// groups. Children are addressed by the path group:child.
func (t *Tree) Load(data []byte) error {
	document, err := ParseDocument(data)
	if err != nil {
		return err
	}
	return t.Mount(document)
}

// Mount mounts an already parsed document. Load is the byte-level entry.
func (t *Tree) Mount(document *Document) error {
	if document == nil {
		return fmt.Errorf("%w: document is nil", ErrInvalidConfig)
	}
	t.loadMu.Lock()
	defer t.loadMu.Unlock()

	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return ErrTreeClosed
	}

	// Validate the whole document before publishing anything. A rejected document
	// then leaves the tree exactly as it was, instead of keeping the entries that
	// happened to precede the offending one.
	if err := validateEntries("", document.Entries, map[string]bool{}); err != nil {
		return err
	}

	return t.mountInto(t.root, "", document.Entries)
}

// mountInto mounts one group's entries in declaration order. Every entry is
// resolved before it is registered, so a rejected entry never becomes visible.
func (t *Tree) mountInto(group *Group, prefix string, options []Options) error {
	for _, declared := range options {
		local := strings.TrimSpace(declared.ID)
		if local == "" {
			return fmt.Errorf("%w: entry in group %q has no id", ErrInvalidConfig, group.id)
		}
		if strings.Contains(local, EntrySeparator) {
			return fmt.Errorf("%w: entry id %q must not contain %q", ErrInvalidConfig, local, EntrySeparator)
		}
		full := local
		if prefix != "" {
			full = prefix + EntrySeparator + local
		}
		if err := t.mountEntry(group, full, declared); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tree) mountEntry(group *Group, full string, declared Options) error {
	// The full path becomes the entry's identity, matching the reference where a
	// nested entry reports its prefixed id rather than its local one.
	effective := declared
	effective.ID = full
	entry, err := t.resolveOne(effective)
	if err != nil {
		return err
	}

	var fiber *cordis.Fiber
	if entry.Runnable() {
		fiber, err = t.ctx.Plugin(entry.Plugin(), entry.Config())
		if err != nil {
			return fmt.Errorf("entry %q: %w", full, err)
		}
	}

	// Publishing the entry and binding its instance happen under one lock, so a
	// close racing this mount either publishes both or neither. An instance whose
	// entry cannot be published is disposed rather than left without an owner.
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		if fiber != nil {
			if disposeErr := fiber.Dispose(); disposeErr != nil {
				return errors.Join(ErrTreeClosed, disposeErr)
			}
		}
		return ErrTreeClosed
	}
	t.entries[full] = entry
	entry.setGroup(group)
	// Declaration order is what both dump shapes render and what Apply reports, so
	// every published path is recorded here.
	t.order = append(t.order, full)
	group.children = append(group.children, entry)
	if fiber != nil {
		t.fibers[full] = fiber
		t.owners[fiber.ID()] = full
	}
	t.mu.Unlock()

	if entry.IsGroup() {
		return t.mountGroup(group, full, entry, declared)
	}
	return nil
}

// mountGroup creates the nested group an entry declares and mounts its children.
func (t *Tree) mountGroup(parent *Group, full string, entry *Entry, declared Options) error {
	nested := &Group{tree: t, owner: entry, id: full, parent: parent}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrTreeClosed
	}
	t.groups[full] = nested
	t.mu.Unlock()

	children, err := declared.groupChildren()
	if err != nil {
		return fmt.Errorf("group %q: %w", full, err)
	}
	return t.mountInto(nested, full, children)
}

// groupChildren decodes the child entries a group entry carries in its config.
// A group without a config declares no children.
func (o Options) groupChildren() ([]Options, error) {
	if !o.IsGroup() {
		return nil, nil
	}
	if len(o.Config) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(o.Config)))
	decoder.DisallowUnknownFields()
	var children []Options
	if err := decoder.Decode(&children); err != nil {
		return nil, fmt.Errorf("%w: group children: %s", ErrInvalidConfig, err)
	}
	return children, nil
}

// resolveOne resolves a single entry through the same codec the flat path uses,
// so identity, configuration and failure classes stay identical.
func (t *Tree) resolveOne(options Options) (*Entry, error) {
	entries, err := Resolve(&Document{Entries: []Options{options}}, t.catalog)
	if err != nil {
		return nil, err
	}
	if len(entries) != 1 {
		return nil, fmt.Errorf("%w: entry %q resolved to %d entries", ErrInvalidConfig, options.ID, len(entries))
	}
	return entries[0], nil
}

// validateEntries checks the whole declaration before anything is mounted: local
// ids within one group, path separators and nested group children. Validating up
// front makes a rejected document a no-op, so a caller never observes a partial
// tree built from the entries that happened to precede the offending one.
func validateEntries(prefix string, options []Options, seen map[string]bool) error {
	for _, declared := range options {
		local := strings.TrimSpace(declared.ID)
		if local == "" {
			return fmt.Errorf("%w: entry in group %q has no id", ErrInvalidConfig, prefix)
		}
		if strings.Contains(local, EntrySeparator) {
			return fmt.Errorf("%w: entry id %q must not contain %q", ErrInvalidConfig, local, EntrySeparator)
		}
		full := local
		if prefix != "" {
			full = prefix + EntrySeparator + local
		}
		if seen[full] {
			return fmt.Errorf("%w: %q", ErrDuplicateID, full)
		}
		seen[full] = true
		if !declared.IsGroup() {
			continue
		}
		children, err := declared.groupChildren()
		if err != nil {
			return fmt.Errorf("group %q: %w", full, err)
		}
		if err := validateEntries(full, children, seen); err != nil {
			return err
		}
	}
	return nil
}

// Entry returns the entry at a path, including entries inside nested groups.
func (t *Tree) Entry(id string) (*Entry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[id]
	return entry, ok
}

// Group returns a group by path. The root group is addressed by the empty id.
func (t *Tree) Group(id string) (*Group, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	group, ok := t.groups[id]
	return group, ok
}

// Fiber returns the plugin instance an entry path created. A disabled, group or
// unresolved entry has no instance, so it reports false.
func (t *Tree) Fiber(id string) (*cordis.Fiber, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fiber, ok := t.fibers[id]
	return fiber, ok
}

// Locate returns the entry path that owns a plugin instance. An instance the tree
// did not mount has no owner, so it reports false.
func (t *Tree) Locate(fiber *cordis.Fiber) (string, bool) {
	if fiber == nil {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	id, ok := t.owners[fiber.ID()]
	return id, ok
}

// Entries returns every mounted entry in declaration order, nested entries
// included.
func (t *Tree) Entries() []*Entry {
	t.mu.Lock()
	paths := append([]string(nil), t.order...)
	t.mu.Unlock()
	result := make([]*Entry, 0, len(paths))
	for _, path := range paths {
		if entry, ok := t.Entry(path); ok {
			result = append(result, entry)
		}
	}
	return result
}

// Paths returns every mounted entry path in declaration order.
func (t *Tree) Paths() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.order...)
}

// Close disposes the root context and waits for every mounted entry to release
// what it acquired. It is idempotent and safe to call from outside a lifecycle
// callback.
func (t *Tree) Close(ctx context.Context) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	return t.ctx.Close(ctx)
}
