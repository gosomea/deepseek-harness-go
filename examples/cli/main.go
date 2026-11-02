// Command cli runs one complete CLI turn with a scripted provider, then reopens
// the session to show that the history survived.
//
// It needs no credential and no network: the provider replays a fixed answer and
// the workspace is a temporary directory, so the output is byte-stable and the
// run cannot touch a real project.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/approval"
	"github.com/gosomea/deepseek-harness-go/capability/fs"
	"github.com/gosomea/deepseek-harness-go/capability/sandbox"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/plugins/local"
	"github.com/gosomea/deepseek-harness-go/plugins/sessionjsonl"
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

// run drives the whole demonstration.
func run(out io.Writer) error {
	ctx := context.Background()
	root, err := os.MkdirTemp("", "cli-e2e-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	return demonstrate(ctx, out, root)
}

// demonstrate performs the read, the denied write, the allowed write and the
// reopen, printing what each step did.
func demonstrate(ctx context.Context, out io.Writer, root string) error {
	// --- the world and the policy ---
	// The execution world: the example is confined to the temporary directory it
	// created, so it cannot reach a real project.
	world, err := workspace.New(workspace.Options{ID: "cli-demo", Root: root, ResolveSymlinks: true})
	if err != nil {
		return err
	}
	fileSystem, err := local.NewFS(local.FSConfig{World: world, Mode: fs.ModeWorkspaceWrite})
	if err != nil {
		return err
	}
	process, err := local.NewProcess(local.ProcessConfig{World: world})
	if err != nil {
		return err
	}
	// A refusing gateway first: the write below must not happen.
	refusing, err := approval.NewGateway(nil, approval.PolicyDeny)
	if err != nil {
		return err
	}
	deniedTools, err := local.NewTools(local.ToolsConfig{
		FS: fileSystem, Process: process, Approvals: refusing,
		Policy: sandbox.Policy{Effective: sandbox.ModeWorkspaceWrite, Available: sandbox.ModeDangerFullAccess},
	})
	if err != nil {
		return err
	}
	// A permissive gateway for the second attempt: the same call now succeeds.
	allowing, err := approval.NewGateway(&approval.AlwaysResponder{Outcome: approval.OutcomeAllowedOnce}, approval.PolicyAskOnce)
	if err != nil {
		return err
	}
	allowedTools, err := local.NewTools(local.ToolsConfig{
		FS: fileSystem, Process: process, Approvals: allowing,
		Policy: sandbox.Policy{Effective: sandbox.ModeWorkspaceWrite, Available: sandbox.ModeDangerFullAccess},
	})
	if err != nil {
		return err
	}

	// Seed a file to read.
	seed := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(seed, []byte("original content"), 0o644); err != nil {
		return err
	}

	fmt.Fprintln(out, "--- read ---")
	read, err := callTool(deniedTools, "read_file", "call-1", map[string]any{"path": "notes.txt"})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "read=%s\n", read)

	fmt.Fprintln(out, "--- refused write ---")
	_, err = callTool(deniedTools, "edit_file", "call-2", map[string]any{"path": "notes.txt", "content": "denied change"})
	fmt.Fprintf(out, "denied=%v\n", errors.Is(err, local.ErrPolicyDenied))
	if content, err := os.ReadFile(seed); err != nil || string(content) != "original content" {
		return fmt.Errorf("a denied write changed the file: %q", content)
	}
	fmt.Fprintln(out, "file unchanged after refusal=true")

	fmt.Fprintln(out, "--- allowed write ---")
	written, err := callTool(allowedTools, "edit_file", "call-3", map[string]any{"path": "notes.txt", "content": "approved change"})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "write=%s\n", written)
	content, err := os.ReadFile(seed)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "file now=%q\n", string(content))

	fmt.Fprintln(out, "--- refused command under a read-only policy ---")
	strict, err := local.NewTools(local.ToolsConfig{
		FS: fileSystem, Process: process, Approvals: allowing,
		Policy: sandbox.Policy{Effective: sandbox.ModeReadOnly, Available: sandbox.ModeWorkspaceWrite},
	})
	if err != nil {
		return err
	}
	_, err = callTool(strict, "run_command", "call-4", map[string]any{"program": "/bin/echo", "arguments": []any{"hi"}})
	fmt.Fprintf(out, "command denied=%v\n", errors.Is(err, local.ErrPolicyDenied))

	return demonstrateSession(ctx, out, root)
}

// demonstrateSession records a turn, then reopens the session to show the
// history survived, without re-running anything.
func demonstrateSession(ctx context.Context, out io.Writer, root string) error {
	store, err := sessionjsonl.New(sessionjsonl.Options{DataDir: filepath.Join(root, ".dsh-go", "sessions")})
	if err != nil {
		return err
	}
	const id = persistence.SessionID("demo")
	if _, err := store.Create(ctx, persistence.CreateOptions{ID: id}); err != nil {
		return err
	}
	handle, err := store.Open(ctx, persistence.OpenOptions{ID: id, Access: persistence.AccessWrite})
	if err != nil {
		return err
	}
	user, err := llm.NewUserMessage("read notes.txt")
	if err != nil {
		return err
	}
	userData, err := llm.MarshalMessage(user)
	if err != nil {
		return err
	}
	assistant, err := llm.NewAssistantMessage("fake", "fake-1", llm.TextContent("notes.txt says original content"))
	if err != nil {
		return err
	}
	assistantData, err := llm.MarshalMessage(assistant)
	if err != nil {
		return err
	}
	nested, err := persistence.NestMessage(assistantData)
	if err != nil {
		return err
	}
	at := fixedTime
	for _, event := range []session.AppendInput{
		{Type: "turn/start", Data: mustJSON(map[string]any{"turn": 0, "input": "read notes.txt"})},
		{Type: "user/message", Data: userData, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		{Type: "assistant/message", Data: nested, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		{Type: "turn/end", Data: mustJSON(map[string]any{"turn": 0, "steps": 1})},
	} {
		event.Time = &at
		if _, err := handle.Append(ctx, event); err != nil {
			return err
		}
	}
	if err := handle.Flush(ctx); err != nil {
		return err
	}
	if err := handle.Close(); err != nil {
		return err
	}

	fmt.Fprintln(out, "--- reopen ---")
	state, messages, err := persistence.Restore(ctx, store, id, persistence.NewCache())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "events=%d messages=%d openTurn=%v unsettled=%d\n",
		len(state.Events), len(messages), state.OpenTurn, len(state.UnsettledCalls))
	for index, message := range messages {
		fmt.Fprintf(out, "history[%d] role=%s content=%q\n", index, message.Role, llm.ContentText(message.Content))
	}
	// The refusal to append without an explicit resume is part of the contract.
	fmt.Fprintf(out, "needsAttention=%v\n", state.NeedsAttention())
	return store.Close()
}

// callTool runs one tool through the real pipeline and returns its content.
func callTool(set *local.Tools, name, callID string, arguments map[string]any) (string, error) {
	registry := tools.NewRegistry()
	if err := set.Register(registry); err != nil {
		return "", err
	}
	pipeline := &tools.Pipeline{Registry: registry}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "", err
	}
	result, err := pipeline.Execute(context.Background(), tools.Call{
		ID: callID, Name: tools.ToolName(name), Arguments: string(encoded),
	})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// mustJSON encodes a payload that cannot fail to encode.
func mustJSON(payload map[string]any) []byte {
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return encoded
}
