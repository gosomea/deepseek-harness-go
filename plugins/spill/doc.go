// Package spill keeps long content addressable after shortening it.
//
// The package exists because two different numbers are easy to confuse: what a
// provider reported as usage, and what this build estimates before sending. An
// estimate presented as a measurement is a number nobody can defend, so the two
// are kept apart — every budget decision names which one it used.
//
// Shortening always leaves a citation. A request that carries a shortened output
// also carries the reference that finds the original, so a reader can tell
// "shortened but retrievable" from "discarded".
package spill
