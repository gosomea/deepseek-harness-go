// Package fs defines the filesystem seam: reading, writing and listing files
// inside a workspace world.
//
// The seam exists so a tool never holds a path it can act on by itself. Every
// method takes a path that the Provider resolves through the workspace rules
// first, which is what makes "the tool cannot reach outside the world" a
// property of the interface rather than of every call site.
package fs

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/workspace"
)

// ErrInvalidValue reports an unusable request.
var ErrInvalidValue = errors.New("fs: invalid value")

// ErrNotFound reports a path that does not exist.
var ErrNotFound = errors.New("fs: not found")

// ErrConflict reports a write whose expectation no longer holds.
//
// It is distinct from a generic I/O failure: the file may be perfectly healthy
// and the caller's assumption about it is what is out of date.
var ErrConflict = errors.New("fs: file changed since it was read")

// ErrTooLarge reports a read whose content exceeds the configured limit.
var ErrTooLarge = errors.New("fs: content exceeds the limit")

// Mode is the access a Service grants.
type Mode string

// The access modes this package defines, ordered from narrowest to widest.
const (
	// ModeReadOnly permits reading only.
	ModeReadOnly Mode = "read-only"
	// ModeWorkspaceWrite permits writing inside the workspace.
	ModeWorkspaceWrite Mode = "workspace-write"
)

// Valid reports whether the mode is one this package defines.
func (m Mode) Valid() bool { return m == ModeReadOnly || m == ModeWorkspaceWrite }

// CanWrite reports whether the mode permits a write.
func (m Mode) CanWrite() bool { return m == ModeWorkspaceWrite }

// Stat describes one file without reading it.
type Stat struct {
	// Path is the canonical path inside the workspace.
	Path string
	// Size is the file size in bytes.
	Size int64
	// Modified is the recorded modification time.
	Modified time.Time
	// IsDir reports whether the path is a directory.
	IsDir bool
	// Mode is the permission bits.
	Mode os.FileMode
}

// Entry is one directory entry.
type Entry struct {
	// Name is the entry's base name.
	Name string
	// IsDir reports whether the entry is a directory.
	IsDir bool
	// Size is the entry size in bytes.
	Size int64
}

// ReadResult is the outcome of a read.
type ReadResult struct {
	// Path is the canonical path that was read.
	Path string
	// Content is the file's bytes.
	Content []byte
	// Modified is the modification time the content was read at.
	//
	// A write may cite it back to detect a change between the read and the
	// write, which is what turns "silently overwrote someone's edit" into a
	// reported conflict.
	Modified time.Time
}

// WriteRequest describes a write.
type WriteRequest struct {
	// Path is the target path, resolved through the workspace.
	Path string
	// Content is the bytes to write.
	Content []byte
	// ExpectedModified, when set, must match the file's current modification
	// time; a mismatch fails with ErrConflict instead of overwriting.
	ExpectedModified *time.Time
	// Create makes the write a creation, failing if the path already exists.
	Create bool
}

// Service is the filesystem seam.
//
// Every method takes a context so long reads can be cancelled, and every path is
// resolved through the workspace before use.
type Service interface {
	// Workspace reports the world this service is confined to.
	Workspace() workspace.Workspace
	// Read returns a file's content.
	Read(ctx Ctx, path string) (ReadResult, error)
	// Write stores content under the policy's rules.
	Write(ctx Ctx, request WriteRequest) (Stat, error)
	// Stat describes a path.
	Stat(ctx Ctx, path string) (Stat, error)
	// List returns one directory's entries sorted by name.
	List(ctx Ctx, path string) ([]Entry, error)
	// Remove deletes one file.
	Remove(ctx Ctx, path string) error
}

// Ctx is the cancellation contract this package needs.
//
// It is declared here rather than importing context so the seam states its own
// minimum requirement; the concrete service accepts a context.Context, which
// satisfies it.
type Ctx interface {
	// Err reports whether the work was cancelled.
	Err() error
	// Done reports a channel closed when the work should stop.
	Done() <-chan struct{}
}

// Validate reports whether a write request is usable.
func (r WriteRequest) Validate() error {
	if strings.TrimSpace(r.Path) == "" {
		return fmt.Errorf("%w: a path is required", ErrInvalidValue)
	}
	if r.Create && r.ExpectedModified != nil {
		return fmt.Errorf("%w: a create cannot also expect a modification time", ErrInvalidValue)
	}
	return nil
}

// MaxReadBytes bounds a single read so one large file cannot exhaust memory.
const MaxReadBytes = 8 << 20

// CheckReadSize reports whether a read of size bytes is acceptable.
func CheckReadSize(size int64) error {
	if size > MaxReadBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrTooLarge, size, MaxReadBytes)
	}
	return nil
}
