package loader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

var (
	// ErrEntryNotGroup reports a move whose target is not a group.
	ErrEntryNotGroup = errors.New("loader: target is not a group")
	// ErrMoveIntoSubtree reports a move that would make an entry its own ancestor.
	ErrMoveIntoSubtree = errors.New("loader: cannot move an entry into its own subtree")
	// ErrGroupNotUpdatable reports an update addressed at a group entry, whose
	// content is its children rather than a plugin configuration.
	ErrGroupNotUpdatable = errors.New("loader: a group entry is updated through its children")
)

// Update is a partial change to one mounted entry. A nil field leaves that aspect
// unchanged, so a caller changes only what it names.
//
// Replacing a field is the only operation the codec supports: an absent field and
// a cleared field are indistinguishable in the document, so an update never
// returns a field to absent.
type Update struct {
	// Name replaces the plugin name when non-nil. Naming a different plugin
	// mounts that plugin and therefore replaces the running instance.
	Name *string
	// Config replaces the raw configuration payload when non-nil. A non-nil but
	// empty slice clears the payload.
	Config *json.RawMessage
	// Inject replaces the required-service list when non-nil.
	Inject *Inject
	// Disabled sets the disabled flag when non-nil.
	Disabled *bool
}

// EntryView is a read-only rendering of one entry's declared identity and live
// state, for callers that report or compare tree contents.
type EntryView struct {
	// ID is the entry's full path inside the tree.
	ID string
	// Name is the catalog name of the entry's plugin; empty for a group entry.
	Name string
	// Disabled reports whether the declaration disabled this entry.
	Disabled bool
	// Group marks an entry that groups other entries instead of running a plugin.
	Group bool
	// Runnable reports whether this entry has a plugin to run.
	Runnable bool
	// State is the live instance state; empty when the entry has no instance.
	State cordis.State
	// Err is the instance's latest failure, if any.
	Err error
}

// View returns one entry's declared identity and live state.
func (t *Tree) View(id string) (EntryView, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.viewLocked(id)
}

// viewLocked reports one path's view while the tree lock is held.
func (t *Tree) viewLocked(id string) (EntryView, bool) {
	entry, ok := t.entries[id]
	if !ok {
		return EntryView{}, false
	}
	view := EntryView{
		ID:       entry.ID(),
		Name:     entry.Name(),
		Disabled: entry.Disabled(),
		Group:    entry.IsGroup(),
		Runnable: entry.Runnable(),
	}
	if fiber, mounted := t.fibers[id]; mounted {
		view.State = fiber.State()
		view.Err = fiber.Err()
	}
	return view, true
}

// Update replaces the named aspects of one entry's declaration. The candidate
// declaration is resolved before anything changes, so an unknown plugin name or a
// configuration the plugin rejects leaves the running tree untouched.
//
// Once the candidate resolves, the instance is reconciled: a configuration
// change reloads the same instance and keeps its identity, while a name change or
// a disable retires the instance and mounts, or omits, a replacement.
func (t *Tree) Update(id string, update Update) (EntryView, error) {
	t.loadMu.Lock()
	defer t.loadMu.Unlock()

	previous, err := t.entryForUpdate(id)
	if err != nil {
		return EntryView{}, err
	}
	declared := previous.Options()
	declared.ID = id
	if update.Name != nil {
		declared.Name = *update.Name
	}
	if update.Config != nil {
		declared.Config = append(json.RawMessage(nil), (*update.Config)...)
	}
	if update.Inject != nil {
		declared.Inject = append(Inject(nil), (*update.Inject)...)
	}
	if update.Disabled != nil {
		value := *update.Disabled
		declared.Disabled = &value
	}
	return t.applyDeclared(id, previous, declared)
}

// SetDisabled enables or disables one entry. A disabled entry keeps its place and
// identity in the tree but has no instance; re-enabling mounts a fresh one.
func (t *Tree) SetDisabled(id string, disabled bool) (EntryView, error) {
	return t.Update(id, Update{Disabled: &disabled})
}

// Enable disables nothing and mounts the entry: shorthand for SetDisabled(id, false).
func (t *Tree) Enable(id string) (EntryView, error) { return t.SetDisabled(id, false) }

// Disable stops and unmounts the entry's instance while keeping its declaration:
// shorthand for SetDisabled(id, true).
func (t *Tree) Disable(id string) (EntryView, error) { return t.SetDisabled(id, true) }

// Move relocates an entry under another group. An entry's full path is its
// identity, so a move changes that path and every descendant path; a caller that
// cached a path must resolve the entry again. The instance is preserved, because
// moving changes where an entry sits, not which plugin it runs.
func (t *Tree) Move(id string, parent string) (EntryView, error) {
	t.loadMu.Lock()
	defer t.loadMu.Unlock()

	if id == "" {
		return EntryView{}, fmt.Errorf("%w: empty entry path", ErrInvalidConfig)
	}
	// The target must not be the entry itself or anything under it, or the move
	// would detach the subtree from the tree it is being moved within.
	if parent == id || (id != "" && strings.HasPrefix(parent, id+EntrySeparator)) {
		return EntryView{}, fmt.Errorf("%w: %q into %q", ErrMoveIntoSubtree, id, parent)
	}

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return EntryView{}, ErrTreeClosed
	}
	entry, exists := t.entries[id]
	if !exists {
		t.mu.Unlock()
		return EntryView{}, fmt.Errorf("%w: %q", ErrEntryNotFound, id)
	}
	target, targetExists := t.groups[parent]
	source, sourceExists := t.groups[groupOf(id)]
	t.mu.Unlock()
	if !targetExists {
		return EntryView{}, fmt.Errorf("%w: group %q", ErrEntryNotFound, parent)
	}
	if !sourceExists {
		return EntryView{}, fmt.Errorf("%w: owning group of %q", ErrEntryNotFound, id)
	}
	if source == target {
		return t.viewOrErr(id)
	}

	local := localID(id)
	newPath := local
	if parent != "" {
		newPath = parent + EntrySeparator + local
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return EntryView{}, ErrTreeClosed
	}
	if _, taken := t.entries[newPath]; taken {
		return EntryView{}, fmt.Errorf("%w: %q", ErrDuplicateID, newPath)
	}

	// Rewrite the moved paths longest-first, so moving a nested entry never lands
	// on a path a not-yet-moved descendant still occupies.
	moved := []string{id}
	if entry.IsGroup() {
		prefix := id + EntrySeparator
		for _, path := range t.order {
			if strings.HasPrefix(path, prefix) {
				moved = append(moved, path)
			}
		}
	}
	sort.Slice(moved, func(i, j int) bool { return len(moved[i]) > len(moved[j]) })
	t.rekeyLocked(moved, func(path string) string {
		return newPath + strings.TrimPrefix(path, id)
	})
	source.children = removeEntry(source.children, entry)
	target.children = append(target.children, entry)
	entry.setGroup(target)
	view, _ := t.viewLocked(newPath)
	return view, nil
}

// Remove stops and removes one entry together with its descendants.
func (t *Tree) Remove(id string) error {
	t.loadMu.Lock()
	defer t.loadMu.Unlock()
	return t.removePath(id)
}

// ApplyResult reports what one Apply changed. Applying a document is not
// transactional: entries are reconciled one at a time, so a failure leaves the
// entries that already succeeded in their new state. These lists name every path
// by outcome, which is what lets a caller report the tree that actually exists
// instead of promising a rollback that did not happen.
type ApplyResult struct {
	// Added lists paths the document introduced.
	Added []string
	// Updated lists paths whose declaration changed.
	Updated []string
	// Removed lists paths the document no longer declares.
	Removed []string
	// Unchanged lists paths whose declaration already matched.
	Unchanged []string
	// Failed lists paths whose reconciliation returned an error. Each keeps the
	// state the failure left it in, reported per path rather than rolled back.
	Failed []string
	// Errors holds one error per failed path, in the same order as Failed.
	Errors []error
}

// Err joins every per-path failure, so a caller can report a partial apply and
// match the underlying sentinels with errors.Is.
func (r ApplyResult) Err() error { return errors.Join(r.Errors...) }

// Apply parses a document and reconciles the tree to match it: declared entries
// are added or updated, and entries the document omits are removed.
//
// Parsing and structural validation both complete before anything changes, so a
// malformed document leaves the running tree exactly as it was. Reconciliation
// itself is not transactional: each entry applies on its own, a failure is
// recorded against its path, and the remaining entries still apply.
func (t *Tree) Apply(data []byte) (ApplyResult, error) {
	document, err := ParseDocument(data)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := validateEntries("", document.Entries, map[string]bool{}); err != nil {
		return ApplyResult{}, err
	}

	t.loadMu.Lock()
	defer t.loadMu.Unlock()
	if t.Closed() {
		return ApplyResult{}, ErrTreeClosed
	}

	order := make([]string, 0, len(document.Entries))
	declared := make(map[string]Options, len(document.Entries))
	flattenDeclaration("", document.Entries, &order, declared)

	var result ApplyResult
	// Remove first, so a path reused with different contents starts from a clean
	// declaration instead of being merged onto the retired one. Removing a group
	// also removes its children, so a child is skipped when an ancestor already
	// took it and is reported as removed under that ancestor.
	removedGroups := make([]string, 0, 4)
	for _, path := range t.Paths() {
		if _, kept := declared[path]; kept {
			continue
		}
		coveredByRemovedGroup := false
		for _, group := range removedGroups {
			if strings.HasPrefix(path, group+EntrySeparator) {
				coveredByRemovedGroup = true
				break
			}
		}
		if coveredByRemovedGroup {
			continue
		}
		if _, exists := t.Entry(path); !exists {
			continue
		}
		if entry, ok := t.Entry(path); ok && entry.IsGroup() {
			removedGroups = append(removedGroups, path)
		}
		if err := t.removePath(path); err != nil {
			result.Failed = append(result.Failed, path)
			result.Errors = append(result.Errors, fmt.Errorf("remove %q: %w", path, err))
			continue
		}
		result.Removed = append(result.Removed, path)
	}

	for _, path := range order {
		options := declared[path]
		options.ID = path
		previous, exists := t.Entry(path)
		if !exists {
			if err := t.addEntry(path, options); err != nil {
				result.Failed = append(result.Failed, path)
				result.Errors = append(result.Errors, err)
				continue
			}
			result.Added = append(result.Added, path)
			continue
		}
		if previous.IsGroup() {
			// A group carries no instance and no configuration of its own; its
			// children are reconciled as their own paths.
			result.Unchanged = append(result.Unchanged, path)
			continue
		}
		if sameDeclaration(previous.Options(), options) {
			result.Unchanged = append(result.Unchanged, path)
			continue
		}
		if _, err := t.applyDeclared(path, previous, options); err != nil {
			result.Failed = append(result.Failed, path)
			result.Errors = append(result.Errors, err)
			continue
		}
		result.Updated = append(result.Updated, path)
	}
	return result, result.Err()
}

// flattenDeclaration walks a document's declarations depth-first, recording each
// entry's full path in declaration order and its options by path.
func flattenDeclaration(prefix string, options []Options, order *[]string, declared map[string]Options) {
	for _, item := range options {
		full := item.ID
		if prefix != "" {
			full = prefix + EntrySeparator + item.ID
		}
		*order = append(*order, full)
		declared[full] = item
		if !item.IsGroup() {
			continue
		}
		children, err := item.groupChildren()
		if err != nil {
			// validateEntries already rejected malformed children, so an error
			// here cannot occur; skipping keeps this walk total.
			continue
		}
		flattenDeclaration(full, children, order, declared)
	}
}

// entryForUpdate resolves the entry an update addresses, rejecting a missing path
// and a group entry.
func (t *Tree) entryForUpdate(id string) (*Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTreeClosed
	}
	entry, ok := t.entries[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrEntryNotFound, id)
	}
	if entry.IsGroup() {
		return nil, fmt.Errorf("%w: %q", ErrGroupNotUpdatable, id)
	}
	return entry, nil
}

// applyDeclared resolves a candidate declaration and reconciles its instance.
// Resolution runs first, so a rejected declaration never reaches the tree.
func (t *Tree) applyDeclared(id string, previous *Entry, declared Options) (EntryView, error) {
	candidate, err := t.resolveOne(declared)
	if err != nil {
		return EntryView{}, err
	}
	if sameDeclaration(previous.Options(), declared) {
		return t.viewOrErr(id)
	}
	return t.reconcileEntry(id, previous, candidate)
}

// addEntry mounts one newly declared entry. It publishes the declaration and, for
// a runnable entry, starts its instance; children follow in declaration order and
// find their parent group already present.
func (t *Tree) addEntry(id string, declared Options) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrTreeClosed
	}
	parentID := groupOf(id)
	group, ok := t.groups[parentID]
	t.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: group %q", ErrEntryNotFound, parentID)
	}
	return t.mountEntry(group, id, declared)
}

// reconcileEntry brings one path's declaration and instance in line.
//
// The declaration is published before the instance is reconciled, and the
// instance is reconciled outside the tree lock because mounting and cleanup run
// plugin code. A close landing in between retires the fresh instance rather than
// publishing one nobody owns.
func (t *Tree) reconcileEntry(id string, previous, next *Entry) (EntryView, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return EntryView{}, ErrTreeClosed
	}
	oldFiber := t.fibers[id]
	// A configuration-only change keeps the same plugin, so the instance is
	// reloaded in place and its identity survives.
	reloadInPlace := next.Runnable() && oldFiber != nil && next.Name() == previous.Name()
	t.entries[id] = next
	t.replaceChildLocked(previous, next)
	if !reloadInPlace {
		delete(t.fibers, id)
		if oldFiber != nil {
			delete(t.owners, oldFiber.ID())
		}
	}
	t.mu.Unlock()

	var errs []error
	switch {
	case reloadInPlace:
		errs = append(errs, oldFiber.Update(next.Config()))
	case !next.Runnable():
		if oldFiber != nil {
			errs = append(errs, oldFiber.Dispose())
		}
	default:
		if oldFiber != nil {
			errs = append(errs, oldFiber.Dispose())
		}
		fiber, mountErr := t.ctx.Plugin(next.Plugin(), next.Config())
		errs = append(errs, mountErr)
		if fiber != nil {
			t.mu.Lock()
			if t.closed {
				t.mu.Unlock()
				errs = append(errs, ErrTreeClosed, fiber.Dispose())
			} else {
				t.fibers[id] = fiber
				t.owners[fiber.ID()] = id
				t.mu.Unlock()
			}
		}
	}
	view, _ := t.View(id)
	return view, errors.Join(errs...)
}

// replaceChildLocked swaps a group's child pointer in place, preserving the
// declaration order an update must not disturb.
func (t *Tree) replaceChildLocked(previous, next *Entry) {
	for _, group := range t.groups {
		for index, child := range group.children {
			if child == previous {
				group.children[index] = next
				return
			}
		}
	}
}

// rekeyLocked rewrites the map keys of one or more paths. Every map that is keyed
// by path moves together, so no lookup can observe a half-moved entry.
func (t *Tree) rekeyLocked(paths []string, target func(string) string) {
	for _, path := range paths {
		dst := target(path)
		if entry, exists := t.entries[path]; exists {
			delete(t.entries, path)
			// An entry's path is its identity, so the stored declaration carries
			// the new path rather than only the map key doing so.
			entry.setPath(dst)
			t.entries[dst] = entry
		}
		if group, exists := t.groups[path]; exists {
			delete(t.groups, path)
			group.id = dst
			t.groups[dst] = group
		}
		if fiber, exists := t.fibers[path]; exists {
			delete(t.fibers, path)
			t.fibers[dst] = fiber
			t.owners[fiber.ID()] = dst
		}
		for index, ordered := range t.order {
			if ordered == path {
				t.order[index] = dst
				break
			}
		}
	}
}

// viewOrErr reports one path's view, or an error when it is gone.
func (t *Tree) viewOrErr(id string) (EntryView, error) {
	view, ok := t.View(id)
	if !ok {
		return EntryView{}, fmt.Errorf("%w: %q", ErrEntryNotFound, id)
	}
	return view, nil
}

// removePath stops and removes one path together with its descendants.
func (t *Tree) removePath(id string) error {
	t.mu.Lock()
	entry, exists := t.entries[id]
	if !exists {
		t.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrEntryNotFound, id)
	}
	if t.closed {
		t.mu.Unlock()
		return ErrTreeClosed
	}
	// Descendants are collected before their ancestor, so removing a group never
	// leaves a child whose owning group is gone.
	paths := []string{id}
	if entry.IsGroup() {
		prefix := id + EntrySeparator
		for _, path := range t.order {
			if strings.HasPrefix(path, prefix) {
				paths = append(paths, path)
			}
		}
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i] < paths[j] })
	fibers := make([]*cordis.Fiber, 0, len(paths))
	for _, path := range paths {
		if fiber, mounted := t.fibers[path]; mounted {
			fibers = append(fibers, fiber)
			delete(t.fibers, path)
			delete(t.owners, fiber.ID())
		}
		delete(t.entries, path)
		delete(t.groups, path)
		t.order = removeString(t.order, path)
	}
	if parent, ok := t.groups[groupOf(id)]; ok {
		parent.children = removeEntry(parent.children, entry)
	}
	t.mu.Unlock()

	var errs []error
	for _, fiber := range fibers {
		errs = append(errs, fiber.Dispose())
	}
	return errors.Join(errs...)
}

// sameDeclaration reports whether two declarations describe the same entry. A
// nil and an explicitly false flag are the same declaration, because the document
// treats an absent flag as false.
func sameDeclaration(a, b Options) bool {
	return a.Name == b.Name &&
		equalDisabled(a.Disabled, b.Disabled) &&
		equalInject(a.Inject, b.Inject) &&
		bytes.Equal(normalizedConfig(a.Config), normalizedConfig(b.Config))
}

// equalDisabled compares the tri-state disabled flag by effective value.
func equalDisabled(a, b *bool) bool {
	return (a != nil && *a) == (b != nil && *b)
}

// equalInject compares dependency lists by content.
func equalInject(a, b Inject) bool {
	return reflect.DeepEqual([]string(a), []string(b))
}

// normalizedConfig lets an absent payload and an explicit null compare equal,
// because both reach a plugin as nil.
func normalizedConfig(raw json.RawMessage) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	return trimmed
}

// localID returns the last path segment of an entry id.
func localID(id string) string {
	if index := strings.LastIndex(id, EntrySeparator); index >= 0 {
		return id[index+len(EntrySeparator):]
	}
	return id
}

// groupOf returns the owning group path of an entry id.
func groupOf(id string) string {
	if index := strings.LastIndex(id, EntrySeparator); index >= 0 {
		return id[:index]
	}
	return ""
}

// removeString returns the slice without the first occurrence of value.
func removeString(values []string, value string) []string {
	for index, item := range values {
		if item == value {
			return append(values[:index:index], values[index+1:]...)
		}
	}
	return values
}

// removeEntry returns the slice without the given entry.
func removeEntry(entries []*Entry, entry *Entry) []*Entry {
	for index, item := range entries {
		if item == entry {
			return append(entries[:index:index], entries[index+1:]...)
		}
	}
	return entries
}
