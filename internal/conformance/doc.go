// Package conformance records the disposition of every source package group.
//
// The package exists because "we covered everything" is a claim that must be
// machine-checkable: the fixed baseline has 54 package groups and 9 vendor
// packages, and each must have a disposition (verified, adapted, or a recorded
// gap). A group with no disposition is not silently assumed done — the validator
// refuses it, so the coverage claim cannot drift from the ledger.
package conformance
