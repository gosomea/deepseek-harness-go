// Package jobs records durable, restartable background work.
//
// The package exists because a goroutine is not a task: a goroutine vanishes with
// the process and leaves no record that the work was requested, half-done or
// settled. A job is a durable record with a state machine, claimed by an owner
// under a lease, retried on a bounded rule and resumable after restart. This
// package keeps the record and the transitions; a scheduler or a clock drives
// them.
//
// Exactly-once is not promised: a job that may have produced an external side
// effect but whose settlement was not recorded is left in an unknown state that a
// human resolves. Pretending such a job is safe to replay is the failure this
// package refuses.
package jobs
