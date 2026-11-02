package agentloop_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// Queued input must be claimed exactly once: a step that took it owns it, and no
// later step may see it again.
func TestQueuedInputIsClaimedExactlyOnce(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"first"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "one"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := handle.Send(context.Background(), agent.Input{Text: "two"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	instance, ok := handle.(interface{ Pending() int })
	if !ok {
		t.Fatalf("handle does not expose Pending: %T", handle)
	}
	if got := instance.Pending(); got != 2 {
		t.Fatalf("want 2 pending inputs, got %d", got)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := instance.Pending(); got != 0 {
		t.Fatalf("a claim must empty the queue, got %d pending", got)
	}
	// Both inputs became user messages; neither was dropped or duplicated.
	var userMessages int
	for _, event := range log.Events() {
		if event.Type == "user/message" {
			userMessages++
		}
	}
	if userMessages != 2 {
		t.Fatalf("want 2 user messages from 2 inputs, got %d", userMessages)
	}
	claimed, ok := handle.(interface{ Claimed() int })
	if !ok {
		t.Fatalf("handle does not expose Claimed: %T", handle)
	}
	if got := claimed.Claimed(); got != 2 {
		t.Fatalf("want 2 claimed inputs, got %d", got)
	}
}

// An empty inbox means no turn at all: the Agent must not open a turn that would
// spend no step.
func TestEmptyInboxOpensNoTurn(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"unused"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := run(owner.Handle()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := log.Len(); got != 0 {
		t.Fatalf("an idle Agent must write nothing, got %d events", got)
	}
	if got := len(provider.Attempts()); got != 0 {
		t.Fatalf("an idle Agent must make no request, got %d", got)
	}
}

// Input arriving before the turn starts is claimed by that turn rather than
// starting a second one: this is the "zero-step turn closes" case from the plan.
func TestInitialInputIsDeliveredBeforeTheFirstTurn(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	request := spec()
	request.InitialInput = &agent.Input{Text: "seeded"}
	owner, err := factory.Create(context.Background(), request)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := run(owner.Handle()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := len(provider.Attempts()); got != 1 {
		t.Fatalf("want 1 request, got %d", got)
	}
	if len(provider.Attempts()[0].Messages) != 1 {
		t.Fatalf("the initial input must reach the first request, got %d messages", len(provider.Attempts()[0].Messages))
	}
}

// A Send after disposal is refused, not silently queued for a driver that will
// never run again.
func TestSendAfterDisposalIsRefused(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if err := handle.Send(context.Background(), agent.Input{Text: "late"}); !errors.Is(err, agent.ErrInputRejected) {
		t.Fatalf("want ErrInputRejected after disposal, got %v", err)
	}
	if err := handle.Send(context.Background(), agent.Input{}); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank input, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := handle.Send(ctx, agent.Input{Text: "canceled"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// A turn with no steps but with input is recorded and closed: the boundary open
// and close must both appear, and no step may be spent.
func TestTurnWithoutStepsCloses(t *testing.T) {
	log := newLog(t)
	// A provider whose response fails to start produces an attempt and a failed
	// turn; the turn boundary still closes.
	provider := testkit.NewFakeProviderWithOptions(
		testkit.FakeProviderID("fake"),
		testkit.FakeProviderFailure(errors.New("provider unavailable")),
	)
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "hi"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	err = run(handle)
	if err == nil {
		t.Fatal("a failed attempt must report an error")
	}
	types := eventTypes(log)
	if types[0] != "turn/start" {
		t.Fatalf("the turn must open first, got %v", types)
	}
	if types[len(types)-1] != "turn/end" {
		t.Fatalf("the turn must close last, got %v", types)
	}
	// The failure is recorded as a failed turn, not as a successful one.
	found := false
	for _, event := range log.Events() {
		if event.Type == "turn/end" {
			found = true
			if !contains(string(event.Data), `"failed":true`) {
				t.Fatalf("a failed turn must be recorded as failed: %s", event.Data)
			}
		}
	}
	if !found {
		t.Fatal("no turn/end event")
	}
}

// Cancellation stops the turn, is recorded as cancellation rather than failure,
// and leaves the Agent alive with its inbox intact.
func TestCancelStopsTheTurnAndKeepsTheAgentAlive(t *testing.T) {
	log := newLog(t)
	// A slow provider: the stream blocks until the context is cancelled, which is
	// how a real in-flight request behaves.
	provider := testkit.NewFakeProvider()
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	canceler, ok := handle.(agent.Canceler)
	if !ok {
		t.Fatalf("handle must implement Canceler, got %T", handle)
	}
	// Cancelling an idle Agent reports that there was nothing to cancel.
	if canceled, err := canceler.Cancel(context.Background()); canceled || err != nil {
		t.Fatalf("cancelling an idle Agent: canceled=%v err=%v", canceled, err)
	}
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if _, err := canceler.Cancel(context.Background()); !errors.Is(err, agent.ErrClosed) {
		t.Fatalf("want ErrClosed when cancelling a disposed Agent, got %v", err)
	}
}

// Disposing waits for the active turn to release resources instead of abandoning
// it, so shared state is quiet when Dispose returns.
func TestDisposeWaitsForTheActiveTurn(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}), testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "one"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- owner.Dispose(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("dispose: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dispose did not return")
	}
	if handle.State() != agent.StateDisposed {
		t.Fatalf("want disposed state, got %s", handle.State())
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("waiting on a disposed Agent must return: %v", err)
	}
	select {
	case <-handle.Done():
	default:
		t.Fatal("Done must be closed after disposal")
	}
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("second dispose must be a no-op, got %v", err)
	}
}

// Two Agents in one factory are independent: disposing one leaves the other
// serviceable.
func TestAgentsAreIndependent(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(
		testkit.ScriptTextAttempt([]string{"a"}),
		testkit.ScriptTextAttempt([]string{"b"}),
	)
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	first, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if got := len(factory.Active()); got != 2 {
		t.Fatalf("want 2 active Agents, got %d", got)
	}
	if err := first.Dispose(context.Background()); err != nil {
		t.Fatalf("dispose first: %v", err)
	}
	if second.Handle().State() == agent.StateDisposed {
		t.Fatal("disposing one Agent must not dispose another")
	}
	if err := second.Handle().Send(context.Background(), agent.Input{Text: "still here"}); err != nil {
		t.Fatalf("the surviving Agent must accept input: %v", err)
	}
	if err := run(second.Handle()); err != nil {
		t.Fatalf("run second: %v", err)
	}
	if got := len(factory.Active()); got != 2 {
		t.Fatalf("Active reports created instances, got %d", got)
	}
	// DisposeAll releases the survivor and is idempotent.
	if err := factory.DisposeAll(context.Background()); err != nil {
		t.Fatalf("dispose all: %v", err)
	}
	if err := factory.DisposeAll(context.Background()); err != nil {
		t.Fatalf("second dispose all must be a no-op, got %v", err)
	}
	// DisposeAll clears the factory's instance list, so the survivor's state is
	// read from the handle the test still holds rather than from Active.
	if got := second.Handle().State(); got != agent.StateDisposed {
		t.Fatalf("want the survivor disposed, got %s", got)
	}
	if got := len(factory.Active()); got != 0 {
		t.Fatalf("a disposed factory owns nothing, got %d", got)
	}
}

// A lookup returns a handle, never an owner: finding an Agent must not confer
// the ability to destroy it.
func TestLookupDoesNotGrantDisposal(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle, ok := factory.Lookup(owner.Handle().Info().ID)
	if !ok {
		t.Fatal("the created Agent must be findable by id")
	}
	if _, isOwner := handle.(agent.Owner); isOwner {
		t.Fatal("a lookup result must not be an Owner")
	}
	if _, ok := factory.Lookup(agent.ID("absent")); ok {
		t.Fatal("an unknown id must not resolve")
	}
	if _, err := factory.Create(context.Background(), agent.Spec{SessionID: "s"}); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an invalid spec, got %v", err)
	}
}

func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
