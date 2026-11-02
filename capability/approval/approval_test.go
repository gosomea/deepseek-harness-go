package approval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/approval"
)

func request(id, callID string) approval.Request {
	return approval.Request{ID: id, CallID: callID, Tool: "write_file", Action: "write notes.txt"}
}

// A request must be bound to one call: a grant bound to nothing is a blanket
// permission.
func TestRequestRequiresBothIdentities(t *testing.T) {
	if err := request("a-1", "call-1").Validate(); err != nil {
		t.Fatalf("a complete request must validate: %v", err)
	}
	for name, bad := range map[string]approval.Request{
		"no exchange id": {CallID: "call-1", Tool: "t"},
		"no call id":     {ID: "a-1", Tool: "t"},
		"no tool":        {ID: "a-1", CallID: "call-1"},
	} {
		if err := bad.Validate(); !errors.Is(err, approval.ErrInvalidValue) {
			t.Fatalf("%s: want ErrInvalidValue, got %v", name, err)
		}
	}
}

// Exactly one outcome grants; the other three deny, and they stay distinct.
func TestOnlyAllowedOnceGrants(t *testing.T) {
	outcomes := []approval.Outcome{
		approval.OutcomeAllowedOnce, approval.OutcomeRejected,
		approval.OutcomeCancelled, approval.OutcomeUnavailable,
	}
	granted := 0
	for _, outcome := range outcomes {
		if !outcome.Valid() {
			t.Fatalf("outcome %q must be valid", outcome)
		}
		if outcome.Granted() {
			granted++
		}
	}
	if granted != 1 {
		t.Fatalf("exactly one outcome may grant, got %d", granted)
	}
	if !approval.OutcomeAllowedOnce.Granted() {
		t.Fatal("allowed-once must grant")
	}
	for _, outcome := range []approval.Outcome{approval.OutcomeRejected, approval.OutcomeCancelled, approval.OutcomeUnavailable} {
		if outcome.Granted() {
			t.Fatalf("outcome %q must not grant", outcome)
		}
	}
	if approval.Outcome("invented").Valid() {
		t.Fatal("an unknown outcome must not be valid")
	}
}

// A headless run under deny never grants, whatever the answer was.
func TestDenyPolicyNeverGrants(t *testing.T) {
	policy := approval.PolicyDeny
	for _, outcome := range []approval.Outcome{
		approval.OutcomeAllowedOnce, approval.OutcomeRejected,
		approval.OutcomeCancelled, approval.OutcomeUnavailable,
	} {
		decision, err := approval.Resolve(policy, request("a-1", "call-1"), outcome)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if decision.Granted() {
			t.Fatalf("policy deny must not grant for outcome %q", outcome)
		}
		if decision.Outcome != approval.OutcomeRejected {
			t.Fatalf("policy deny must report a rejection, got %q", decision.Outcome)
		}
	}
}

// Asking under ask-once grants only an explicit allowed-once; an unavailable
// responder is not permission.
func TestAskOnceGrantsOnlyAnExplicitGrant(t *testing.T) {
	policy := approval.PolicyAskOnce
	allowed, err := approval.Resolve(policy, request("a-1", "call-1"), approval.OutcomeAllowedOnce)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !allowed.Granted() {
		t.Fatal("an explicit grant must authorize")
	}
	for _, outcome := range []approval.Outcome{approval.OutcomeRejected, approval.OutcomeCancelled, approval.OutcomeUnavailable} {
		decision, err := approval.Resolve(policy, request("a-1", "call-1"), outcome)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if decision.Granted() {
			t.Fatalf("outcome %q must not authorize under ask-once", outcome)
		}
	}
	// The unavailable case gives a reason a caller can report.
	unavailable, err := approval.Resolve(policy, request("a-1", "call-1"), approval.OutcomeUnavailable)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if unavailable.Reason == "" {
		t.Fatal("an unavailable responder must carry an explanation")
	}
}

// Resolve validates its inputs rather than guessing.
func TestResolveRefusals(t *testing.T) {
	if _, err := approval.Resolve(approval.PolicyAskOnce, approval.Request{}, approval.OutcomeAllowedOnce); !errors.Is(err, approval.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an invalid request, got %v", err)
	}
	if _, err := approval.Resolve("invented", request("a-1", "call-1"), approval.OutcomeAllowedOnce); !errors.Is(err, approval.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unknown policy, got %v", err)
	}
	if _, err := approval.Resolve(approval.PolicyAskOnce, request("a-1", "call-1"), "invented"); !errors.Is(err, approval.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unknown outcome, got %v", err)
	}
	// There is deliberately no policy that allows without asking.
	if approval.Policy("allow").Valid() {
		t.Fatal("an allow policy must not exist")
	}
	if !approval.PolicyDeny.Valid() || !approval.PolicyAskOnce.Valid() {
		t.Fatal("the two defined policies must be valid")
	}
}

// A decision reports what it authorized, so a caller can log the binding.
func TestDecisionReportsItsRequest(t *testing.T) {
	decision, err := approval.Resolve(approval.PolicyAskOnce, request("a-9", "call-9"), approval.OutcomeAllowedOnce)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if decision.Request.CallID != "call-9" || decision.Request.ID != "a-9" {
		t.Fatalf("the decision must name the call it covers: %+v", decision.Request)
	}
	if !decision.Granted() {
		t.Fatal("an explicit grant must be reported as granted")
	}
	// A stated expiry is carried through untouched.
	expiring := request("a-2", "call-2")
	expiring.ExpiresAt = time.UnixMilli(1_700_000_000_000)
	if err := expiring.Validate(); err != nil {
		t.Fatalf("an expiring request must validate: %v", err)
	}
	_ = context.Background()
}
