package loader

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// Shape distinguishes what an entry declares from what the tree actually runs.
type Shape string

const (
	// Declared is the configuration document's entry tree: what the file asks for.
	Declared Shape = "declared"
	// Running is the tree's live state: which entries have a plugin instance and
	// what lifecycle state that instance reached.
	Running Shape = "running"
)

// Row is one line of a tree dump. It reports the same path in both shapes, so a
// reader can compare the document against the running tree line by line.
type Row struct {
	// Path is the entry's full id inside the tree.
	Path string
	// Name is the catalog name of the entry's plugin; empty for a group entry.
	Name string
	// Group marks an entry that groups other entries instead of running a plugin.
	Group bool
	// Disabled reports whether the document disabled this entry.
	Disabled bool
	// Depth is the nesting level, zero for a direct child of the root group.
	Depth int
	// State is the plugin instance's lifecycle state; empty when the entry has no
	// instance in the running shape.
	State cordis.State
	// Err is the instance's startup or teardown failure, if any.
	Err error
}

// Dump renders the requested shape as stable text. The declared shape reads the
// configuration entries; the running shape adds each entry's live instance state.
// Output order is declaration order, so two dumps of an unchanged tree match.
func (t *Tree) Dump(shape Shape) (string, error) {
	switch shape {
	case Declared, Running:
	default:
		return "", fmt.Errorf("%w: unknown dump shape %q", ErrInvalidConfig, shape)
	}
	paths := t.Paths()
	var out strings.Builder
	for _, path := range paths {
		entry, ok := t.Entry(path)
		if !ok {
			return "", fmt.Errorf("%w: %q", ErrEntryNotFound, path)
		}
		row := Row{
			Path:     path,
			Name:     entry.Name(),
			Group:    entry.IsGroup(),
			Disabled: entry.Disabled(),
			Depth:    strings.Count(path, EntrySeparator),
		}
		if shape == Running {
			if fiber, mounted := t.Fiber(path); mounted {
				row.State = fiber.State()
				row.Err = fiber.Err()
			}
		}
		fmt.Fprintln(&out, formatRow(shape, row))
	}
	return out.String(), nil
}

// WriteDump writes one shape to a writer, for callers that stream diagnostics.
func (t *Tree) WriteDump(w io.Writer, shape Shape) error {
	text, err := t.Dump(shape)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, text)
	return err
}

// formatRow renders one dump line. Both shapes share the indentation and the
// path, so a diff between them shows only what changed between declaring and
// running an entry.
func formatRow(shape Shape, row Row) string {
	indent := strings.Repeat("  ", row.Depth)
	kind := "plugin"
	if row.Group {
		kind = "group"
	}
	flags := ""
	if row.Disabled {
		flags = " disabled"
	}
	if shape == Declared {
		return fmt.Sprintf("%s%s [%s]%s", indent, row.Path, kind, flags)
	}
	state := "not-mounted"
	if row.State != "" {
		state = string(row.State)
	}
	line := fmt.Sprintf("%s%s [%s]%s state=%s", indent, row.Path, kind, flags, state)
	if row.Err != nil {
		line += " error=" + row.Err.Error()
	}
	return line
}

// Diagnose returns one line per mounted entry whose state needs attention: a
// startup failure, a pending dependency, or an entry that declares a plugin but
// has no instance. An empty result means nothing is wrong.
//
// A closed tree diagnoses nothing: every instance is disposed by design, so
// reporting them would make a healthy shutdown look like a fault.
func (t *Tree) Diagnose() []string {
	if t.Closed() {
		return nil
	}
	paths := t.Paths()
	sort.SliceStable(paths, func(i, j int) bool { return paths[i] < paths[j] })
	var lines []string
	for _, path := range paths {
		entry, ok := t.Entry(path)
		if !ok {
			continue
		}
		if entry.Disabled() || entry.IsGroup() {
			continue
		}
		fiber, mounted := t.Fiber(path)
		if !mounted {
			lines = append(lines, fmt.Sprintf("entry %q declares plugin %q but has no instance", path, entry.Name()))
			continue
		}
		if err := fiber.Err(); err != nil {
			lines = append(lines, fmt.Sprintf("entry %q failed: %v", path, err))
			continue
		}
		if state := fiber.State(); state != cordis.Active {
			lines = append(lines, fmt.Sprintf("entry %q is %s", path, state))
		}
	}
	return lines
}
