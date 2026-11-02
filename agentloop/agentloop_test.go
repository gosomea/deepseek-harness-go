package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// fixedTime is the timestamp every deterministic test records, so the log is
// byte-stable across runs.
var fixedTime = time.UnixMilli(1_700_000_000_000)

// newLog returns a session log with a fixed clock.
func newLog(t *testing.T) *session.Log {
	t.Helper()
	log := session.NewLog(session.KnownTypes())
	if err := log.SetClock(func() time.Time { return fixedTime }); err != nil {
		t.Fatalf("set clock: %v", err)
	}
	return log
}

// readFileTool registers a deterministic read_file tool and returns the pipeline
// plus a counter of how many times the body actually ran.
func readFileTool(t *testing.T, counter *int) *tools.Pipeline {
	t.Helper()
	registry := tools.NewRegistry()
	err := registry.Register(tools.Definition{
		Name:        "read_file",
		Description: "Read a file.",
		Schema: tools.InputSchema{
			Type:       tools.SchemaTypeObject,
			Properties: map[string]tools.PropertySchema{"path": {Type: tools.SchemaTypeString}},
			Required:   []string{"path"},
		},
		ReadOnly: true,
		Execute: func(execution tools.Context) (tools.Result, error) {
			*counter++
			return tools.Result{Content: "contents of " + execution.Arguments["path"].(string)}, nil
		},
	})
	if err != nil {
		t.Fatalf("register tool: %v", err)
	}
	return &tools.Pipeline{Registry: registry}
}

func spec() agent.Spec {
	return agent.Spec{Scope: "main", SessionID: "session-1", Provider: "fake", Model: "fake-1"}
}

func newFactory(t *testing.T, config agentloop.Config) *agentloop.Factory {
	t.Helper()
	if config.Log == nil {
		config.Log = newLog(t)
	}
	factory, err := agentloop.NewFactory(config)
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	return factory
}

// eventTypes returns the ordered event type list, which is the shape several
// assertions compare.
func eventTypes(log *session.Log) []string {
	types := []string{}
	for _, event := range log.Events() {
		types = append(types, event.Type)
	}
	return types
}

// The checkpoint behaviour: one input, two model requests, one tool call, and a
// log that lets a reader rebuild every model-visible input.
func TestTurnRunsTwoModelRequestsAndOneToolCall(t *testing.T) {
	log := newLog(t)
	var bodyRuns int
	provider := testkit.NewFakeProvider(
		testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"pa`, `th":"a.txt"}`}),
		testkit.ScriptTextAttempt([]string{"the file says ", "hello"}),
	)
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log, Pipeline: readFileTool(t, &bodyRuns)})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "read a.txt", Origin: "test"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Two requests: the first asked for a tool, the second answered with text.
	if got := len(provider.Attempts()); got != 2 {
		t.Fatalf("want 2 model requests, got %d", got)
	}
	if bodyRuns != 1 {
		t.Fatalf("want the tool body to run once, ran %d", bodyRuns)
	}

	types := eventTypes(log)
	want := []string{
		"turn/start",
		"user/message",
		"step/start",
		"request/header",
		"assistant/attempt",
		"assistant/message",
		"tool/call",
		"tool/result",
		"step/end",
		"step/start",
		"request/header",
		"assistant/attempt",
		"assistant/message",
		"step/end",
		"turn/end",
	}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected event order:\n got %v\nwant %v", types, want)
	}

	// The tool call is recorded before its result, which is what lets a reader
	// tell "requested" from "settled".
	callSeq, resultSeq := -1, -1
	for index, eventType := range types {
		if eventType == "tool/call" {
			callSeq = index
		}
		if eventType == "tool/result" && callSeq >= 0 {
			resultSeq = index
		}
	}
	if callSeq < 0 || resultSeq < 0 || callSeq > resultSeq {
		t.Fatalf("tool/call must precede tool/result, got %d then %d", callSeq, resultSeq)
	}
}

// The second request must carry the tool result, which is the observable proof
// that history is derived from the log rather than accumulated in memory.
func TestSecondRequestIsRebuiltFromTheLog(t *testing.T) {
	log := newLog(t)
	var bodyRuns int
	provider := testkit.NewFakeProvider(
		testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"path":"a.txt"}`}),
		testkit.ScriptTextAttempt([]string{"done"}),
	)
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log, Pipeline: readFileTool(t, &bodyRuns)})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := owner.Handle().Send(context.Background(), agent.Input{Text: "read a.txt"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(owner.Handle()); err != nil {
		t.Fatalf("run: %v", err)
	}
	attempts := provider.Attempts()
	if len(attempts) != 2 {
		t.Fatalf("want 2 requests, got %d", len(attempts))
	}
	// The second request must contain the tool message produced by the first.
	var sawToolMessage bool
	for _, message := range attempts[1].Messages {
		if message.Role == llm.RoleTool {
			sawToolMessage = true
			if message.ToolCallID != "call-1" {
				t.Fatalf("tool message must answer call-1, got %q", message.ToolCallID)
			}
			if !strings.Contains(llm.ContentText(message.Content), "contents of a.txt") {
				t.Fatalf("tool message lost its content: %q", llm.ContentText(message.Content))
			}
		}
	}
	if !sawToolMessage {
		t.Fatal("the second request must carry the tool result derived from the log")
	}
	// The request was built from the log as it stood before that request, so its
	// messages must be a prefix of the history the finished log derives. Equality
	// would be wrong: the final assistant reply is recorded after the request was
	// sent, so the current history is legitimately longer.
	derived, err := log.DeriveMessages()
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if len(derived) < len(attempts[1].Messages) {
		t.Fatalf("derived history (%d) is shorter than the request (%d)", len(derived), len(attempts[1].Messages))
	}
	for index, message := range attempts[1].Messages {
		if llm.ContentText(message.Content) != llm.ContentText(derived[index].Content) {
			t.Fatalf("message %d differs: request %q, derived %q",
				index, llm.ContentText(message.Content), llm.ContentText(derived[index].Content))
		}
		if message.Role != derived[index].Role {
			t.Fatalf("message %d role differs: request %s, derived %s", index, message.Role, derived[index].Role)
		}
	}
}

// The request recorded in the log must describe what was actually sent, so a
// reader does not have to re-derive the provider and model.
func TestRequestHeaderRecordsWhatWasSent(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"hi"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := owner.Handle().Send(context.Background(), agent.Input{Text: "hello"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(owner.Handle()); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, event := range log.Events() {
		if event.Type != "request/header" {
			continue
		}
		text := string(event.Data)
		for _, want := range []string{`"provider":"fake"`, `"model":"fake-1"`, `"step":0`, `"messages":1`} {
			if !strings.Contains(text, want) {
				t.Fatalf("request/header missing %s: %s", want, text)
			}
		}
		return
	}
	t.Fatal("no request/header event was recorded")
}

// run drives one turn to settlement. The driver exposes it through the factory's
// instance so a test does not have to own a scheduler.
func run(handle agent.Handle) error {
	runner, ok := handle.(interface{ RunForTest(context.Context) error })
	if !ok {
		return errors.New("handle is not runnable")
	}
	return runner.RunForTest(context.Background())
}
