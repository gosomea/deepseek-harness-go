// Package testkit holds deterministic test infrastructure shared across
// packages.
//
// It is deliberately not a general-purpose "utils" package: every type here
// exists to make one class of test deterministic. The parity runner replays a
// shared scenario file against an implementation and emits a canonical trace,
// so the Go runtime and the fixed TypeScript reference can be compared on the
// same input instead of on hand-written expectations.
package testkit
