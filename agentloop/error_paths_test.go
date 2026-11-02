package agentloop_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// A log that refuses an event must fail the turn rather than continue with a
// history the reader cannot reconstruct.
func TestTurnFailsWhenTheLogRefusesAnEvent(t *testing.T) {
	// A registry that knows only some event types: the turn/start write succeeds
	// but the user message does not, so the driver must abort instead of running
	// a turn whose history is incomplete.
	registry := session.NewRegistry("turn/start")
	log := session.NewLog(registry)
	if err := log.SetClock(func() time.Time { return fixedTime }); err != nil {
		t.Fatalf("set clock: %v", err)
	}
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"never sent"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	err = run(handle)
	if !errors.Is(err, agentloop.ErrEventRejected) {
		t.Fatalf("want ErrEventRejected, got %v", err)
	}
	if got := len(provider.Attempts()); got != 0 {
		t.Fatalf("no request may be sent once recording failed, got %d", got)
	}
}

// A request that would carry no message is a configuration error, not a panic.
func TestTurnRefusesARequestWithNoMessages(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"unused"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	// A pipeline without a registry refuses every call, which exercises the
	// normalized-refusal path with a non-nil pipeline.
	factoryWithEmptyPipeline := newFactory(t, agentloop.Config{
		Provider: testkit.NewFakeProvider(
			testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"path":"a.txt"}`}),
			testkit.ScriptTextAttempt([]string{"done"}),
		),
		Log:      newLog(t),
		Pipeline: &tools.Pipeline{},
	})
	emptyOwner, err := factoryWithEmptyPipeline.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	emptyHandle := emptyOwner.Handle()
	if err := emptyHandle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(emptyHandle); err != nil {
		t.Fatalf("a refused tool call must not fail the turn: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// A pipeline whose registry lacks the requested tool records a normalized
// refusal and the turn still finishes, so the model can react.
func TestUnknownToolIsRecordedAsANormalizedResult(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(
		testkit.ScriptToolCallAttempt("call-1", "absent_tool", []string{`{}`}),
		testkit.ScriptTextAttempt([]string{"understood"}),
	)
	registry := tools.NewRegistry()
	if err := registry.Register(tools.Definition{
		Name: "read_file", Description: "Read a file.",
		Schema:  tools.InputSchema{Type: tools.SchemaTypeObject},
		Execute: func(tools.Context) (tools.Result, error) { return tools.Result{Content: "x"}, nil },
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log, Pipeline: &tools.Pipeline{Registry: registry}})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "call a missing tool"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("an unknown tool must not fail the turn: %v", err)
	}
	var found bool
	for _, event := range log.Events() {
		if event.Type == "tool/result" {
			found = true
			if !contains(string(event.Data), `"outcome":"failed"`) {
				t.Fatalf("an unknown tool must be recorded as failed: %s", event.Data)
			}
		}
	}
	if !found {
		t.Fatal("the refusal must be recorded as a result")
	}
}

// A tool that returns a failed outcome without an error is recorded as an error
// result, so the model is told rather than left guessing.
func TestToolFailureOutcomeIsRecorded(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(
		testkit.ScriptToolCallAttempt("call-1", "flaky", []string{`{}`}),
		testkit.ScriptTextAttempt([]string{"recovered"}),
	)
	registry := tools.NewRegistry()
	if err := registry.Register(tools.Definition{
		Name: "flaky", Description: "Always fails.",
		Schema: tools.InputSchema{Type: tools.SchemaTypeObject},
		Execute: func(tools.Context) (tools.Result, error) {
			return tools.Result{}, errors.New("tool exploded")
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log, Pipeline: &tools.Pipeline{Registry: registry}})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("a failing tool must not fail the turn: %v", err)
	}
	var sawFailure bool
	for _, event := range log.Events() {
		if event.Type == "tool/result" && contains(string(event.Data), `"isError":true`) {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Fatalf("the failed result must be recorded as an error: %v", eventTypes(log))
	}
	// The next request still happened, so the model could react to the failure.
	if got := len(provider.Attempts()); got != 2 {
		t.Fatalf("want a second request after the failure, got %d", got)
	}
}

// Creating an Agent whose spec is invalid is refused before anything is built.
func TestCreateRefusesAnInvalidSpec(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"x"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	if _, err := factory.Create(context.Background(), agent.Spec{}); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an empty spec, got %v", err)
	}
	if _, err := factory.Create(context.Background(), agent.Spec{Scope: "main"}); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a missing session id, got %v", err)
	}
	badInput := agent.Input{}
	badSpec := spec()
	badSpec.InitialInput = &badInput
	if _, err := factory.Create(context.Background(), badSpec); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank initial input, got %v", err)
	}
	// Nothing was published by any of the failures.
	if got := len(factory.Active()); got != 0 {
		t.Fatalf("failed creations must publish nothing, got %d", got)
	}
}

// Creating after the factory closed is refused rather than half-succeeding.
func TestCreateAfterDisposeAllIsRefused(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"x"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	if err := factory.DisposeAll(context.Background()); err != nil {
		t.Fatalf("dispose all: %v", err)
	}
	if _, err := factory.Create(context.Background(), spec()); !errors.Is(err, agent.ErrClosed) {
		t.Fatalf("want ErrClosed after DisposeAll, got %v", err)
	}
	if !factory.Closed() {
		t.Fatal("DisposeAll must close the factory")
	}
	if err := factory.DisposeAll(context.Background()); err != nil {
		t.Fatalf("second dispose all: %v", err)
	}
}

// Multiple tool calls in one step are each recorded and each answered.
func TestMultipleToolCallsInOneStepAreEachAnswered(t *testing.T) {
	log := newLog(t)
	firstName, secondName := "read_file", "write_file"
	// Each call is closed by block-end, which the assembler requires before an
	// attempt may become a message: an open call is treated as a truncated one.
	chunks := []llm.StreamChunk{
		{Type: llm.ChunkToolCallDelta, Index: 0, ID: "call-a", Name: &firstName, ArgumentsDelta: `{"path":"a.txt"}`},
		{Type: llm.ChunkToolCallDelta, Index: 1, ID: "call-b", Name: &secondName, ArgumentsDelta: `{"path":"b.txt"}`},
		{Type: llm.ChunkBlockEnd, Index: 0, Block: llm.ToolCallBlock{ID: "call-a", Name: firstName, Arguments: `{"path":"a.txt"}`}},
		{Type: llm.ChunkBlockEnd, Index: 1, Block: llm.ToolCallBlock{ID: "call-b", Name: secondName, Arguments: `{"path":"b.txt"}`}},
		{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishToolCalls}},
	}
	provider := testkit.NewFakeProvider(chunks, testkit.ScriptTextAttempt([]string{"both done"}))
	registry := tools.NewRegistry()
	for _, name := range []string{"read_file", "write_file"} {
		if err := registry.Register(tools.Definition{
			Name: tools.ToolName(name), Description: "A file tool.",
			Schema: tools.InputSchema{
				Type:       tools.SchemaTypeObject,
				Properties: map[string]tools.PropertySchema{"path": {Type: tools.SchemaTypeString}},
				Required:   []string{"path"},
			},
			Execute: func(execution tools.Context) (tools.Result, error) {
				return tools.Result{Content: "did " + execution.Name.String()}, nil
			},
		}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log, Pipeline: &tools.Pipeline{Registry: registry}})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "do both"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	calls, results := 0, 0
	for _, event := range log.Events() {
		switch event.Type {
		case "tool/call":
			calls++
		case "tool/result":
			results++
		}
	}
	if calls != 2 || results != 2 {
		t.Fatalf("want 2 calls and 2 results, got %d and %d", calls, results)
	}
	// The second request must carry both tool results.
	second := provider.Attempts()[1]
	toolMessages := 0
	for _, message := range second.Messages {
		if message.Role == llm.RoleTool {
			toolMessages++
		}
	}
	if toolMessages != 2 {
		t.Fatalf("the second request must carry both results, got %d", toolMessages)
	}
}
