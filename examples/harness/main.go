// Command harness runs one complete Agent turn with a fake provider and a fake
// tool, then prints the authoritative event order and rebuilds each model-visible
// request from the log.
//
// It needs no credential and no network: the provider replays a fixed script and
// the clock is fixed, so the output is byte-stable.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// fixedTime keeps every recorded timestamp identical between runs.
var fixedTime = time.UnixMilli(1_700_000_000_000)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run drives one turn and prints what the log contains.
func run(out io.Writer) error {
	log := session.NewLog(session.KnownTypes())
	if err := log.SetClock(func() time.Time { return fixedTime }); err != nil {
		return err
	}

	var bodyRuns int
	registry := tools.NewRegistry()
	if err := registry.Register(tools.Definition{
		Name:        "read_file",
		Description: "Read a file from the workspace.",
		Schema: tools.InputSchema{
			Type:       tools.SchemaTypeObject,
			Properties: map[string]tools.PropertySchema{"path": {Type: tools.SchemaTypeString}},
			Required:   []string{"path"},
		},
		ReadOnly: true,
		Owner:    "example",
		Execute: func(execution tools.Context) (tools.Result, error) {
			bodyRuns++
			return tools.Result{Content: "contents of " + execution.Arguments["path"].(string)}, nil
		},
	}); err != nil {
		return err
	}

	// The scripted provider: first asks for the tool, then answers with text.
	provider := testkit.NewFakeProvider(
		testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"pa`, `th":"a.txt"}`}),
		testkit.ScriptTextAttempt([]string{"a.txt says ", "hello"}),
	)

	factory, err := agentloop.NewFactory(agentloop.Config{
		Provider: provider,
		Log:      log,
		Pipeline: &tools.Pipeline{Registry: registry},
	})
	if err != nil {
		return err
	}
	owner, err := factory.Create(context.Background(), agent.Spec{
		Scope: "main", SessionID: "session-1", Provider: "fake", Model: "fake-1",
	})
	if err != nil {
		return err
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "read a.txt", Origin: "example"}); err != nil {
		return err
	}
	runner, ok := agentloop.AsRunner(handle)
	if !ok {
		return fmt.Errorf("the default driver's handle is not runnable")
	}
	if err := runner.RunTurn(context.Background()); err != nil {
		return err
	}

	fmt.Fprintln(out, "--- event order ---")
	for _, event := range log.Events() {
		fmt.Fprintf(out, "seq=%d %s\n", event.Seq, event.Type)
	}
	fmt.Fprintf(out, "events=%d\n", log.Len())

	fmt.Fprintln(out, "--- attempts seen by the provider ---")
	attempts := provider.Attempts()
	fmt.Fprintf(out, "requests=%d toolBodyRuns=%d\n", len(attempts), bodyRuns)
	for index, request := range attempts {
		fmt.Fprintf(out, "request[%d] provider=%s model=%s messages=%d\n",
			index, request.Provider, request.Model, len(request.Messages))
		for messageIndex, message := range request.Messages {
			fmt.Fprintf(out, "  message[%d] role=%s content=%q blocks=%s\n",
				messageIndex, message.Role, llm.ContentText(message.Content), describeBlocks(message.Content))
		}
	}

	fmt.Fprintln(out, "--- history rebuilt from the log ---")
	history, err := log.DeriveMessages()
	if err != nil {
		return err
	}
	for index, message := range history {
		fmt.Fprintf(out, "history[%d] role=%s content=%q blocks=%s\n",
			index, message.Role, llm.ContentText(message.Content), describeBlocks(message.Content))
	}
	fmt.Fprintf(out, "history=%d\n", len(history))

	// The second request must carry the tool result, which is the observable proof
	// that the request is derived from the log rather than accumulated in memory.
	carriesToolResult := false
	if len(attempts) == 2 {
		for _, message := range attempts[1].Messages {
			if message.Role == llm.RoleTool && message.ToolCallID == "call-1" {
				carriesToolResult = true
			}
		}
	}
	fmt.Fprintf(out, "second request carries the tool result=%v\n", carriesToolResult)

	fmt.Fprintln(out, "--- shutdown ---")
	if err := owner.Dispose(context.Background()); err != nil {
		return err
	}
	fmt.Fprintf(out, "state=%s\n", handle.State())
	return nil
}

// describeBlocks names each content block so a reader can see that an assistant
// message which requested a tool carries a tool-call block even though its
// visible text is empty.
func describeBlocks(blocks []llm.ContentBlock) string {
	if len(blocks) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if call, ok := block.(llm.ToolCallBlock); ok {
			names = append(names, "tool-call:"+call.Name)
			continue
		}
		names = append(names, string(block.BlockType()))
	}
	return strings.Join(names, ",")
}
