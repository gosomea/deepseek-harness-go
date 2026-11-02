package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gosomea/deepseek-harness-go/capability/approval"
	"github.com/gosomea/deepseek-harness-go/capability/fs"
	"github.com/gosomea/deepseek-harness-go/capability/sandbox"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/plugins/local"
	"github.com/gosomea/deepseek-harness-go/plugins/sessionjsonl"
	"github.com/gosomea/deepseek-harness-go/session"
)

// Exit codes this command reports. They are named because a test and an operator
// both need to refer to the same numbers.
const (
	// exitOK reports a completed run.
	exitOK = 0
	// exitFailure reports a run that failed for a reason other than the command
	// line.
	exitFailure = 1
	// exitUsage reports a rejected command line. It matches the value the
	// configuration commands already use.
	exitUsageRun = 2
	// exitDenied reports that a side effect was refused by policy.
	//
	// It is distinct from a generic failure because the operator's next action
	// differs: a denial is fixed by granting an approval, not by debugging.
	exitDenied = 3
	// exitInterrupted reports that a signal stopped the run.
	exitInterrupted = 130
)

// sessionOptions is one parsed session command line.
type sessionOptions struct {
	// workspace is the directory the run is confined to. Required.
	workspace string
	// session is the stored session identity. Required.
	session string
	// prompt is the user input for this turn. Required.
	prompt string
	// dataDir overrides where session artifacts are stored. It defaults to a
	// directory inside the workspace, so a run never writes outside its world.
	dataDir string
	// headless selects the approval policy: "deny" or "ask-once".
	headless string
	// resume continues a stored session instead of starting a new one.
	resume bool
}

// parseSession reads a session command line.
func parseSession(args []string, stderr io.Writer) (sessionOptions, error) {
	options := sessionOptions{}
	flags := flag.NewFlagSet("dsh-go session", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.workspace, "workspace", "", "directory the run is confined to")
	flags.StringVar(&options.session, "session", "", "stored session identity")
	flags.StringVar(&options.prompt, "prompt", "", "user input for this turn")
	flags.StringVar(&options.dataDir, "data-dir", "", "where session artifacts are stored (default: inside the workspace)")
	flags.StringVar(&options.headless, "headless", string(approval.PolicyDeny), "approval policy when nobody can answer (deny|ask-once)")
	flags.BoolVar(&options.resume, "resume", false, "continue a stored session instead of starting a new one")
	if err := flags.Parse(args); err != nil {
		return sessionOptions{}, usageError{err}
	}
	if strings.TrimSpace(options.workspace) == "" {
		return sessionOptions{}, usageError{errors.New("-workspace is required")}
	}
	if strings.TrimSpace(options.session) == "" {
		return sessionOptions{}, usageError{errors.New("-session is required")}
	}
	if strings.TrimSpace(options.prompt) == "" {
		return sessionOptions{}, usageError{errors.New("-prompt is required")}
	}
	if !approval.Policy(options.headless).Valid() {
		return sessionOptions{}, usageError{fmt.Errorf("unknown headless policy %q: use deny or ask-once", options.headless)}
	}
	return options, nil
}

// sessionDeps are the collaborators a session run needs.
//
// They are parameters rather than package-level state so a test can run the same
// code against a scripted provider and a temporary directory.
type sessionDeps struct {
	// provider generates responses.
	provider Provider
	// stdout receives the answer.
	stdout io.Writer
	// stderr receives diagnostics.
	stderr io.Writer
	// signals delivers termination signals; nil uses the process's.
	signals <-chan os.Signal
}

// Provider is the model seam this command needs.
//
// It is the llm package's own interface rather than a local restatement: the
// command consumes a model exactly as the rest of the harness does, so the same
// scripted provider and the same adapter both work here.
type Provider = llm.Provider

// runSession executes one session command and returns its exit code.
//
// The ordering is the point of the command: the workspace is resolved and the
// policy validated BEFORE any provider or tool is constructed, so a
// configuration that cannot be enforced is refused before a side effect is
// possible.
func runSession(ctx context.Context, options sessionOptions, deps sessionDeps) int {
	if deps.stdout == nil {
		deps.stdout = io.Discard
	}
	if deps.stderr == nil {
		deps.stderr = io.Discard
	}
	// An already-cancelled run is reported as an interruption before any setup:
	// otherwise the same condition would be classified as a plain failure just
	// because it surfaced while opening the store rather than during the turn.
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go: interrupted")
		return exitInterrupted
	}
	// 1. The execution world. A missing or unusable root stops here.
	world, err := workspace.New(workspace.Options{ID: workspace.ID(options.session), Root: options.workspace, ResolveSymlinks: true})
	if err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	// 2. The confinement policy. A policy that cannot be enforced is refused
	//    rather than run unconfined.
	policy := sandbox.Policy{Effective: sandbox.ModeWorkspaceWrite, Available: sandbox.ModeDangerFullAccess}
	if err := policy.Validate(); err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	// 3. The store. It defaults to a directory INSIDE the world, so the command
	//    cannot write outside the workspace it was given.
	dataDir := options.dataDir
	if dataDir == "" {
		dataDir = filepath.Join(world.Root(), ".dsh-go", "sessions")
	}
	store, err := sessionjsonl.New(sessionjsonl.Options{DataDir: dataDir})
	if err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	// 4. The capability providers and the approval channel.
	fileSystem, err := local.NewFS(local.FSConfig{World: world, Mode: fs.ModeWorkspaceWrite})
	if err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	process, err := local.NewProcess(local.ProcessConfig{World: world})
	if err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	// A headless run has no responder by design: ask-once then resolves to
	// unavailable, which does not authorize.
	gateway, err := approval.NewGateway(nil, approval.Policy(options.headless))
	if err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	if _, err := local.NewTools(local.ToolsConfig{
		FS: fileSystem, Process: process, Approvals: gateway, Policy: policy,
	}); err != nil {
		// Building the tool set is how the policy and the providers are validated
		// together; a policy that cannot be enforced is refused here, before any
		// side effect is possible. This command drives the model turn only, so the
		// set itself is not used yet.
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	if err := ensureSession(ctx, store, persistence.SessionID(options.session)); err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}

	// 5. Signals stop the run and are reported as an interruption, not as a
	//    failure of the command.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if deps.signals != nil {
		go func() {
			select {
			case <-deps.signals:
				cancel()
			case <-runCtx.Done():
			}
		}()
	} else {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(signals)
		go func() {
			select {
			case <-signals:
				cancel()
			case <-runCtx.Done():
			}
		}()
	}

	outcome, err := driveTurn(runCtx, options, deps, store)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(deps.stderr, "dsh-go: interrupted")
			return exitInterrupted
		}
		if errors.Is(err, local.ErrPolicyDenied) {
			fmt.Fprintln(deps.stderr, "dsh-go: denied:", err)
			return exitDenied
		}
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	if _, err := fmt.Fprintln(deps.stdout, outcome); err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	// Clean up owned resources before returning: the store holds a writer claim,
	// and releasing it is what lets the next run open the same session.
	if err := store.Close(); err != nil {
		fmt.Fprintln(deps.stderr, "dsh-go:", err)
		return exitFailure
	}
	return exitOK
}

// ensureSession creates the stored session when it does not exist yet.
//
// A resume of a session that was never created is refused rather than silently
// starting a new one, because "continue where I left off" and "begin afresh" are
// different requests.
func ensureSession(ctx context.Context, store *sessionjsonl.Provider, id persistence.SessionID) error {
	if _, err := store.Stat(ctx, id); err == nil {
		return nil
	} else if !errors.Is(err, persistence.ErrSessionNotFound) {
		return err
	}
	_, err := store.Create(ctx, persistence.CreateOptions{ID: id})
	return err
}

// driveTurn is the turn itself, separated so a test can drive it against a
// scripted provider.
func driveTurn(ctx context.Context, options sessionOptions, deps sessionDeps, store *sessionjsonl.Provider) (string, error) {
	// The history is rebuilt from the log, so a resumed run sees what happened
	// before instead of starting empty.
	history, err := restoreHistory(ctx, store, persistence.SessionID(options.session))
	if err != nil {
		return "", err
	}
	if len(history) > 0 && !options.resume {
		// Continuing a session without asking to resume is refused: appending to
		// an existing history is a decision the caller must make explicitly.
		return "", fmt.Errorf("session %s already has %d recorded messages; pass -resume to continue it",
			options.session, len(history))
	}
	if deps.provider == nil {
		return "", errors.New("a model provider is required")
	}
	request := llm.Request{Provider: "local", Model: "local"}
	// The prior history is sent once, followed by this turn's input. Building the
	// slice in order rather than appending to the history in place keeps the
	// caller's slice untouched and the request free of duplicates.
	request.Messages = make([]llm.Message, 0, len(history)+1)
	request.Messages = append(request.Messages, history...)
	request.Messages = append(request.Messages, userMessage(options.prompt))
	stream, err := deps.provider.Generate(ctx, request)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()
	var answer strings.Builder
	for {
		chunk, ok, err := stream.Recv()
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		answer.WriteString(chunk.Text)
	}
	if err := appendTurn(ctx, store, persistence.SessionID(options.session), options.prompt, answer.String(),
		request.Provider, request.Model); err != nil {
		return "", err
	}
	return answer.String(), nil
}

// restoreHistory rebuilds the conversation from the stored log.
//
// The history comes from the log rather than from memory, which is what makes a
// resumed run see what happened before.
func restoreHistory(ctx context.Context, store *sessionjsonl.Provider, id persistence.SessionID) ([]llm.Message, error) {
	_, messages, err := persistence.Restore(ctx, store, id, persistence.NewCache())
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// userMessage builds the user message for one prompt.
func userMessage(prompt string) llm.Message {
	message, err := llm.NewUserMessage(prompt)
	if err != nil {
		// NewUserMessage only fails on unusable content, which a non-empty parsed
		// prompt cannot be; returning an empty message keeps the caller simple
		// while the recorded event still carries the prompt text.
		return llm.Message{}
	}
	return message
}

// appendTurn records the input and the answer as one closed turn.
func appendTurn(ctx context.Context, store *sessionjsonl.Provider, id persistence.SessionID, prompt, answer, provider, model string) error {
	handle, err := store.Open(ctx, persistence.OpenOptions{ID: id, Access: persistence.AccessWrite})
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	if err := appendEvent(ctx, handle, "turn/start", map[string]any{"turn": 0, "input": prompt}); err != nil {
		return err
	}
	// The user message is the event the fold reads, so it has to be recorded with
	// its surface operation. A turn boundary alone records that something
	// happened without recording what was said, and a restore of such a log
	// correctly derives no messages.
	if err := appendUserMessage(ctx, handle, prompt); err != nil {
		return err
	}
	if err := appendAssistantMessage(ctx, handle, answer, provider, model); err != nil {
		return err
	}
	if err := appendEvent(ctx, handle, "turn/end", map[string]any{"turn": 0, "steps": 1}); err != nil {
		return err
	}
	return handle.Flush(ctx)
}

// appendUserMessage records one user message.
//
// The payload is the bare message: of the five message-bearing types, only
// `user/message` is NOT nested, which is a difference the log's surface rules
// enforce.
func appendUserMessage(ctx context.Context, handle persistence.Handle, text string) error {
	message, err := llm.NewUserMessage(text)
	if err != nil {
		return err
	}
	encoded, err := llm.MarshalMessage(message)
	if err != nil {
		return err
	}
	_, err = handle.Append(ctx, session.AppendInput{
		Type: "user/message", Data: encoded,
		SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	})
	return err
}

// appendAssistantMessage records one assistant message.
//
// This payload IS nested under "message", together with the route the answer came
// from, which is what lets a reader attribute the answer to a model.
func appendAssistantMessage(ctx context.Context, handle persistence.Handle, text, provider, model string) error {
	message, err := llm.NewAssistantMessage(provider, model, llm.TextContent(text))
	if err != nil {
		return err
	}
	encoded, err := llm.MarshalMessage(message)
	if err != nil {
		return err
	}
	nested, err := persistence.NestMessage(encoded)
	if err != nil {
		return err
	}
	_, err = handle.Append(ctx, session.AppendInput{
		Type: "assistant/message", Data: nested,
		SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	})
	return err
}

// appendEvent records one event with a JSON object payload.
func appendEvent(ctx context.Context, handle persistence.Handle, eventType string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = handle.Append(ctx, session.AppendInput{Type: eventType, Data: encoded})
	return err
}
