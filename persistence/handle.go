package persistence

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/gosomea/deepseek-harness-go/session"
)

// Access is the log access an open grants.
//
// `write` is read-write: the session's single mutator, which also reads its own
// log. `read` only observes — it never takes ownership and works while another
// handle or process holds `write`.
type Access string

// The access modes this package defines.
const (
	// AccessRead observes the log without taking ownership.
	AccessRead Access = "read"
	// AccessWrite appends to the log and owns the session while open.
	AccessWrite Access = "write"
)

// Valid reports whether the access mode is one this package defines.
func (a Access) Valid() bool { return a == AccessRead || a == AccessWrite }

// CanWrite reports whether the mode may append.
func (a Access) CanWrite() bool { return a == AccessWrite }

// SessionID identifies one stored session.
//
// It is an opaque string. The reference brands identity at compile time and does
// no format validation, so a provider that turns an id into a path must encode
// it rather than trust it; this package passes it through unchanged.
type SessionID string

// String returns the raw identifier.
func (id SessionID) String() string { return string(id) }

// IsZero reports whether the identifier is unset.
func (id SessionID) IsZero() bool { return id == "" }

// Stat describes one stored session without opening it.
type Stat struct {
	// ID is the session identity.
	ID SessionID
	// FormatVersion is the version the artifact declares.
	FormatVersion int
	// Events counts the complete records the artifact holds.
	Events int
	// Bytes is the artifact size on disk.
	Bytes int64
	// RecoverableTail reports that the artifact ends with an incomplete record
	// which a repair can drop without touching the committed prefix.
	RecoverableTail bool
	// WriterHeld reports that a writer currently holds the session.
	WriterHeld bool
}

// Handle is one open channel onto a stored session.
//
// It is single-owner state rather than a shared service. A write handle reads
// its own successful appends; `close` is the one teardown and is idempotent;
// every operation after Close returns ErrHandleClosed.
type Handle interface {
	// ID reports the session this handle addresses.
	ID() SessionID
	// Access reports the mode this handle was opened with.
	Access() Access
	// Read returns the committed events from the given offset.
	//
	// It never backtracks below what this handle already observed, so a caller
	// that has seen offset N cannot be handed a shorter prefix later.
	Read(ctx context.Context, from session.Seq) ([]session.Event, error)
	// Append commits one event and reports the offset it was written at.
	//
	// The returned event is the record as stored. A failed append writes
	// nothing: the artifact is either extended by one complete record or left
	// untouched.
	Append(ctx context.Context, input session.AppendInput) (session.Event, error)
	// Flush makes every completed append durable.
	//
	// Only Flush promises persistence. Append returning means the record is
	// visible to this handle's own reads, which is a weaker promise, and the
	// difference is observable in the tests.
	Flush(ctx context.Context) error
	// Close releases the handle. It is idempotent and uncancellable.
	Close() error
}

// HandleBase carries the state every backend handle shares.
//
// It exists so the closed-handle rule and the read-only rule are implemented
// once, in one place, instead of being repeated — and possibly forgotten — by
// every backend.
type HandleBase struct {
	// id is the session identity.
	id SessionID
	// access is the mode the handle was opened with.
	access Access
	// closed marks the handle released.
	closed atomic.Bool
}

// ID reports the session this handle addresses.
func (h *HandleBase) ID() SessionID { return h.id }

// Access reports the mode this handle was opened with.
func (h *HandleBase) Access() Access { return h.access }

// Init sets the identity and access mode. A backend calls it once, before the
// handle is returned, so the shared rules have what they need.
func (h *HandleBase) Init(id SessionID, access Access) {
	h.id = id
	h.access = access
}

// Close marks the handle closed. It is idempotent.
func (h *HandleBase) Close() error {
	h.closed.Store(true)
	return nil
}

// CheckOpen reports whether an operation may proceed.
//
// The read-only rule is checked before the closed rule for a mutation, so a
// read-only handle reports the more specific reason: a caller that tried to
// write through a read handle has a different bug from one that closed too
// early.
func (h *HandleBase) CheckOpen(writing bool) error {
	if writing && !h.access.CanWrite() {
		return fmt.Errorf("%w: %s", ErrReadOnly, h.id)
	}
	if h.closed.Load() {
		return fmt.Errorf("%w: %s", ErrHandleClosed, h.id)
	}
	return nil
}

// Closed reports whether the handle was closed.
func (h *HandleBase) Closed() bool { return h.closed.Load() }
