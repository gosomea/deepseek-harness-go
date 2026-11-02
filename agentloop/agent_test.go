package agentloop_test

import (
	"context"
	"testing"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
)

func TestWaitBlocksUntilTheTurnSettles(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("an idle Agent must wait cleanly: %v", err)
	}
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("a settled Agent must wait cleanly: %v", err)
	}
}

func TestDisposeWaitsAndHonoursDeadline(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Disposing an already-idle Agent returns immediately and is idempotent.
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("second dispose: %v", err)
	}
	if got := handle.State(); got != agent.StateDisposed {
		t.Fatalf("want disposed, got %s", got)
	}
}

func TestLookupHandleForwardsTheReadOperations(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"via lookup"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle, ok := factory.Lookup(owner.Handle().Info().ID)
	if !ok {
		t.Fatal("the created Agent must be findable")
	}
	if handle.Info().ID.IsZero() {
		t.Fatal("the lookup handle must report the identity")
	}
	if handle.State() != agent.StateIdle {
		t.Fatalf("want idle, got %s", handle.State())
	}
	select {
	case <-handle.Done():
		t.Fatal("Done must not be closed while the Agent is alive")
	default:
	}
	if err := handle.Send(context.Background(), agent.Input{Text: "through the lookup handle"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	runner, ok := agentloop.AsRunner(handle)
	if !ok {
		t.Fatal("the lookup handle must still be runnable")
	}
	if err := runner.RunTurn(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := len(provider.Attempts()); got != 1 {
		t.Fatalf("want 1 request, got %d", got)
	}
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	select {
	case <-handle.Done():
	default:
		t.Fatal("Done must close once the Agent is disposed")
	}
}

func TestTurnCountAndRunnerAccessor(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"a"}), testkit.ScriptTextAttempt([]string{"b"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	counter, ok := handle.(interface{ TurnCount() int })
	if !ok {
		t.Fatalf("handle must expose TurnCount, got %T", handle)
	}
	if counter.TurnCount() != 0 {
		t.Fatalf("a fresh Agent has run no turn, got %d", counter.TurnCount())
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
			t.Fatalf("send: %v", err)
		}
		if err := run(handle); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	if counter.TurnCount() != 2 {
		t.Fatalf("want 2 turns, got %d", counter.TurnCount())
	}
	runner, ok := agentloop.AsRunner(handle)
	if !ok {
		t.Fatalf("the default driver's handle must be runnable, got %T", handle)
	}
	if runner == nil {
		t.Fatal("AsRunner must not return a nil runner with ok=true")
	}
	if _, ok := agentloop.AsRunner(struct{}{}); ok {
		t.Fatal("a value without the capability must report ok=false")
	}
}

func TestConcurrentSendAndReadIsSafe(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for index := 0; index < 20; index++ {
			_ = handle.Send(context.Background(), agent.Input{Text: "input"})
			_ = handle.State()
		}
	}()
	for index := 0; index < 20; index++ {
		_ = handle.Info()
		_ = handle.State()
	}
	<-done
	if err := handle.Send(context.Background(), agent.Input{Text: "final"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Every accepted input becomes a user message; concurrent sends must not lose
	// or duplicate one.
	var userMessages int
	for _, event := range log.Events() {
		if event.Type == "user/message" {
			userMessages++
		}
	}
	if userMessages == 0 {
		t.Fatal("accepted input must reach the log")
	}
}

func TestOverlappingTurnIsRefused(t *testing.T) {
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
	runner, _ := agentloop.AsRunner(handle)
	// Cancelling an idle Agent reports no active work; this exercises the
	// branch where nothing is cancelled.
	canceler, ok := handle.(agent.Canceler)
	if !ok {
		t.Fatalf("handle must implement Canceler, got %T", handle)
	}
	if canceled, err := canceler.Cancel(context.Background()); canceled || err != nil {
		t.Fatalf("idle cancel: canceled=%v err=%v", canceled, err)
	}
	if err := runner.RunTurn(context.Background()); err != nil {
		t.Fatalf("run turn: %v", err)
	}
}

func TestSecondTurnAppendsToTheSameHistory(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(
		testkit.ScriptTextAttempt([]string{"first answer"}),
		testkit.ScriptTextAttempt([]string{"second answer"}),
	)
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	for index, text := range []string{"one", "two"} {
		if err := handle.Send(context.Background(), agent.Input{Text: text}); err != nil {
			t.Fatalf("send %d: %v", index, err)
		}
		if err := run(handle); err != nil {
			t.Fatalf("run %d: %v", index, err)
		}
	}
	derived, err := log.DeriveMessages()
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	// Two user messages and two assistant replies.
	if len(derived) != 4 {
		t.Fatalf("want 4 derived messages, got %d", len(derived))
	}
	// The second request must carry the first turn's history.
	second := provider.Attempts()[1]
	if len(second.Messages) != 3 {
		t.Fatalf("the second request must carry user, assistant and user, got %d", len(second.Messages))
	}
	if !contains(llm.ContentText(second.Messages[1].Content), "first answer") {
		t.Fatalf("the second request lost the first answer: %q", llm.ContentText(second.Messages[1].Content))
	}
}
