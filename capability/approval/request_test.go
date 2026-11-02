package approval_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/approval"
)

// A responder that grants lets the action through, and the grant is bound to the
// call it named.
func TestGatewayGrantsOnlyAnExplicitAnswer(t *testing.T) {
	responder := &approval.AlwaysResponder{Outcome: approval.OutcomeAllowedOnce}
	gateway, err := approval.NewGateway(responder, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	decision, err := gateway.Request(context.Background(), request("a-1", "call-1"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !decision.Granted() {
		t.Fatalf("an explicit grant must authorize, got %+v", decision)
	}
	if decision.Request.CallID != "call-1" {
		t.Fatalf("the grant must stay bound to its call: %+v", decision.Request)
	}
	if len(responder.Calls) != 1 {
		t.Fatalf("the responder must have been asked once, got %d", len(responder.Calls))
	}
	if gateway.Presented() != 1 {
		t.Fatalf("want one presentation, got %d", gateway.Presented())
	}
}

// With no responder, asking produces unavailable, which does not authorize.
func TestGatewayWithoutAResponderDenies(t *testing.T) {
	gateway, err := approval.NewGateway(nil, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	decision, err := gateway.Request(context.Background(), request("a-1", "call-1"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if decision.Granted() {
		t.Fatal("nobody answered, so nothing may be authorized")
	}
	if decision.Outcome != approval.OutcomeUnavailable {
		t.Fatalf("want unavailable, got %q", decision.Outcome)
	}
	if decision.Reason == "" {
		t.Fatal("the denial must explain itself")
	}
}

// A cancelled wait denies even with a willing responder: the answer would have
// authorized something nobody was waiting for.
func TestGatewayCancelledWaitDenies(t *testing.T) {
	responder := &approval.AlwaysResponder{Outcome: approval.OutcomeAllowedOnce}
	gateway, err := approval.NewGateway(responder, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	decision, err := gateway.Request(ctx, request("a-1", "call-1"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if decision.Granted() {
		t.Fatal("a cancelled wait must not authorize")
	}
	if decision.Outcome != approval.OutcomeCancelled {
		t.Fatalf("want cancelled, got %q", decision.Outcome)
	}
}

// A repeated presentation must not produce a second answer, so a caller cannot
// ask until it gets a yes.
func TestGatewayRefusesASecondPresentation(t *testing.T) {
	responder := &approval.AlwaysResponder{Outcome: approval.OutcomeRejected}
	gateway, err := approval.NewGateway(responder, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	first, err := gateway.Request(context.Background(), request("a-1", "call-1"))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Granted() {
		t.Fatal("the first answer was a rejection")
	}
	second, err := gateway.Request(context.Background(), request("a-1", "call-1"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Granted() {
		t.Fatal("re-presenting one exchange must not authorize it")
	}
	if second.Outcome != first.Outcome {
		t.Fatalf("the recorded answer must stand: %q vs %q", second.Outcome, first.Outcome)
	}
	if len(responder.Calls) != 1 {
		t.Fatalf("the responder must not be asked twice, got %d", len(responder.Calls))
	}
	if gateway.Presented() != 1 {
		t.Fatalf("want one presentation, got %d", gateway.Presented())
	}
}

// A late answer must not upgrade an exchange the caller already acted on.
func TestLateAnswerCannotUpgradeADenial(t *testing.T) {
	gateway, err := approval.NewGateway(nil, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	decision, err := gateway.Request(context.Background(), request("a-1", "call-1"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if decision.Granted() {
		t.Fatal("no responder means no grant")
	}
	// The human replies afterwards, allowing it.
	late, err := gateway.Decide("a-1", approval.OutcomeAllowedOnce, "answered late")
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if late.Granted() {
		t.Fatal("a late grant must not rewrite what the caller already acted on")
	}
	if late.Outcome != decision.Outcome {
		t.Fatalf("the first recorded answer must stand: %q vs %q", late.Outcome, decision.Outcome)
	}
	if stored, ok := gateway.Decision("a-1"); !ok || stored.Granted() {
		t.Fatalf("the stored decision must stay a denial: %+v", stored)
	}
}

// An expired request is cancelled rather than answered.
func TestExpiredRequestIsCancelled(t *testing.T) {
	responder := &approval.AlwaysResponder{Outcome: approval.OutcomeAllowedOnce}
	gateway, err := approval.NewGateway(responder, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	frozen := time.UnixMilli(1_700_000_000_000)
	if err := gateway.SetClock(func() time.Time { return frozen }); err != nil {
		t.Fatalf("clock: %v", err)
	}
	expired := request("a-1", "call-1")
	expired.ExpiresAt = frozen.Add(-time.Second)
	decision, err := gateway.Request(context.Background(), expired)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if decision.Granted() {
		t.Fatal("an expired request must not authorize")
	}
	if decision.Outcome != approval.OutcomeCancelled {
		t.Fatalf("want cancelled, got %q", decision.Outcome)
	}
	if len(responder.Calls) != 0 {
		t.Fatal("an expired request must not reach the responder")
	}
	if decision.DecidedAt != frozen {
		t.Fatalf("the decision must be stamped by the gateway clock: %v", decision.DecidedAt)
	}
}

// A responder returning an unknown outcome grants nothing.
func TestUnknownResponderOutcomeGrantsNothing(t *testing.T) {
	gateway, err := approval.NewGateway(&approval.AlwaysResponder{Outcome: approval.Outcome("maybe")}, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	decision, err := gateway.Request(context.Background(), request("a-1", "call-1"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if decision.Granted() {
		t.Fatal("an unknown outcome must not be read as permission")
	}
	if decision.Outcome != approval.OutcomeUnavailable {
		t.Fatalf("want unavailable, got %q", decision.Outcome)
	}
}

// The gateway validates its own configuration and inputs.
func TestGatewayRefusals(t *testing.T) {
	if _, err := approval.NewGateway(nil, "invented"); !errors.Is(err, approval.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unknown policy, got %v", err)
	}
	gateway, err := approval.NewGateway(nil, approval.PolicyDeny)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	if gateway.Policy() != approval.PolicyDeny {
		t.Fatalf("unexpected policy %q", gateway.Policy())
	}
	if _, err := gateway.Request(context.Background(), approval.Request{}); !errors.Is(err, approval.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a malformed request, got %v", err)
	}
	if _, err := gateway.Decide("absent", approval.OutcomeAllowedOnce, ""); !errors.Is(err, approval.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unknown exchange, got %v", err)
	}
	if _, err := gateway.Decide("a-1", "invented", ""); !errors.Is(err, approval.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unknown outcome, got %v", err)
	}
	if err := gateway.SetClock(nil); !errors.Is(err, approval.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a nil clock, got %v", err)
	}
	if _, ok := gateway.Decision("absent"); ok {
		t.Fatal("an unknown exchange has no decision")
	}
}

// Deny never grants even with a willing responder: the policy is not a hint.
func TestDenyIgnoresAWillingResponder(t *testing.T) {
	responder := &approval.AlwaysResponder{Outcome: approval.OutcomeAllowedOnce}
	gateway, err := approval.NewGateway(responder, approval.PolicyDeny)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	decision, err := gateway.Request(context.Background(), request("a-1", "call-1"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if decision.Granted() {
		t.Fatal("policy deny must not grant")
	}
	if decision.Outcome != approval.OutcomeRejected {
		t.Fatalf("want rejected, got %q", decision.Outcome)
	}
}

// Concurrent presentations of one exchange record exactly one answer.
func TestConcurrentPresentationsRecordOneAnswer(t *testing.T) {
	responder := &approval.AlwaysResponder{Outcome: approval.OutcomeAllowedOnce}
	gateway, err := approval.NewGateway(responder, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	const attempts = 8
	var group sync.WaitGroup
	start := make(chan struct{})
	decisions := make([]approval.Decision, attempts)
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func(slot int) {
			defer group.Done()
			<-start
			decision, err := gateway.Request(context.Background(), request("a-shared", "call-1"))
			if err != nil {
				t.Errorf("request: %v", err)
				return
			}
			decisions[slot] = decision
		}(index)
	}
	close(start)
	group.Wait()
	first := decisions[0]
	for index, decision := range decisions {
		if decision.Outcome != first.Outcome {
			t.Fatalf("caller %d saw %q while caller 0 saw %q", index, decision.Outcome, first.Outcome)
		}
	}
	if gateway.Presented() != 1 {
		t.Fatalf("want one presentation, got %d", gateway.Presented())
	}
}

// A second presenter that arrives while the first question is still open must
// wait for its answer rather than being told the exchange is closed.
//
// This is a regression test for a real race: answering the latecomer immediately
// with a denial gave two callers of one exchange two different outcomes, and
// which one a caller saw depended on scheduling.
func TestSecondPresenterWaitsForTheOpenQuestion(t *testing.T) {
	release := make(chan struct{})
	blocking := newBlockingResponder(release, approval.OutcomeAllowedOnce)
	gateway, err := approval.NewGateway(blocking, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	// The first caller raises the question and blocks inside the responder.
	firstResult := make(chan approval.Decision, 1)
	go func() {
		decision, err := gateway.Request(context.Background(), request("a-1", "call-1"))
		if err != nil {
			t.Errorf("first: %v", err)
		}
		firstResult <- decision
	}()
	<-blocking.started
	// The second caller arrives while the question is still open.
	secondResult := make(chan approval.Decision, 1)
	go func() {
		decision, err := gateway.Request(context.Background(), request("a-1", "call-1"))
		if err != nil {
			t.Errorf("second: %v", err)
		}
		secondResult <- decision
	}()
	// It must not have been answered yet: nothing has decided this exchange.
	select {
	case decision := <-secondResult:
		t.Fatalf("the second presenter was answered before the question settled: %+v", decision)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	first := <-firstResult
	second := <-secondResult
	if first.Outcome != second.Outcome {
		t.Fatalf("both callers must see one answer: %q vs %q", first.Outcome, second.Outcome)
	}
	if !second.Granted() {
		t.Fatalf("the shared answer must be the recorded grant: %+v", second)
	}
	if blocking.asked() != 1 {
		t.Fatalf("the responder must be asked once, got %d", blocking.asked())
	}
}

// A presenter that gives up while waiting for another's answer is cancelled
// rather than left blocked.
func TestSecondPresenterCancellationIsHonoured(t *testing.T) {
	release := make(chan struct{})
	blocking := newBlockingResponder(release, approval.OutcomeAllowedOnce)
	gateway, err := approval.NewGateway(blocking, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	go func() {
		_, _ = gateway.Request(context.Background(), request("a-1", "call-1"))
	}()
	<-blocking.started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	decision, err := gateway.Request(ctx, request("a-1", "call-1"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if decision.Granted() {
		t.Fatal("a cancelled waiter must not receive authorization")
	}
	if decision.Outcome != approval.OutcomeCancelled {
		t.Fatalf("want cancelled, got %q", decision.Outcome)
	}
	close(release)
}

// blockingResponder blocks until released, so a test can hold a question open.
type blockingResponder struct {
	// release unblocks the answer.
	release chan struct{}
	// outcome is the answer to give.
	outcome approval.Outcome
	// started is closed once the first question has been received.
	started chan struct{}
	// mu guards the call count.
	mu sync.Mutex
	// calls counts the questions received.
	calls int
}

// newBlockingResponder builds a responder whose started channel exists up front,
// so a test cannot race its own constructor against the answer.
func newBlockingResponder(release chan struct{}, outcome approval.Outcome) *blockingResponder {
	return &blockingResponder{release: release, outcome: outcome, started: make(chan struct{})}
}

// Answer blocks until released, then returns the configured outcome.
func (r *blockingResponder) Answer(_ context.Context, _ approval.Request) approval.Outcome {

	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	if r.firstCall() {
		close(r.started)
	}
	<-r.release
	return r.outcome
}

// firstCall reports whether this invocation is the first.
func (r *blockingResponder) firstCall() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls == 1
}

// asked reports how many questions were received.
func (r *blockingResponder) asked() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}
