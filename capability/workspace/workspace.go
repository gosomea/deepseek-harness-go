// Package workspace defines the execution world: one stable identity over one
// existing directory, plus the path rules that make "inside this workspace" a
// decidable question.
//
// Identity is a generated value, never the path. Paths are normalized, can be
// reached through symlinks, and differ in case between platforms; anchoring a
// session to a path string would mean the anchor moves whenever the path is
// spelled differently. The id stays put and the canonical root is derived from
// it.
package workspace

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidValue reports a workspace value that cannot be used.
var ErrInvalidValue = errors.New("workspace: invalid value")

// ErrOutsideWorkspace reports a path that resolves outside the workspace root.
//
// It is a distinct condition because it is not a caller mistake to be corrected
// by retrying: the path is either inside the world or it is not, and a tool that
// treated it as a transient error would keep asking.
var ErrOutsideWorkspace = errors.New("workspace: path is outside the workspace")

// ID identifies one workspace record.
//
// It is an opaque string rather than a path, so a reference anchor survives
// re-spelling its root.
type ID string

// String returns the raw identifier.
func (id ID) String() string { return string(id) }

// IsZero reports whether the identifier is unset.
func (id ID) IsZero() bool { return id == "" }

// Workspace is one execution world: a stable identity over a directory, and the
// normalization that decides what belongs to it.
//
// The two methods are deliberately paired. A caller that can ask "what is the
// real path of this candidate" can also ask "is this other path inside", and the
// two answers cannot disagree because one is defined in terms of the other.
type Workspace interface {
	// ID reports the stable identity.
	ID() ID
	// Root reports the canonical absolute root of the world.
	Root() string
	// Resolve normalizes a candidate path against the root.
	//
	// It returns the canonical path when the candidate is inside the world, and
	// ErrOutsideWorkspace when it is not. It never returns a "best effort" path:
	// a tool that received one could write outside the world without knowing.
	Resolve(candidate string) (string, error)
}

// World is an immutable Workspace over one directory.
type World struct {
	// id is the stable identity.
	id ID
	// root is the canonical absolute root.
	root string
	// symlinks says whether symlink escapes are resolved before the containment
	// check. It exists because resolving them has a cost, and a caller that
	// already knows the tree has no symlinks should not pay it.
	symlinks bool
}

// Options configures a World.
type Options struct {
	// ID is the stable identity. Required.
	ID ID
	// Root is the directory the world is anchored to. Required.
	Root string
	// ResolveSymlinks makes containment resolution follow links. It defaults to
	// false, and New documents what that means.
	ResolveSymlinks bool
}

// New builds a World anchored to an existing directory.
//
// The root is made absolute and cleaned, so two spellings of the same directory
// produce the same root. Symlink resolution is off by default: a workspace whose
// root is itself reached through a symlink must still contain the files under
// it, and resolving the root would compare against a different spelling than the
// one the caller supplied. Callers that need link-escape detection turn it on.
func New(options Options) (*World, error) {
	if options.ID.IsZero() {
		return nil, fmt.Errorf("%w: a workspace id is required", ErrInvalidValue)
	}
	if strings.TrimSpace(options.Root) == "" {
		return nil, fmt.Errorf("%w: a workspace root is required", ErrInvalidValue)
	}
	absolute, err := filepath.Abs(options.Root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	root := filepath.Clean(absolute)
	if options.ResolveSymlinks {
		// The root has to be canonicalized too, not only the candidates. On
		// platforms where the temporary directory is itself reached through a
		// link (macOS /var -> /private/var is the common case), resolving a
		// candidate would otherwise compare a canonical path against an
		// uncanonical root and report every path as outside the world.
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
	}
	return &World{id: options.ID, root: root, symlinks: options.ResolveSymlinks}, nil
}

// ID reports the stable identity.
func (w *World) ID() ID { return w.id }

// Root reports the canonical absolute root.
func (w *World) Root() string { return w.root }

// Resolve normalizes a candidate path against the root.
//
// A relative candidate is joined to the root; an absolute one is taken as
// given. The result is cleaned and then checked for containment by comparing
// path elements, not string prefixes — otherwise a sibling directory named like
// a prefix of the root ("/work" and "/workshop") would pass.
func (w *World) Resolve(candidate string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("%w: a path is required", ErrInvalidValue)
	}
	target := candidate
	if !filepath.IsAbs(target) {
		target = filepath.Join(w.root, target)
	}
	target = filepath.Clean(target)
	if w.symlinks {
		resolved, err := filepath.EvalSymlinks(target)
		if err == nil {
			target = resolved
		}
		// A path that does not exist yet cannot be evaluated; it is still checked
		// for containment so a create cannot escape through a missing tail.
	}
	if !w.contains(target) {
		return "", fmt.Errorf("%w: %s", ErrOutsideWorkspace, candidate)
	}
	return target, nil
}

// ResolveForCreate normalizes a path that does not exist yet.
//
// It resolves the deepest existing ancestor through symlinks and re-appends the
// missing tail, so a create cannot escape the world through a symlinked parent
// that does not exist yet in full. This is the check a write needs; Resolve is
// the one a read needs.
func (w *World) ResolveForCreate(candidate string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("%w: a path is required", ErrInvalidValue)
	}
	target := candidate
	if !filepath.IsAbs(target) {
		target = filepath.Join(w.root, target)
	}
	target = filepath.Clean(target)
	if !w.contains(target) {
		return "", fmt.Errorf("%w: %s", ErrOutsideWorkspace, candidate)
	}
	if !w.symlinks {
		return target, nil
	}
	// Walk up to the deepest existing ancestor and resolve that, then re-append
	// the tail. Resolving the full path would fail on the part being created.
	tail := ""
	probe := target
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			joined := resolved
			if tail != "" {
				joined = filepath.Join(resolved, tail)
			}
			if !w.contains(joined) {
				return "", fmt.Errorf("%w: %s", ErrOutsideWorkspace, candidate)
			}
			return joined, nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return target, nil
		}
		tail = filepath.Join(filepath.Base(probe), tail)
		probe = parent
	}
}

// Contains reports whether a path already inside the world stays inside it.
//
// It is exported so a caller that has canonicalized a path elsewhere can ask the
// same containment question without round-tripping through Resolve.
func (w *World) Contains(path string) bool { return w.contains(filepath.Clean(path)) }

// contains compares path elements so a prefix that is not a parent does not
// count as containment.
func (w *World) contains(path string) bool {
	if path == w.root {
		return true
	}
	relative, err := filepath.Rel(w.root, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
