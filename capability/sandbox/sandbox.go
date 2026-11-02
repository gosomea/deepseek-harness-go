// Package sandbox defines the confinement seam: which mode a call runs under,
// which wider modes it may escalate to, and when a missing capability must stop
// a program instead of silently running unconfined.
//
// The escalation ladder is deliberately one-directional. "More confined than
// required" is allowed; "less confined than required" is not, and nothing
// escalates toward the narrower end. Validating that at execution time — rather
// than in a tool's schema — matters because a schema is registry-global while
// the effective mode belongs to one call.
package sandbox

import (
	"errors"
	"fmt"
)

// ErrInvalidValue reports an unusable mode or request.
var ErrInvalidValue = errors.New("sandbox: invalid value")

// ErrNotWider reports an escalation that does not widen confinement.
var ErrNotWider = errors.New("sandbox: target mode is not wider than the effective mode")

// ErrUnavailable reports a required mode this build cannot enforce.
var ErrUnavailable = errors.New("sandbox: required confinement is unavailable")

// Mode is a confinement level, ordered from narrowest to widest.
type Mode string

// The modes this package defines.
const (
	// ModeReadOnly permits no mutation at all.
	ModeReadOnly Mode = "read-only"
	// ModeWorkspaceWrite permits writes inside the workspace.
	ModeWorkspaceWrite Mode = "workspace-write"
	// ModeDangerFullAccess permits everything the host allows.
	//
	// Its name says what it is: not "full access" as a convenience, but the mode
	// that removes confinement, and the only one that should ever be reached
	// through an explicit grant.
	ModeDangerFullAccess Mode = "danger-full-access"
)

// rank orders the modes so widening is a comparison rather than a table lookup.
func (m Mode) rank() int {
	switch m {
	case ModeReadOnly:
		return 0
	case ModeWorkspaceWrite:
		return 1
	case ModeDangerFullAccess:
		return 2
	default:
		return -1
	}
}

// Valid reports whether the mode is one this package defines.
func (m Mode) Valid() bool { return m.rank() >= 0 }

// Wider reports whether m is strictly wider than other.
func (m Mode) Wider(other Mode) bool { return m.rank() > other.rank() }

// EscalationTargets lists every mode a call may escalate to.
//
// read-only is the floor, so nothing escalates to it. The list is closed and
// advertised in full rather than cut down to the modes wider than one
// composition's default: a session whose effective mode sits below the default
// would otherwise be offered nothing while still being confined.
func EscalationTargets() []Mode {
	return []Mode{ModeWorkspaceWrite, ModeDangerFullAccess}
}

// ValidateEscalation reports whether a call may move from effective to target.
//
// A request for the same mode is refused rather than ignored: the caller asked
// for an escalation it does not need, which usually means it misread the
// effective mode, and silently accepting it would hide that.
func ValidateEscalation(effective, target Mode) error {
	if !effective.Valid() {
		return fmt.Errorf("%w: unknown effective mode %q", ErrInvalidValue, effective)
	}
	if !target.Valid() {
		return fmt.Errorf("%w: unknown target mode %q", ErrInvalidValue, target)
	}
	if !target.Wider(effective) {
		return fmt.Errorf("%w: %s -> %s", ErrNotWider, effective, target)
	}
	return nil
}

// Fits reports whether an available mode satisfies a required one.
//
// It refuses a required mode wider than what is available, which is the check
// that turns "this platform cannot sandbox" into a refusal at startup instead of
// an unconfined run.
func Fits(available, required Mode) error {
	if !available.Valid() || !required.Valid() {
		return fmt.Errorf("%w: unknown mode", ErrInvalidValue)
	}
	if required.Wider(available) {
		return fmt.Errorf("%w: %s needed, %s available", ErrUnavailable, required, available)
	}
	return nil
}

// Policy is the confinement a run is configured with.
type Policy struct {
	// Effective is the mode calls run under.
	Effective Mode
	// Available is the strongest mode this build can enforce.
	//
	// It exists separately from Effective because a program may be configured
	// more narrowly than the platform allows, and the two answer different
	// questions: what applies now, and what could apply.
	Available Mode
}

// Validate reports whether the policy can be enforced.
//
// A policy whose effective mode is wider than what is available is refused
// outright: running it anyway would mean the confinement the caller asked for is
// not the confinement it gets.
func (p Policy) Validate() error {
	if !p.Effective.Valid() {
		return fmt.Errorf("%w: unknown effective mode %q", ErrInvalidValue, p.Effective)
	}
	if !p.Available.Valid() {
		return fmt.Errorf("%w: unknown available mode %q", ErrInvalidValue, p.Available)
	}
	return Fits(p.Available, p.Effective)
}
