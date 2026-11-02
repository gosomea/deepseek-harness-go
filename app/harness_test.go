package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/app"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// fixedTime keeps every recorded timestamp identical between runs.
var fixedTime = time.UnixMilli(1_700_000_000_000)

// newLog returns a deterministic session log.
func newLog(t *testing.T) *session.Log {
	t.Helper()
	log := session.NewLog(session.KnownTypes())
	if err := log.SetClock(func() time.Time { return fixedTime }); err != nil {
		t.Fatalf("set clock: %v", err)
	}
	return log
}

// readFilePipeline registers one deterministic tool.
func readFilePipeline(t *testing.T) *tools.Pipeline {
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
			return tools.Result{Content: "contents of " + execution.Arguments["path"].(string)}, nil
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return &tools.Pipeline{Registry: registry}
}

// A Harness assembled from a fake provider runs a full turn: two requests, one
// tool call, and a log that rebuilds every model-visible input.
func TestHarnessRunsAFullTurn(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(
		testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"path":"a.txt"}`}),
		testkit.ScriptTextAttempt([]string{"done"}),
	)
	harness, err := app.NewHarnessWithLog(app.HarnessConfig{
		Provider: provider,
		Pipeline: readFilePipeline(t),
	}, log, "main")
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}
	owner, err := harness.Start(context.Background(), agent.Spec{SessionID: "s-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "read a.txt"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := harness.RunTurn(context.Background(), handle); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if got := len(provider.Attempts()); got != 2 {
		t.Fatalf("want 2 model requests, got %d", got)
	}
	history, err := log.DeriveMessages()
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	// A completed turn leaves four model-visible messages: the user input, the
	// assistant message that requested the tool, the tool result, and the final
	// assistant answer.
	if len(history) != 4 {
		t.Fatalf("want 4 derived messages, got %d", len(history))
	}
	if history[1].Role != llm.RoleAssistant || history[2].Role != llm.RoleTool || history[3].Role != llm.RoleAssistant {
		t.Fatalf("unexpected roles: %s %s %s %s", history[0].Role, history[1].Role, history[2].Role, history[3].Role)
	}
	// Closing the harness releases the Agent it started.
	if err := harness.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if handle.State() != agent.StateDisposed {
		t.Fatalf("closing the harness must dispose its Agents, got %s", handle.State())
	}
}

// An incomplete configuration is refused before anything is created.
func TestHarnessRefusesIncompleteConfiguration(t *testing.T) {
	if _, err := app.NewHarness(app.HarnessConfig{}); !errors.Is(err, app.ErrHarnessConfig) {
		t.Fatalf("want ErrHarnessConfig without a provider, got %v", err)
	}
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"x"}))
	if _, err := app.NewHarnessWithLog(app.HarnessConfig{Provider: provider}, nil, "main"); !errors.Is(err, app.ErrHarnessConfig) {
		t.Fatalf("want ErrHarnessConfig without a log, got %v", err)
	}
	if _, err := app.NewHarnessWithLog(app.HarnessConfig{Provider: provider}, newLog(t), ""); !errors.Is(err, app.ErrHarnessConfig) {
		t.Fatalf("want ErrHarnessConfig without a scope, got %v", err)
	}
}

// A Harness fills in the scope and session identity so a caller does not have to
// repeat them, and it refuses a handle from a different driver.
func TestHarnessFillsIdentityAndValidatesTheHandle(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	harness, err := app.NewHarnessWithLog(app.HarnessConfig{Provider: provider}, log, "scoped")
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}
	owner, err := harness.Start(context.Background(), agent.Spec{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := owner.Handle().Info().Scope; got != agent.ScopeKey("scoped") {
		t.Fatalf("the harness must fill the scope, got %q", got)
	}
	if err := harness.RunTurn(context.Background(), foreignHandle{}); !errors.Is(err, app.ErrHarnessConfig) {
		t.Fatalf("want ErrHarnessConfig for a foreign handle, got %v", err)
	}
	// A nil Harness is refused rather than panicking.
	var absent *app.Harness
	if _, err := absent.Start(context.Background(), agent.Spec{}); !errors.Is(err, app.ErrHarnessConfig) {
		t.Fatalf("want ErrHarnessConfig from a nil harness, got %v", err)
	}
	if err := absent.Close(context.Background()); err != nil {
		t.Fatalf("closing a nil harness must be a no-op, got %v", err)
	}
}

type foreignHandle struct{}

func (foreignHandle) Info() agent.Info                        { return agent.Info{ID: "x", Scope: "y"} }
func (foreignHandle) State() agent.State                      { return agent.StateIdle }
func (foreignHandle) Send(context.Context, agent.Input) error { return nil }
func (foreignHandle) Wait(context.Context) error              { return nil }
func (foreignHandle) Done() <-chan struct{}                   { return nil }
