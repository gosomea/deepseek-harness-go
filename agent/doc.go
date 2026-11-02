// Package agent defines the public contract every consumer of an Agent uses:
// a handle, a state observation, and the factory seam that creates one.
//
// The package is deliberately free of any driving logic. It knows what an Agent
// *is* — its identity, its scope, the inbox it accepts input through, and how a
// caller waits for or cancels its work — but not how a turn is executed. The
// default driver lives in package agentloop and depends on this package; this
// package must never depend on it, so an extension plugin can offer its own
// driver without importing the default one.
//
// That direction is enforced by a check, not by convention: nothing under agent/
// may import agentloop/.
package agent
