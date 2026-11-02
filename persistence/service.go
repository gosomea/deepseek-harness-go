package persistence

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// CreateOptions describes a session to create.
type CreateOptions struct {
	// ID is the session identity. Required.
	ID SessionID
	// FormatVersion is the version to write. Zero uses the provider's default.
	FormatVersion int
}

// OpenOptions describes how to open an existing session.
type OpenOptions struct {
	// ID is the session identity. Required.
	ID SessionID
	// Access is the mode to open with. Required.
	Access Access
}

// Validate reports whether the options are complete.
func (o OpenOptions) Validate() error {
	if o.ID.IsZero() {
		return fmt.Errorf("%w: open requires a session id", ErrInvalidValue)
	}
	if !o.Access.Valid() {
		return fmt.Errorf("%w: open requires a valid access mode, got %q", ErrInvalidValue, o.Access)
	}
	return nil
}

// Service is the persistence seam: it creates, opens, stats and lists sessions.
//
// It is a service rather than a set of free functions because a running process
// has exactly one storage configuration, and because the writer bookkeeping
// that rejects a second writer has to live somewhere shared.
type Service interface {
	// Create makes a new session and returns an unopened description of it.
	Create(ctx context.Context, options CreateOptions) (Stat, error)
	// Open returns a handle with the requested access.
	Open(ctx context.Context, options OpenOptions) (Handle, error)
	// Stat describes a session without opening it.
	Stat(ctx context.Context, id SessionID) (Stat, error)
	// List returns the known sessions sorted by id, so a listing is stable.
	List(ctx context.Context) ([]Stat, error)
	// Close releases the service and refuses later use.
	Close() error
}

// BaseService implements the parts of Service that do not depend on a backend:
// writer bookkeeping, validation and the closed-service rule.
//
// A backend embeds it and supplies create/open/stat/list. Keeping the shared
// part here is what makes "a second writer is refused" a property of the seam
// rather than something each provider must remember.
type BaseService struct {
	// mu guards the writer set and the closed flag.
	mu sync.Mutex
	// writers holds the session ids currently held by a write handle.
	writers map[SessionID]bool
	// closed marks the service released.
	closed bool
}

// NewBaseService returns an initialized base.
func NewBaseService() BaseService {
	return BaseService{writers: map[SessionID]bool{}}
}

// ClaimWriter registers a writer for one session.
//
// It refuses a second writer, which is the rule that stops two processes from
// interleaving appends into one file. The claim happens before the backend
// opens anything, so a refused open leaves no partially acquired resource.
func (b *BaseService) ClaimWriter(id SessionID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fmt.Errorf("%w: service is closed", ErrHandleClosed)
	}
	if b.writers[id] {
		return fmt.Errorf("%w: %s", ErrWriterExists, id)
	}
	b.writers[id] = true
	return nil
}

// ReleaseWriter releases a writer claim. It is idempotent.
func (b *BaseService) ReleaseWriter(id SessionID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.writers, id)
}

// WriterHeld reports whether a writer currently holds a session.
func (b *BaseService) WriterHeld(id SessionID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writers[id]
}

// MarkClosed closes the service and returns the writer ids it still holds.
//
// The ids are returned rather than dropped so the caller can decide how to
// release the underlying resources; silently clearing the set would let a
// backend leak a lock file.
func (b *BaseService) MarkClosed() (bool, []SessionID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false, nil
	}
	b.closed = true
	ids := make([]SessionID, 0, len(b.writers))
	for id := range b.writers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return true, ids
}

// Closed reports whether the service is closed.
func (b *BaseService) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// ValidateCreate reports whether a create request is usable.
func ValidateCreate(options CreateOptions) error {
	if options.ID.IsZero() {
		return fmt.Errorf("%w: create requires a session id", ErrInvalidValue)
	}
	if options.FormatVersion < 0 {
		return fmt.Errorf("%w: format version must not be negative", ErrInvalidValue)
	}
	return nil
}

// SortStats sorts stats by id so every listing is stable.
func SortStats(stats []Stat) {
	sort.Slice(stats, func(i, j int) bool { return stats[i].ID < stats[j].ID })
}
