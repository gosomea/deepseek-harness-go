// Package persistence defines the storage contract a Session log is opened
// through: a service that creates, opens, stats and lists sessions, and a
// per-session handle that appends and reads.
//
// The package deliberately knows nothing about files, JSON lines or locking
// mechanisms. Those are Provider concerns; this package owns the seam and the
// rules a handle must keep, so a second backend can be added without touching
// the Agent or the Session log.
//
// Ownership is the central idea here. A handle is single-owner state, not a
// shared service: a read handle never takes ownership, a write handle is the
// session's only mutator, and every operation on a closed handle fails.
package persistence
