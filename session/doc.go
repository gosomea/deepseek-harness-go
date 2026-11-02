// Package session stores an agent's authoritative history as an append-only event
// log, which is the source every derived view is rebuilt from.
//
// The log, not a message slice, is the source of truth. Every fact is one
// immutable event with a dense sequence number, so the same log always
// reconstructs the same history and nothing a caller holds afterwards can change
// what was recorded. This package owns the log and the rules that decide what may
// enter it; deriving model history from it is done by a component that attaches
// through Log.OnValidate and Log.OnEvent.
//
// Two rules shape the design. First, sequence numbers are positions, not
// counters: a log's next sequence is its length, so a gap or a duplicate is
// always a defect and is refused rather than repaired. Second, only events that
// produce model messages enter the ordered surface; boundary markers, attempts
// and audit records lengthen the log without changing what the model sees.
//
// This package is in-memory. Durable files, handles, single-writer rules and
// format interoperability belong to a later stage.
package session
