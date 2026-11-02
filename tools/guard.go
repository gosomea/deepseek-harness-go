package tools

import (
	"fmt"
	"strings"
)

// GuardDecision is a policy answer about one call.
type GuardDecision string

// The guard decisions this package understands.
const (
	// GuardAllow admits the call.
	GuardAllow GuardDecision = "allow"
	// GuardDeny refuses the call before its body runs.
	GuardDeny GuardDecision = "deny"
	// GuardAbstain leaves the decision to a later guard.
	GuardAbstain GuardDecision = "abstain"
)

// Guard is one admission check.
//
// A guard sees the definition and the raw call but not decoded arguments: a
// guard that needs to inspect arguments must do so from the raw string, so a
// guard can never be bypassed by the pipeline decoding differently than the
// guard assumed. The returned reason is shown to the model when a call is
// denied.
type Guard interface {
	// Check returns the guard's decision and, for a denial, its reason.
	Check(definition Definition, call Call) (GuardDecision, string)
}

// GuardFunc adapts a function to the Guard interface.
type GuardFunc func(definition Definition, call Call) (GuardDecision, string)

// Check implements Guard.
func (f GuardFunc) Check(definition Definition, call Call) (GuardDecision, string) {
	return f(definition, call)
}

// GuardChain is an ordered, append-only set of guards.
//
// Order matters and is preserved: the reference's guard sequence is monotonic,
// meaning a later guard can add refusals but cannot undo an earlier refusal.
// That property is what stops a plugin registered after a policy from quietly
// re-authorizing a denied call, so it is enforced here rather than left to
// registration order at the call site.
type GuardChain struct {
	guards []Guard
}

// NewGuardChain returns a chain over the given guards, in order.
func NewGuardChain(guards ...Guard) *GuardChain {
	return &GuardChain{guards: append([]Guard(nil), guards...)}
}

// Append adds a guard after every existing one. It never inserts or reorders,
// so an existing denial cannot be weakened by appending.
func (c *GuardChain) Append(guard Guard) {
	if guard == nil {
		return
	}
	c.guards = append(c.guards, guard)
}

// Len returns how many guards are present.
func (c *GuardChain) Len() int { return len(c.guards) }

// Check runs every guard in order and returns the first refusal.
//
// A guard that abstains is skipped. The first denial wins and no later guard is
// consulted, which is the monotonic rule: adding guards can only make admission
// harder.
func (c *GuardChain) Check(definition Definition, call Call) (GuardDecision, string) {
	for index, guard := range c.guards {
		decision, reason := guard.Check(definition, call)
		switch decision {
		case GuardDeny:
			return GuardDeny, reason
		case GuardAllow, GuardAbstain, "":
			continue
		default:
			// An unknown decision is treated as a denial. A guard whose answer
			// this build cannot interpret must not be able to authorize work.
			return GuardDeny, fmt.Sprintf("guard %d returned an unknown decision %q", index, decision)
		}
	}
	return GuardAllow, ""
}

// DenyTools is a guard that refuses a fixed set of tool names.
func DenyTools(names ...ToolName) Guard {
	denied := make(map[ToolName]bool, len(names))
	for _, name := range names {
		denied[name] = true
	}
	return GuardFunc(func(definition Definition, call Call) (GuardDecision, string) {
		if denied[definition.Name] {
			return GuardDeny, "tool " + definition.Name.String() + " is not permitted in this scope"
		}
		return GuardAbstain, ""
	})
}

// DenyPrefix is a guard that refuses any tool whose name starts with a prefix.
// It exists so a policy can reserve a namespace, for example "mcp.".
func DenyPrefix(prefix string) Guard {
	return GuardFunc(func(definition Definition, call Call) (GuardDecision, string) {
		if strings.HasPrefix(definition.Name.String(), prefix) {
			return GuardDeny, "tools under " + prefix + " are not permitted in this scope"
		}
		return GuardAbstain, ""
	})
}

// ValidateNames reports whether a name list is free of blanks and duplicates,
// so a policy definition fails at construction rather than at call time.
func ValidateNames(names []ToolName) error {
	seen := make(map[ToolName]bool, len(names))
	for _, name := range names {
		if name.IsZero() {
			return fmt.Errorf("%w: tool name must not be empty", ErrInvalidValue)
		}
		if seen[name] {
			return fmt.Errorf("%w: tool name %q is duplicated", ErrInvalidValue, name)
		}
		seen[name] = true
	}
	return nil
}
