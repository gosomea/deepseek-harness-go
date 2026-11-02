package approval

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Responder answers approval requests.
//
// It is deliberately a minimal shape rather than a service interface: the
// tool layer owns the agent, the call id and the tool name, and closes over them
// when it asks. A channel that needed to know about agents would make the
// approval package depend on the very thing it guards.
type Responder interface {
	// Answer returns the decision for one request, or OutcomeUnavailable when it
	// will not answer it.
	Answer(ctx context.Context, request Request) Outcome
}

// Gateway is a Channel that presents requests to an optional responder.
//
// It exists as a concrete type because every provider needs the same bookkeeping
// done correctly: one answer per exchange, later answers refused, and an
// uncancellable decision that is not silently replaced. Those rules live here
// once.
type Gateway struct {
	// mu guards the exchange table.
	mu sync.Mutex
	// responder answers requests; nil means nobody can.
	responder Responder
	// policy decides what happens when there is no answer.
	policy Policy
	// exchanges records every exchange this gateway has presented.
	exchanges map[string]*exchange
	// clock supplies timestamps, so a test can freeze time.
	clock func() time.Time
	// now records how many requests were presented, for diagnostics.
	presented int
}

// exchange is one presented request and its outcome.
type exchange struct {
	// request is the exchange that was presented.
	request Request
	// decision is the answer, once one has been recorded.
	decision *Decision
	// settled is closed when a decision has been recorded.
	//
	// It exists because "already presented" and "already answered" are different
	// moments. A second presenter that arrived before the answer must WAIT for it
	// and receive the same decision; answering it immediately with a denial would
	// give two callers of one exchange two different outcomes, which is the race
	// this field removes.
	settled chan struct{}
}

// NewGateway builds a channel over an optional responder and a headless policy.
//
// A nil responder is allowed and means "nobody can answer": requests under
// PolicyAskOnce resolve to OutcomeUnavailable, which does not authorize. That is
// a supported configuration, not a misconfiguration — a headless run is
// precisely the case the policy exists for.
func NewGateway(responder Responder, policy Policy) (*Gateway, error) {
	if !policy.Valid() {
		return nil, fmt.Errorf("%w: unknown policy %q", ErrInvalidValue, policy)
	}
	return &Gateway{
		responder: responder,
		policy:    policy,
		exchanges: map[string]*exchange{},
		clock:     time.Now,
	}, nil
}

// SetClock replaces the time source. It exists so a test can assert an expiry
// without waiting for one.
func (g *Gateway) SetClock(clock func() time.Time) error {
	if clock == nil {
		return fmt.Errorf("%w: a clock is required", ErrInvalidValue)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clock = clock
	return nil
}

// Policy reports the headless policy in force.
func (g *Gateway) Policy() Policy { return g.policy }

// Presented reports how many exchanges this gateway has presented.
func (g *Gateway) Presented() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.presented
}

// Request asks about one side effect and returns a decision in every case.
//
// The error return is reserved for a malformed request: an approval that did not
// happen is an outcome, not a failure to be retried. A caller that checked only
// the error would otherwise treat "nobody answered" as permission.
func (g *Gateway) Request(ctx context.Context, request Request) (Decision, error) {
	if err := request.Validate(); err != nil {
		return Decision{}, err
	}
	if g == nil {
		return Decision{}, fmt.Errorf("%w: gateway is nil", ErrInvalidValue)
	}
	g.mu.Lock()
	if existing, seen := g.exchanges[request.ID]; seen {
		g.mu.Unlock()
		// Wait for the exchange to settle, then return the recorded answer. Both
		// callers of one exchange therefore observe the same outcome however the
		// two raced: the first to arrive raises the question, the others receive
		// its answer.
		select {
		case <-existing.settled:
		case <-ctx.Done():
			return Decision{Request: existing.request, Outcome: OutcomeCancelled, Reason: "the wait was cancelled"}, nil
		}
		g.mu.Lock()
		decision := Decision{Request: existing.request, Outcome: OutcomeRejected, Reason: "exchange was already presented"}
		if existing.decision != nil {
			decision = *existing.decision
		}
		g.mu.Unlock()
		return decision, nil
	}
	created := &exchange{request: request, settled: make(chan struct{})}
	g.exchanges[request.ID] = created
	g.presented++
	responder := g.responder
	clock := g.clock
	g.mu.Unlock()

	if !request.ExpiresAt.IsZero() && !clock().Before(request.ExpiresAt) {
		return g.record(request, OutcomeCancelled, "request had already expired")
	}

	outcome := OutcomeUnavailable
	if responder != nil {
		outcome = responder.Answer(ctx, request)
		if !outcome.Valid() {
			// A responder that returned something off the vocabulary has not
			// granted anything; an unknown value must not be read as permission.
			outcome = OutcomeUnavailable
		}
	}
	// A cancelled wait denies even when a responder was present: the answer would
	// have authorized an action nobody was waiting for any more.
	if err := ctx.Err(); err != nil {
		return g.record(request, OutcomeCancelled, "the wait was cancelled")
	}
	return g.record(request, outcome, "")
}

// record stores and resolves one decision, applying the headless policy.
func (g *Gateway) record(request Request, outcome Outcome, reason string) (Decision, error) {
	decision, err := Resolve(g.policy, request, outcome)
	if err != nil {
		return Decision{}, err
	}
	if reason != "" && decision.Reason == "" {
		decision.Reason = reason
	}
	decision.DecidedAt = g.clock()
	g.mu.Lock()
	defer g.mu.Unlock()
	if entry, ok := g.exchanges[request.ID]; ok {
		if entry.decision == nil {
			stored := decision
			entry.decision = &stored
			// Wake every waiter now that the answer exists. Closing happens under
			// the lock so a waiter that observes the close also observes the
			// decision it was waiting for.
			close(entry.settled)
		}
		// A second recording must not replace the first: a late answer arriving
		// after a denial must not turn that denial into a grant.
		decision = *entry.decision
	}
	return decision, nil
}

// Decide records a late or out-of-band answer for an exchange.
//
// It exists for the case the reference calls a late answer: a human replies after
// the caller already gave up. Recording it must not change what the caller
// already acted on, so an exchange that already has a decision keeps it.
func (g *Gateway) Decide(id string, outcome Outcome, reason string) (Decision, error) {
	if !outcome.Valid() {
		return Decision{}, fmt.Errorf("%w: unknown outcome %q", ErrInvalidValue, outcome)
	}
	g.mu.Lock()
	entry, ok := g.exchanges[id]
	g.mu.Unlock()
	if !ok {
		return Decision{}, fmt.Errorf("%w: no exchange %q", ErrInvalidValue, id)
	}
	return g.record(entry.request, outcome, reason)
}

// Decision returns a recorded decision, if there is one.
func (g *Gateway) Decision(id string) (Decision, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.exchanges[id]
	if !ok || entry.decision == nil {
		return Decision{}, false
	}
	return *entry.decision, true
}

// AlwaysResponder answers every request with one fixed outcome.
//
// It is a test and demonstration responder, not a policy: nothing in production
// should answer "allowed" without a human.
type AlwaysResponder struct {
	// Outcome is the answer to give.
	Outcome Outcome
	// Calls records the requests it answered.
	Calls []Request
}

// Answer records the request and returns the fixed outcome.
func (r *AlwaysResponder) Answer(_ context.Context, request Request) Outcome {
	r.Calls = append(r.Calls, request)
	return r.Outcome
}
