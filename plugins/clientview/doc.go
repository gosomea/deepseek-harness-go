// Package clientview builds the client-facing projection of a session's
// authoritative log.
//
// The package exists because a UI must never become a second source of truth:
// what a client shows is a projection of the host's log, delivered either as a
// snapshot (everything up to a sequence) or as incremental deltas (everything
// after a sequence). Reconnecting at a sequence is what makes "no missing and no
// duplicated state" checkable rather than hoped for.
//
// The projection is pure and keyless: it takes an ordered slice of entries and a
// last-seen sequence, and returns what to show. A snapshot and the deltas after
// it compose to the same view as a single snapshot, and a test asserts exactly
// that.
package clientview
