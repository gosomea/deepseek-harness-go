package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// ErrInvalidScenario reports a harness scenario fixture that cannot be replayed.
var ErrInvalidScenario = errors.New("testkit: invalid harness scenario")

// HarnessScenario is one deterministic Agent turn described as data.
//
// It exists so the behaviour of a turn — including cancellation and queued input
// — can be written down, replayed, and compared between runs without a test
// function per case. The scenario names the scripts the fake provider replays and
// the actions the driver performs between them, so the interesting part is the
// data rather than the harness around it.
type HarnessScenario struct {
	// ID is the scenario's stable name.
	ID string `json:"id"`
	// Title explains what the scenario demonstrates.
	Title string `json:"title"`
	// Inputs are the user messages delivered to the Agent.
	Inputs []string `json:"inputs"`
	// Scripts are the fake provider's per-attempt chunk scripts. A script is a
	// list of chunk descriptors because a raw StreamChunk carries an interface
	// field that does not survive JSON.
	Scripts [][]ScenarioChunk `json:"scripts"`
	// Tools names the tools the pipeline registers.
	Tools []ScenarioTool `json:"tools"`
	// Actions are what happens during the turn, in order.
	Actions []ScenarioAction `json:"actions"`
}

// ScenarioChunk describes one stream chunk in a fixture.
type ScenarioChunk struct {
	// Type is the chunk discriminator, for example "text-delta".
	Type string `json:"type"`
	// Index is the block index the chunk belongs to.
	Index int `json:"index"`
	// Text is the delta text, for text and reasoning deltas.
	Text string `json:"text,omitempty"`
	// ID is the tool call id, for tool-call deltas.
	ID string `json:"id,omitempty"`
	// Name is the tool name, for the first tool-call delta of a call.
	Name string `json:"name,omitempty"`
	// Arguments is the argument fragment a tool-call delta appends.
	Arguments string `json:"arguments,omitempty"`
	// Finish is the terminal reason, on the final chunk.
	Finish string `json:"finish,omitempty"`
}

// ScenarioTool describes one tool the pipeline registers.
type ScenarioTool struct {
	// Name is the tool name.
	Name string `json:"name"`
	// Content is what the tool returns. It is text so the outcome is observable
	// in the rebuilt history without depending on a real capability.
	Content string `json:"content"`
}

// ScenarioAction is one thing that happens during the turn.
type ScenarioAction struct {
	// Kind is "send", "cancel", or "waitForAttempt".
	Kind string `json:"kind"`
	// Text is the input text for a "send" action.
	Text string `json:"text,omitempty"`
	// Attempt is the attempt number to wait for, for "waitForAttempt".
	Attempt int `json:"attempt,omitempty"`
}

// LoadHarnessScenario reads one fixture from disk.
func LoadHarnessScenario(path string) (HarnessScenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return HarnessScenario{}, err
	}
	var scenario HarnessScenario
	if err := json.Unmarshal(data, &scenario); err != nil {
		return HarnessScenario{}, fmt.Errorf("%w: %s: %v", ErrInvalidScenario, path, err)
	}
	if err := scenario.Validate(); err != nil {
		return HarnessScenario{}, fmt.Errorf("%w: %s: %v", ErrInvalidScenario, path, err)
	}
	return scenario, nil
}

// Validate reports whether the scenario can be replayed.
func (s HarnessScenario) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: scenario requires an id", ErrInvalidScenario)
	}
	if len(s.Inputs) == 0 {
		return fmt.Errorf("%w: scenario %q requires at least one input", ErrInvalidScenario, s.ID)
	}
	for scriptIndex, script := range s.Scripts {
		if len(script) == 0 {
			return fmt.Errorf("%w: scenario %q script %d is empty", ErrInvalidScenario, s.ID, scriptIndex)
		}
		hasFinish := false
		for _, chunk := range script {
			if chunk.Type == "finish" {
				hasFinish = true
			}
		}
		if !hasFinish {
			return fmt.Errorf("%w: scenario %q script %d has no finish chunk", ErrInvalidScenario, s.ID, scriptIndex)
		}
	}
	for actionIndex, action := range s.Actions {
		switch action.Kind {
		case "send", "cancel", "waitForAttempt":
		default:
			return fmt.Errorf("%w: scenario %q action %d has unknown kind %q", ErrInvalidScenario, s.ID, actionIndex, action.Kind)
		}
	}
	return nil
}

// script converts the fixture chunks into real stream chunks.
func (s HarnessScenario) script(index int) []llm.StreamChunk {
	chunks := make([]llm.StreamChunk, 0, len(s.Scripts[index]))
	for _, descriptor := range s.Scripts[index] {
		chunk := llm.StreamChunk{Type: llm.ChunkType(descriptor.Type), Index: descriptor.Index, Text: descriptor.Text, ArgumentsDelta: descriptor.Arguments}
		if descriptor.ID != "" {
			chunk.ID = llm.ToolCallID(descriptor.ID)
		}
		if descriptor.Name != "" {
			name := descriptor.Name
			chunk.Name = &name
		}
		if descriptor.Finish != "" {
			chunk.Reason = &llm.FinishReason{Kind: llm.FinishKind(descriptor.Finish)}
		}
		// A block-end carries the assembled block, and a chunk without one is
		// refused by the vocabulary. The fixture therefore names its block by the
		// same fields a delta would use, so the scenario stays declarative instead
		// of embedding a Go interface value.
		if chunk.Type == llm.ChunkBlockEnd {
			chunk.Block = llm.ToolCallBlock{
				ID:        llm.ToolCallID(descriptor.ID),
				Name:      descriptor.Name,
				Arguments: descriptor.Arguments,
			}
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// HarnessTrace is the normalized result of replaying one scenario.
//
// It is a line-oriented trace so two runs can be compared by text, which is what
// makes "the same scenario produces the same trace" checkable rather than
// asserted in prose.
type HarnessTrace struct {
	// Lines are the trace lines, in order.
	Lines []string
}

// String renders the trace as newline-separated lines.
func (t HarnessTrace) String() string { return strings.Join(t.Lines, "\n") }

// ReplayHarnessScenario runs one scenario and returns its trace.
//
// The whole turn is driven synchronously except for the actions that must occur
// while a turn is in flight ("cancel" and "send" during a blocking attempt),
// which is why the fixture names them explicitly instead of relying on timing.
func ReplayHarnessScenario(scenario HarnessScenario) (HarnessTrace, error) {
	if err := scenario.Validate(); err != nil {
		return HarnessTrace{}, err
	}
	log := session.NewLog(session.KnownTypes())
	fixed := time.UnixMilli(1_700_000_000_000)
	// SetClock only fails for a nil function, and this is a literal; a branch
	// that cannot run would be untestable coverage rather than a real path.
	_ = log.SetClock(func() time.Time { return fixed })

	toolRegistry := tools.NewRegistry()
	for _, definition := range scenario.Tools {
		content := definition.Content
		if err := toolRegistry.Register(tools.Definition{
			Name: tools.ToolName(definition.Name), Description: "Scenario tool " + definition.Name + ".",
			Schema: tools.InputSchema{
				Type:       tools.SchemaTypeObject,
				Properties: map[string]tools.PropertySchema{"path": {Type: tools.SchemaTypeString}},
				Required:   []string{"path"},
			},
			ReadOnly: true,
			Execute: func(tools.Context) (tools.Result, error) {
				return tools.Result{Content: content}, nil
			},
		}); err != nil {
			return HarnessTrace{}, err
		}
	}

	scripts := make([][]llm.StreamChunk, 0, len(scenario.Scripts))
	for index := range scenario.Scripts {
		scripts = append(scripts, scenario.script(index))
	}
	provider := NewFakeProvider(scripts...)
	factory, err := agentloop.NewFactory(agentloop.Config{
		Provider: provider,
		Log:      log,
		Pipeline: &tools.Pipeline{Registry: toolRegistry},
	})
	if err != nil {
		return HarnessTrace{}, err
	}
	owner, err := factory.Create(context.Background(), agent.Spec{
		Scope: "scenario", SessionID: "scenario-1", Provider: "fake", Model: "fake-1",
	})
	if err != nil {
		return HarnessTrace{}, err
	}
	handle := owner.Handle()
	for _, input := range scenario.Inputs {
		if err := handle.Send(context.Background(), agent.Input{Text: input, Origin: "scenario"}); err != nil {
			return HarnessTrace{}, err
		}
	}

	runner, ok := agentloop.AsRunner(handle)
	if !ok {
		return HarnessTrace{}, fmt.Errorf("%w: scenario handle is not runnable", ErrInvalidScenario)
	}
	var runErr error
	if len(scenario.Actions) == 0 {
		runErr = runner.RunTurn(context.Background())
	} else {
		done := make(chan error, 1)
		go func() { done <- runner.RunTurn(context.Background()) }()
		actionErr, turnErr := applyActions(scenario, provider, handle, done)
		// A failed action is a defect in the scenario or in delivery, not a
		// result of the turn: it is returned so the caller sees it, while the
		// turn's own error is recorded in the trace.
		if actionErr != nil {
			return HarnessTrace{}, actionErr
		}
		runErr = turnErr
	}

	trace := HarnessTrace{}
	for _, event := range log.Events() {
		trace.Lines = append(trace.Lines, fmt.Sprintf("seq=%d %s", event.Seq, event.Type))
	}
	trace.Lines = append(trace.Lines, fmt.Sprintf("events=%d", log.Len()))
	trace.Lines = append(trace.Lines, fmt.Sprintf("requests=%d", len(provider.Attempts())))
	history, err := log.DeriveMessages()
	if err != nil {
		return HarnessTrace{}, err
	}
	for index, message := range history {
		trace.Lines = append(trace.Lines, fmt.Sprintf("history[%d] role=%s content=%q", index, message.Role, llm.ContentText(message.Content)))
	}
	trace.Lines = append(trace.Lines, fmt.Sprintf("history=%d", len(history)))
	trace.Lines = append(trace.Lines, fmt.Sprintf("failed=%v", runErr != nil))
	if runErr != nil {
		trace.Lines = append(trace.Lines, "failure="+runErr.Error())
	}
	return trace, nil
}

// applyActions performs the scenario's in-flight actions and reports the turn's
// own outcome separately.
//
// The two errors are distinct on purpose: an action that cannot be delivered is a
// defect the caller must see, while a turn that fails is a result the trace
// records. Conflating them would hide a broken action behind a trace line.
func applyActions(scenario HarnessScenario, provider *FakeProvider, handle agent.Handle, done <-chan error) (error, error) {
	canceler, _ := handle.(agent.Canceler)
	turnFinished := make(chan struct{})
	var turnErr error
	go func() {
		defer close(turnFinished)
		turnErr = <-done
	}()
	for _, action := range scenario.Actions {
		switch action.Kind {
		case "waitForAttempt":
			if err := waitForAttempt(provider, action.Attempt, turnFinished); err != nil {
				return err, nil
			}
		case "send":
			if err := handle.Send(context.Background(), agent.Input{Text: action.Text, Origin: "scenario"}); err != nil {
				return err, nil
			}
		case "cancel":
			if canceler == nil {
				return fmt.Errorf("%w: scenario cancels but the handle is not a Canceler", ErrInvalidScenario), nil
			}
			if _, err := canceler.Cancel(context.Background()); err != nil {
				return err, nil
			}
		}
	}
	<-turnFinished
	return nil, turnErr
}

// waitForAttempt blocks until the fake provider has received the given attempt,
// or until the turn ends.
//
// A turn that ends first is not an error when it ended by having made at least
// one attempt: a script can finish so quickly that the action's observation point
// passes while the turn is already settling, and the action's purpose is "happen
// after this attempt", not "happen while the attempt is in flight". It is an
// error only when the requested attempt never happened at all, because then the
// scenario describes something that cannot occur.
func waitForAttempt(provider *FakeProvider, attempt int, turnFinished <-chan struct{}) error {
	if attempt <= 0 {
		attempt = 1
	}
	for {
		if len(provider.Attempts()) >= attempt {
			return nil
		}
		select {
		case <-turnFinished:
			if len(provider.Attempts()) >= attempt {
				return nil
			}
			return fmt.Errorf("%w: turn ended before attempt %d", ErrInvalidScenario, attempt)
		case <-time.After(time.Millisecond):
		}
	}
}

// HarnessScenarioDir returns the fixture directory for a repository root.
func HarnessScenarioDir(root string) string {
	return filepath.Join(root, "testdata", "parity", "agentloop", "scenarios")
}
