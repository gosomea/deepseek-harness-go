package loader

// Group is the runtime owner of a list of child entries. The root group belongs
// to a Tree and has no owning entry; a nested group is carried by the entry that
// declared `group: true`.
//
// A group holds no resources of its own. Entries reach their plugins through the
// tree, and disposing the tree releases whatever those plugins acquired.
type Group struct {
	tree     *Tree
	owner    *Entry
	id       string
	parent   *Group
	children []*Entry
}

// ID is the group's full path inside the tree. The root group's ID is empty.
func (g *Group) ID() string { return g.id }

// Owner returns the entry that declared this group, or nil for the root group.
func (g *Group) Owner() *Entry { return g.owner }

// Parent returns the enclosing group, or nil for the root group.
func (g *Group) Parent() *Group { return g.parent }

// Children returns the group's direct children in declaration order. The
// returned slice is a copy, so a caller cannot reorder the tree.
func (g *Group) Children() []*Entry { return append([]*Entry(nil), g.children...) }

// Len reports the number of direct children.
func (g *Group) Len() int { return len(g.children) }

// setGroup records the group an entry belongs to. A moved entry keeps its own
// identity while its owning group changes.
func (e *Entry) setGroup(group *Group) { e.group = group }

// Entries returns this group's descendants depth-first in declaration order,
// including entries of nested groups.
func (g *Group) Entries() []*Entry {
	var result []*Entry
	for _, entry := range g.children {
		result = append(result, entry)
		if nested, ok := g.tree.Group(entry.ID()); ok {
			result = append(result, nested.Entries()...)
		}
	}
	return result
}

// Group returns a group by path. The root group is addressed by the empty id;
// nested groups use their full path, such as "outer:mid".
func (g *Group) Group(id string) (*Group, bool) {
	if g.tree == nil {
		return nil, false
	}
	return g.tree.Group(id)
}
