// Command session-replay stores a session in a temporary directory, flushes it,
// closes it, and reopens it from a fresh provider to show that the history and
// the outstanding work survive.
//
// It needs no credential and no network; the provider is scripted and the clock
// is fixed, so the output is byte-stable.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/plugins/sessionjsonl"
	"github.com/gosomea/deepseek-harness-go/session"
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
	root, err := os.MkdirTemp("", "session-replay-")
	if err != nil {
		return err
	}
	// The demonstration writes only under its own temporary directory, which is
	// what keeps it away from any existing session data.
	defer func() { _ = os.RemoveAll(root) }()
	ctx := context.Background()

	provider, err := sessionjsonl.New(sessionjsonl.Options{DataDir: filepath.Join(root, "sessions")})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "data dir=%s\n", filepath.Join("sessions"))

	// --- write a session and stop in the middle of a turn ---
	if _, err := provider.Create(ctx, persistence.CreateOptions{ID: "demo"}); err != nil {
		return err
	}
	handle, err := provider.Open(ctx, persistence.OpenOptions{ID: "demo", Access: persistence.AccessWrite})
	if err != nil {
		return err
	}
	message, err := llm.NewUserMessage("read a.txt")
	if err != nil {
		return err
	}
	message.ID = "m1"
	messageData, err := llm.MarshalMessage(message)
	if err != nil {
		return err
	}
	at := fixedTime
	appended := 0
	for _, event := range []session.AppendInput{
		{Type: "turn/start", Data: []byte(`{"turn":0,"input":"read a.txt"}`)},
		{Type: "user/message", Data: messageData, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		{Type: "step/start", Data: []byte(`{"turn":0,"step":0}`)},
		{Type: "tool/call", Data: []byte(`{"turn":0,"step":0,"callId":"call-1","name":"read_file","arguments":"{}"}`)},
	} {
		event.Time = &at
		if _, err := handle.Append(ctx, event); err != nil {
			return err
		}
		appended++
	}
	fmt.Fprintln(out, "--- writer ---")
	fmt.Fprintf(out, "appended=%d\n", appended)
	// A second writer must be refused while this handle owns the session.
	if _, err := provider.Open(ctx, persistence.OpenOptions{ID: "demo", Access: persistence.AccessWrite}); err != nil {
		fmt.Fprintf(out, "second writer refused=%v\n", errors.Is(err, persistence.ErrWriterExists))
	}
	if err := handle.Flush(ctx); err != nil {
		return err
	}
	if err := handle.Close(); err != nil {
		return err
	}

	// --- reopen from a fresh provider, as a new process would ---
	reopened, err := sessionjsonl.New(sessionjsonl.Options{DataDir: filepath.Join(root, "sessions")})
	if err != nil {
		return err
	}
	stat, err := reopened.Stat(ctx, "demo")
	if err != nil {
		return err
	}
	reader, err := reopened.Open(ctx, persistence.OpenOptions{ID: "demo", Access: persistence.AccessRead})
	if err != nil {
		return err
	}
	state, err := persistence.Resume(ctx, reader)
	if err != nil {
		return err
	}
	history, err := session.DeriveMessages(state.Events)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "--- reopened ---")
	fmt.Fprintf(out, "events=%d version=%d writerHeld=%v\n", stat.Events, stat.FormatVersion, stat.WriterHeld)
	fmt.Fprintf(out, "openTurn=%v unsettled=%v needsAttention=%v\n",
		state.OpenTurn, state.UnsettledCalls, state.NeedsAttention())
	for index, restored := range history {
		fmt.Fprintf(out, "history[%d] role=%s content=%q\n", index, restored.Role, llm.ContentText(restored.Content))
	}
	if err := reader.Close(); err != nil {
		return err
	}

	// --- damage a copy and show the two recovery verdicts ---
	return reportDamagedCopy(ctx, out, provider, root)
}

// reportDamagedCopy takes a copy of the artifact, damages it two ways, and
// reports what recovery says about each.
func reportDamagedCopy(ctx context.Context, out io.Writer, provider *sessionjsonl.Provider, root string) error {
	path, err := sessionjsonl.SessionPath(filepath.Join(root, "sessions"), "demo")
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "--- damaged copies ---")
	// A torn tail: the writer stopped mid-record. This is repairable.
	tornPath := filepath.Join(root, "torn-demo.jsonl")
	if err := os.WriteFile(tornPath, append(append([]byte(nil), body...), []byte(`{"seq":4,"payl`)...), 0o644); err != nil {
		return err
	}
	torn, err := sessionjsonl.InspectArtifactAt(tornPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "torn tail: complete=%d repairable=%v reason=%q\n", torn.Complete, torn.Repairable, torn.Reason)

	// A damaged middle record. This is not repairable.
	lines := splitLines(string(body))
	if len(lines) < 3 {
		return fmt.Errorf("expected a header and records, got %d lines", len(lines))
	}
	lines[2] = replaceOnce(lines[2], `"checksum":"`, `"checksum":"00`)
	middlePath := filepath.Join(root, "middle-demo.jsonl")
	if err := os.WriteFile(middlePath, []byte(joinLines(lines)), 0o644); err != nil {
		return err
	}
	middle, err := sessionjsonl.InspectArtifactAt(middlePath)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "middle damage: complete=%d repairable=%v reason=%q\n", middle.Complete, middle.Repairable, middle.Reason)
	_ = provider
	return nil
}
