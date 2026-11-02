// Package approval defines the channel a side effect is cleared through before
// it happens: a request bound to one specific call, and a one-shot answer.
//
// The vocabulary is built so that the safe answer is the one you get by doing
// nothing. Every outcome other than an explicit grant — nobody answered, the
// answer was no, the wait was cancelled — denies the action, and a headless
// policy must say which of those it produces rather than falling through to
// permission.
package approval

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidValue reports an unusable request.
var ErrInvalidValue = errors.New("approval: invalid value")

// ErrNoResponder reports that no responder is attached to the channel.
var ErrNoResponder = errors.New("approval: no responder is available")

// Outcome is what an approval request resolved to.
type Outcome string

// The outcomes this package defines.
//
// only allowed-once authorizes anything. The other three are distinct so a
// caller can report why an action did not run; treating them as one would make
// "the operator said no" indistinguishable from "nobody was watching".
const (
	// OutcomeAllowedOnce authorizes exactly the call the request named.
	OutcomeAllowedOnce Outcome = "allowed-once"
	// OutcomeRejected means a responder declined.
	OutcomeRejected Outcome = "rejected"
	// OutcomeCancelled means the wait was cancelled before an answer arrived.
	OutcomeCancelled Outcome = "cancelled"
	// OutcomeUnavailable means no responder could answer.
	OutcomeUnavailable Outcome = "unavailable"
)

// Granted reports whether the outcome authorizes the action.
//
// It is the single place that decides, so no caller has to remember that three
// of the four outcomes deny.
func (o Outcome) Granted() bool { return o == OutcomeAllowedOnce }

// Valid reports whether the outcome is one this package defines.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeAllowedOnce, OutcomeRejected, OutcomeCancelled, OutcomeUnavailable:
		return true
	default:
		return false
	}
}

// Request identifies one side effect awaiting a decision.
type Request struct {
	// ID identifies the approval exchange.
	ID string
	// CallID binds the request to one specific tool call.
	//
	// The binding is what makes a grant one-shot: an answer meant for one call
	// must not authorize a different one, however similar.
	CallID string
	// Tool names the tool that would act.
	Tool string
	// Action describes what would happen.
	Action string
	// Justification is the caller's reason, shown to the responder.
	Justification string
	// ExpiresAt optionally bounds how long the request waits.
	ExpiresAt time.Time
}

// Validate reports whether the request is usable.
//
// A request without both an exchange identity and a call identity is refused:
// the identity is what a grant is bound to, and a grant bound to nothing is a
// blanket permission.
func (r Request) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("%w: an approval request needs an id", ErrInvalidValue)
	}
	if r.CallID == "" {
		return fmt.Errorf("%w: an approval request must be bound to a call", ErrInvalidValue)
	}
	if r.Tool == "" {
		return fmt.Errorf("%w: an approval request must name the tool", ErrInvalidValue)
	}
	return nil
}

// Decision is one resolved approval.
type Decision struct {
	// Request is the exchange that was decided.
	Request Request
	// Outcome is the answer.
	Outcome Outcome
	// Reason explains a denial, when there is one.
	Reason string
	// DecidedAt records when the answer arrived.
	DecidedAt time.Time
}

// Granted reports whether this decision authorizes the request it names.
func (d Decision) Granted() bool { return d.Outcome.Granted() }

// Channel presents requests and receives at most one decision each.
type Channel interface {
	// Request asks about one side effect.
	//
	// It returns a decision in every case: a channel with no responder produces
	// OutcomeUnavailable rather than an error a caller might mistake for success,
	// and a cancelled wait produces OutcomeCancelled. Neither authorizes.
	Request(ctx context.Context, request Request) (Decision, error)
}

// Policy decides what happens when no responder can answer.
type Policy string

// The headless policies this package defines.
const (
	// PolicyDeny refuses when nobody can answer.
	PolicyDeny Policy = "deny"
	// PolicyAskOnce asks and allows at most the one call, refusing if no
	// responder can answer.
	PolicyAskOnce Policy = "ask-once"
)

// Valid reports whether the policy is one this package defines.
//
// There is deliberately no "allow" policy: a headless run that silently
// permitted side effects would make every other guard optional.
func (p Policy) Valid() bool { return p == PolicyDeny || p == PolicyAskOnce }

// Resolve turns a policy and an outcome into the decision a caller acts on.
//
// It exists so a headless decision is computed in one auditable place: under
// PolicyDeny nothing is ever granted, and under PolicyAskOnce only an explicit
// allowed-once grant is.
func Resolve(policy Policy, request Request, outcome Outcome) (Decision, error) {
	if err := request.Validate(); err != nil {
		return Decision{}, err
	}
	if !policy.Valid() {
		return Decision{}, fmt.Errorf("%w: unknown policy %q", ErrInvalidValue, policy)
	}
	if !outcome.Valid() {
		return Decision{}, fmt.Errorf("%w: unknown outcome %q", ErrInvalidValue, outcome)
	}
	if policy == PolicyDeny {
		return Decision{Request: request, Outcome: OutcomeRejected, Reason: "headless policy denies without asking"}, nil
	}
	if outcome == OutcomeUnavailable {
		// Asking produced no answer, which is not permission.
		return Decision{Request: request, Outcome: OutcomeUnavailable, Reason: "no responder was available"}, nil
	}
	return Decision{Request: request, Outcome: outcome}, nil
}
