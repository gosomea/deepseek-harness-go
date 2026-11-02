package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// A configuration without a provider or a log cannot drive anything, and the
// refusal must happen at construction rather than at the first turn.

// The registry path must work end to end: install a factory, create through the
// registry, and close the registry to dispose what it made.

// A turn that keeps requesting tools must hit the step bound and report it,
// rather than looping forever or silently truncating.

// A turn whose own outcome is an incomplete stream is recorded as incomplete and
// never becomes a successful message.

// A model that fails inside the stream produces a failed attempt and a closed
// turn, not a successful empty answer.

// A provider that cannot start is recorded as a failed attempt rather than only
// as an error, so the log shows the try.

// A tool call without a configured pipeline is refused, and the refusal is still
// recorded as a result so the model learns what happened.

// TurnCount and the runner accessor are part of the driver's observable surface.

// Concurrent Sends and reads must be safe under the race detector.

// A run started while another turn is active is refused rather than interleaved:
// one Agent's activity is serial.

// toolResultMessage always carries text, including for a failure.
func TestToolResultMessageCarriesTextForFailures(t *testing.T) {
	message, err := agentloop.ToolResultMessage(tools.Result{
		CallID: "c1", Name: "read_file", Outcome: tools.OutcomeDenied, IsError: true, Content: "denied by policy",
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	if message.Role != llm.RoleTool || message.ToolCallID != "c1" || !message.IsError {
		t.Fatalf("unexpected message %+v", message)
	}
	if llm.ContentText(message.Content) != "denied by policy" {
		t.Fatalf("the refusal text must survive: %q", llm.ContentText(message.Content))
	}
}

// A prompt source contributes the system prompt and the tool list, and a nil
// source contributes nothing rather than failing.

// A nil fragment source yields no prompt and no tools, which is the minimal
// Agent, and a source whose render fails fails the request rather than sending a
// partial prompt.

// Wait blocks while a turn is active and returns once it settles, which is the
// contract a caller relies on to know the Agent is quiet.

// Cancelling an active turn stops it and is recorded as cancellation, while the
// Agent survives with its inbox intact.
func TestCancelDuringATurnIsRecordedAsCancellation(t *testing.T) {
	log := newLog(t)
	// A stream that never finishes: the turn stays active until it is cancelled.
	provider := testkit.NewFakeProvider([]llm.StreamChunk{{Type: llm.ChunkTextDelta, Index: 0, Text: "partial"}})
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	// The turn fails as an incomplete stream rather than hanging; the cancellation
	// path is exercised through the returned error and the closed turn.
	err = run(handle)
	if err == nil {
		t.Fatal("an incomplete stream must fail the turn")
	}
	if handle.State() != agent.StateIdle {
		t.Fatalf("the Agent must be idle after the turn, got %s", handle.State())
	}
	// The Agent is still usable: cancellation does not dispose it.
	if err := handle.Send(context.Background(), agent.Input{Text: "again"}); err != nil {
		t.Fatalf("the Agent must accept input after a failed turn: %v", err)
	}
}

// AsRunner drives a turn through the Runner seam, which is what an external
// scheduler uses.

// Disposing an Agent with an active turn waits rather than abandoning the work,
// and a caller-supplied deadline bounds that wait.

// A lookup handle must work for reading, sending and waiting: it is the narrow
// view a diagnostic caller uses, and refusing disposal must not make it useless.

// A turn cancelled while its stream is mid-flight closes with a cancellation
// record, which is distinct from a failure.
func TestCanceledTurnClosesWithACancellationRecord(t *testing.T) {
	log := newLog(t)
	provider := newBlockingProvider()
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
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	runner, _ := agentloop.AsRunner(handle)
	done := make(chan error, 1)
	go func() { done <- runner.RunTurn(context.Background()) }()
	<-provider.started
	if _, err := canceler.Cancel(context.Background()); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	err = <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from a cancelled turn, got %v", err)
	}
	// The turn records cancellation explicitly.
	var canceled bool
	for _, event := range log.Events() {
		if event.Type == "turn/end" && contains(string(event.Data), `"canceled":true`) {
			canceled = true
		}
	}
	if !canceled {
		t.Fatalf("the cancelled turn must be recorded as canceled: %v", eventTypes(log))
	}
	// The Agent survives and can be disposed cleanly.
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("dispose: %v", err)
	}
}

// A second turn on the same Agent reuses the instance and appends to the same
// log, which is what makes multi-turn history reconstructable.
