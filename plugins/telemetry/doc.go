// Package telemetry records a privacy-bounded view of product events.
//
// The package exists because telemetry sits on a boundary that must be explicit:
// what leaves the host is a fixed set of numeric/status fields, never message
// content or credentials. This package keeps that boundary checkable — a field
// not on the allow-list is refused — so "what we send" is a rule, not a hope.
//
// It is keyless and in-process: a sink is an interface a test supplies, so the
// allow-list and redaction rules are proven without a network.
package telemetry
