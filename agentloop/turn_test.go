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

func TestStepLimitStopsARunawayTurn(t *testing.T) {
	log := newLog(t)
	var bodyRuns int
	// Every attempt requests another call, so the only thing that can stop the
	// turn is the bound.
	scripts := make([][]llm.StreamChunk, 0, 4)
	for index := 0; index < 4; index++ {
		scripts = append(scripts, testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"path":"a.txt"}`}))
	}
	provider := testkit.NewFakeProvider(scripts...)
	factory := newFactory(t, agentloop.Config{
		Provider: provider, Log: log, Pipeline: readFileTool(t, &bodyRuns), MaxSteps: 2,
	})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "loop"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	err = run(handle)
	if !errors.Is(err, agentloop.ErrStepLimit) {
		t.Fatalf("want ErrStepLimit, got %v", err)
	}
	if bodyRuns != 2 {
		t.Fatalf("want the bound to allow 2 steps, got %d tool runs", bodyRuns)
	}
	types := eventTypes(log)
	if types[len(types)-1] != "turn/end" {
		t.Fatalf("a bounded turn must still close, got %v", types)
	}
}

func TestFragmentSourceRendersPromptAndTools(t *testing.T) {
	source := agentloop.NewFragmentSource(
		func(scope string) (string, error) { return "instructions for " + scope, nil },
		func(scope string) []tools.Definition {
			return []tools.Definition{{
				Name: "read_file", Description: "Read a file.",
				Schema: tools.InputSchema{Type: tools.SchemaTypeObject},
			}}
		},
	)
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log, SystemFragments: source})
	request := spec()
	owner, err := factory.Create(context.Background(), request)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "hi"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	attempts := provider.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("want 1 request, got %d", len(attempts))
	}
	// The system prompt is prepended, so the request carries it plus the user
	// message.
	if len(attempts[0].Messages) != 2 {
		t.Fatalf("want system + user, got %d messages", len(attempts[0].Messages))
	}
	if attempts[0].Messages[0].Role != llm.RoleSystem {
		t.Fatalf("the first message must be the rendered system prompt, got %s", attempts[0].Messages[0].Role)
	}
	if !contains(llm.ContentText(attempts[0].Messages[0].Content), "instructions for main") {
		t.Fatalf("the prompt was not rendered for the scope: %q", llm.ContentText(attempts[0].Messages[0].Content))
	}
	// The header records both the prompt and the offered tool names.
	for _, event := range log.Events() {
		if event.Type == "request/header" {
			if !contains(string(event.Data), `"tools":["read_file"]`) {
				t.Fatalf("the header must list the offered tools: %s", event.Data)
			}
			if !contains(string(event.Data), `"systemPrompt":"instructions for main"`) {
				t.Fatalf("the header must record the rendered prompt: %s", event.Data)
			}
		}
	}
}

func TestFragmentSourceHandlesNilAndFailure(t *testing.T) {
	var nilSource *struct{}
	_ = nilSource
	empty := agentloop.NewFragmentSource(nil, nil)
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log, SystemFragments: empty})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "hi"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(provider.Attempts()[0].Messages) != 1 {
		t.Fatalf("a nil source must add no message, got %d", len(provider.Attempts()[0].Messages))
	}

	boom := errors.New("prompt assembly failed")
	failing := agentloop.NewFragmentSource(func(string) (string, error) { return "", boom }, nil)
	failingFactory := newFactory(t, agentloop.Config{Provider: provider, Log: newLog(t), SystemFragments: failing})
	failingOwner, err := failingFactory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	failingHandle := failingOwner.Handle()
	if err := failingHandle.Send(context.Background(), agent.Input{Text: "hi"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(failingHandle); !errors.Is(err, boom) {
		t.Fatalf("want the assembly failure, got %v", err)
	}
}

func TestIncompleteStreamIsRecordedAsAnIncompleteAttempt(t *testing.T) {
	log := newLog(t)
	// A stream with text but no terminal finish chunk.
	provider := testkit.NewFakeProvider([]llm.StreamChunk{{Type: llm.ChunkTextDelta, Index: 0, Text: "half"}})
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
	if !errors.Is(err, llm.ErrIncompleteStream) {
		t.Fatalf("want ErrIncompleteStream, got %v", err)
	}
	var sawIncomplete bool
	for _, event := range log.Events() {
		if event.Type == "assistant/attempt" && contains(string(event.Data), `"kind":"incomplete"`) {
			sawIncomplete = true
		}
		if event.Type == "assistant/message" {
			t.Fatal("an incomplete attempt must not become an assistant message")
		}
	}
	if !sawIncomplete {
		t.Fatal("the incomplete attempt must be recorded")
	}
}

func TestFailedFinishIsRecordedAndClosesTheTurn(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider([]llm.StreamChunk{
		{Type: llm.ChunkTextDelta, Index: 0, Text: "partial"},
		{Type: llm.ChunkFinish, Reason: &llm.FinishReason{
			Kind:    llm.FinishError,
			Failure: llm.LlmFailure{Message: "upstream 500", Code: "server-error"},
		}},
	})
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
	var failure *llm.FailureError
	if !errors.As(err, &failure) {
		t.Fatalf("want a FailureError, got %v", err)
	}
	if failure.Failure.Code != "server-error" {
		t.Fatalf("unexpected code %q", failure.Failure.Code)
	}
	types := eventTypes(log)
	if types[len(types)-1] != "turn/end" {
		t.Fatalf("a failed turn must close, got %v", types)
	}
	if !contains(string(log.Events()[len(log.Events())-1].Data), `"failed":true`) {
		t.Fatal("the failed turn must be recorded as failed")
	}
}

func TestProviderStartFailureIsRecorded(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProviderWithOptions(
		testkit.FakeProviderID("fake"),
		testkit.FakeProviderFailure(errors.New("dial failed")),
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
	if err := run(handle); err == nil {
		t.Fatal("a provider that cannot start must fail the turn")
	}
	var found bool
	for _, event := range log.Events() {
		if event.Type == "assistant/attempt" && contains(string(event.Data), "dial failed") {
			found = true
		}
	}
	if !found {
		t.Fatal("the failed attempt must be recorded")
	}
}

func TestMissingPipelineProducesANormalizedRefusal(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(
		testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"path":"a.txt"}`}),
		testkit.ScriptTextAttempt([]string{"ok"}),
	)
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "read"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	var sawResult bool
	for _, event := range log.Events() {
		if event.Type == "tool/result" {
			sawResult = true
			if !contains(string(event.Data), `"outcome":"failed"`) {
				t.Fatalf("a missing pipeline must be recorded as a failure: %s", event.Data)
			}
		}
	}
	if !sawResult {
		t.Fatal("a refused call must still produce a recorded result")
	}
}

func TestRunTurnThroughTheRunnerSeam(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"seam"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	runner, ok := agentloop.AsRunner(handle)
	if !ok {
		t.Fatalf("handle must expose a runner, got %T", handle)
	}
	if err := runner.RunTurn(context.Background()); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if got := len(provider.Attempts()); got != 1 {
		t.Fatalf("want 1 request, got %d", got)
	}
}
