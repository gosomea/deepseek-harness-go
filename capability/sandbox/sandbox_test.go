package sandbox_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/sandbox"
)

// The ladder is strictly one-directional: only wider moves are allowed, and a
// request for the same or a narrower mode is refused rather than ignored.
func TestEscalationIsStrictlyWider(t *testing.T) {
	cases := []struct {
		name      string
		effective sandbox.Mode
		target    sandbox.Mode
		allowed   bool
	}{
		{"read-only widens to workspace-write", sandbox.ModeReadOnly, sandbox.ModeWorkspaceWrite, true},
		{"read-only widens to full", sandbox.ModeReadOnly, sandbox.ModeDangerFullAccess, true},
		{"workspace-write widens to full", sandbox.ModeWorkspaceWrite, sandbox.ModeDangerFullAccess, true},
		{"same mode is not an escalation", sandbox.ModeWorkspaceWrite, sandbox.ModeWorkspaceWrite, false},
		{"narrowing is refused", sandbox.ModeDangerFullAccess, sandbox.ModeWorkspaceWrite, false},
		{"nothing escalates to read-only", sandbox.ModeWorkspaceWrite, sandbox.ModeReadOnly, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := sandbox.ValidateEscalation(testCase.effective, testCase.target)
			if testCase.allowed && err != nil {
				t.Fatalf("expected the escalation to be allowed: %v", err)
			}
			if !testCase.allowed && !errors.Is(err, sandbox.ErrNotWider) {
				t.Fatalf("want ErrNotWider, got %v", err)
			}
		})
	}
	// An unknown mode on either side is a value error, not a widening decision.
	if err := sandbox.ValidateEscalation("invented", sandbox.ModeWorkspaceWrite); !errors.Is(err, sandbox.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unknown effective mode, got %v", err)
	}
	if err := sandbox.ValidateEscalation(sandbox.ModeReadOnly, "invented"); !errors.Is(err, sandbox.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unknown target, got %v", err)
	}
}

// Only the two wider modes are offered, and the list is complete rather than cut
// down to one composition's default.
func TestEscalationTargets(t *testing.T) {
	targets := sandbox.EscalationTargets()
	if len(targets) != 2 {
		t.Fatalf("want two targets, got %v", targets)
	}
	for _, target := range targets {
		if target == sandbox.ModeReadOnly {
			t.Fatal("read-only is the floor and must not be an escalation target")
		}
		if !target.Valid() {
			t.Fatalf("target %q must be a defined mode", target)
		}
	}
}

// A policy whose effective mode is wider than the platform can enforce is
// refused: running it would mean the requested confinement is not what applies.
func TestPolicyRefusesUnenforceableConfinement(t *testing.T) {
	ok := sandbox.Policy{Effective: sandbox.ModeReadOnly, Available: sandbox.ModeWorkspaceWrite}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a narrower effective mode must be enforceable: %v", err)
	}
	equal := sandbox.Policy{Effective: sandbox.ModeWorkspaceWrite, Available: sandbox.ModeWorkspaceWrite}
	if err := equal.Validate(); err != nil {
		t.Fatalf("an equal mode must be enforceable: %v", err)
	}
	tooWide := sandbox.Policy{Effective: sandbox.ModeDangerFullAccess, Available: sandbox.ModeWorkspaceWrite}
	if err := tooWide.Validate(); !errors.Is(err, sandbox.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	for _, bad := range []sandbox.Policy{
		{Effective: "invented", Available: sandbox.ModeWorkspaceWrite},
		{Effective: sandbox.ModeReadOnly, Available: "invented"},
	} {
		if err := bad.Validate(); !errors.Is(err, sandbox.ErrInvalidValue) {
			t.Fatalf("want ErrInvalidValue, got %v", err)
		}
	}
	if err := sandbox.Fits(sandbox.ModeReadOnly, sandbox.ModeWorkspaceWrite); !errors.Is(err, sandbox.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable from Fits, got %v", err)
	}
	if err := sandbox.Fits("x", sandbox.ModeReadOnly); !errors.Is(err, sandbox.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue from Fits, got %v", err)
	}
}

// Ranking is what widening is computed from, so it must be monotone.
func TestModeRanking(t *testing.T) {
	if !sandbox.ModeDangerFullAccess.Wider(sandbox.ModeWorkspaceWrite) {
		t.Fatal("full access must be wider than workspace-write")
	}
	if !sandbox.ModeWorkspaceWrite.Wider(sandbox.ModeReadOnly) {
		t.Fatal("workspace-write must be wider than read-only")
	}
	if sandbox.ModeReadOnly.Wider(sandbox.ModeReadOnly) {
		t.Fatal("a mode is not wider than itself")
	}
	if sandbox.Mode("invented").Valid() {
		t.Fatal("an unknown mode must not be valid")
	}
}
