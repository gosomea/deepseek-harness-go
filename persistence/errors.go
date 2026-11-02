package persistence

import "errors"

// ErrInvalidValue reports a value that cannot be used: a missing session id, an
// unknown access mode, or an unset clock.
var ErrInvalidValue = errors.New("persistence: invalid value")

// ErrHandleClosed reports use of a handle after Close.
//
// It is a distinct error because "you closed this" is a different fact from
// "the storage is broken": a caller that closed too early has a bug, while a
// backend failure needs a different response.
var ErrHandleClosed = errors.New("persistence: handle is closed")

// ErrReadOnly reports a mutation attempted through a read-only handle.
var ErrReadOnly = errors.New("persistence: handle is read-only")

// ErrWriterExists reports a second write handle for a session that already has
// one.
//
// The rule is enforced at open time rather than at write time, so two processes
// cannot both believe they own the session and interleave their appends.
var ErrWriterExists = errors.New("persistence: a writer already holds this session")

// ErrSessionNotFound reports a session id the storage does not hold.
var ErrSessionNotFound = errors.New("persistence: session not found")

// ErrUnsupportedVersion reports a stored session whose format version this
// build does not understand.
//
// It is refused rather than migrated: a reader that guessed at a future format
// could silently reinterpret records, and the original file must stay intact.
var ErrUnsupportedVersion = errors.New("persistence: unsupported session format version")

// ErrCorruptReport reports a stored session whose middle is damaged.
//
// Damage at the tail can be repaired by dropping the incomplete record; damage
// that leaves a hole in the middle cannot, because the missing records are
// unrecoverable and the remaining ones no longer form a contiguous log.
var ErrCorruptReport = errors.New("persistence: session log is corrupt")
