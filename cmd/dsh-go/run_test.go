package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// scriptedProvider returns one fixed text answer, so a session run needs no
// network and no credential.
type scriptedProvider struct {
	// text is the answer to stream back.
	text string
	// requests records the requests it received.
	requests []llm.Request
	// err, when set, fails the attempt.
	err error
}

// Generate records the request and returns a one-chunk stream.
func (p *scriptedProvider) Generate(_ context.Context, request llm.Request) (llm.Stream, error) {
	p.requests = append(p.requests, request)
	if p.err != nil {
		return nil, p.err
	}
	return &scriptedStream{text: p.text}, nil
}

// Info describes the scripted route.
func (p *scriptedProvider) Info() llm.ProviderInfo {
	return llm.ProviderInfo{ID: "scripted", Name: "Scripted"}
}

// scriptedStream emits one text chunk then ends.
type scriptedStream struct {
	// text is the chunk to emit.
	text string
	// sent records whether the chunk was already emitted.
	sent bool
	// closed records whether the stream was released.
	closed bool
}

// Recv returns the single chunk, then reports the end of the stream.
func (s *scriptedStream) Recv() (llm.StreamChunk, bool, error) {
	if s.sent {
		return llm.StreamChunk{}, false, nil
	}
	s.sent = true
	return llm.StreamChunk{Type: llm.ChunkTextDelta, Text: s.text, Index: 0}, true, nil
}

// Close releases the stream.
func (s *scriptedStream) Close() error {
	s.closed = true
	return nil
}

// runArgs builds a command line for a run in a temporary workspace.
func runArgs(t *testing.T, workspace, session, prompt string, extra ...string) []string {
	t.Helper()
	args := []string{
		"-workspace", workspace,
		"-session", session,
		"-prompt", prompt,
	}
	return append(args, extra...)
}

// A successful run prints the answer and exits zero.
func TestSessionRunSucceeds(t *testing.T) {
	root := t.TempDir()
	provider := &scriptedProvider{text: "hello there"}
	var stdout, stderr bytes.Buffer
	options, err := parseSession(runArgs(t, root, "s-1", "say hello"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	code := runSession(context.Background(), options, sessionDeps{
		provider: provider, stdout: &stdout, stderr: &stderr,
	})
	if code != exitOK {
		t.Fatalf("want exit 0, got %d (%s)", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "hello there" {
		t.Fatalf("unexpected output %q", stdout.String())
	}
	if len(provider.requests) != 1 {
		t.Fatalf("want one request, got %d", len(provider.requests))
	}
	// The request carries the prompt and nothing else on a fresh session.
	messages := provider.requests[0].Messages
	if len(messages) != 1 || llm.ContentText(messages[0].Content) != "say hello" {
		t.Fatalf("unexpected request messages %+v", messages)
	}
	// The run stores its artifacts INSIDE the workspace, so it cannot write
	// outside the world it was given.
	if _, err := os.Stat(filepath.Join(root, ".dsh-go", "sessions")); err != nil {
		t.Fatalf("the store must live inside the workspace: %v", err)
	}
}

// Continuing an existing session without asking to resume is refused, because
// appending to a history is a decision the caller must make explicitly.
func TestSessionRefusesToAppendWithoutResume(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	first, err := parseSession(runArgs(t, root, "s-1", "first"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if code := runSession(context.Background(), first, sessionDeps{
		provider: &scriptedProvider{text: "one"}, stdout: &stdout, stderr: &stderr,
	}); code != exitOK {
		t.Fatalf("first run must succeed, got %d (%s)", code, stderr.String())
	}
	// A second run without -resume is refused.
	stderr.Reset()
	second, err := parseSession(runArgs(t, root, "s-1", "second"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	code := runSession(context.Background(), second, sessionDeps{
		provider: &scriptedProvider{text: "two"}, stdout: &stdout, stderr: &stderr,
	})
	if code != exitFailure {
		t.Fatalf("want a failure, got %d", code)
	}
	if !strings.Contains(stderr.String(), "resume") {
		t.Fatalf("the refusal must name the remedy: %q", stderr.String())
	}
}

// A resumed run sees the stored history, which is what makes recovery real.
func TestSessionResumeCarriesHistory(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	first, err := parseSession(runArgs(t, root, "s-1", "first question"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if code := runSession(context.Background(), first, sessionDeps{
		provider: &scriptedProvider{text: "first answer"}, stdout: &stdout, stderr: &stderr,
	}); code != exitOK {
		t.Fatalf("first run must succeed, got %d (%s)", code, stderr.String())
	}
	// The second run resumes and its request carries the stored events plus the
	// new prompt.
	provider := &scriptedProvider{text: "second answer"}
	resumed, err := parseSession(runArgs(t, root, "s-1", "second question", "-resume"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if code := runSession(context.Background(), resumed, sessionDeps{
		provider: provider, stdout: &stdout, stderr: &stderr,
	}); code != exitOK {
		t.Fatalf("resumed run must succeed, got %d (%s)", code, stderr.String())
	}
	if len(provider.requests) != 1 {
		t.Fatalf("want one request, got %d", len(provider.requests))
	}
	// The stored events are turn boundaries plus the prompt; the request must
	// carry the new prompt and must not duplicate anything.
	messages := provider.requests[0].Messages
	if len(messages) == 0 {
		t.Fatal("the request must carry the prompt")
	}
	last := llm.ContentText(messages[len(messages)-1].Content)
	if last != "second question" {
		t.Fatalf("the last message must be the new prompt, got %q", last)
	}
	// No message appears twice, which is what a duplicated-history defect would
	// look like.
	seen := map[string]int{}
	for _, message := range messages {
		seen[llm.ContentText(message.Content)]++
	}
	for text, count := range seen {
		if count > 1 {
			t.Fatalf("message %q appears %d times", text, count)
		}
	}
}

// A missing provider is reported rather than answered with an empty response.
func TestSessionWithoutAProviderFails(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	options, err := parseSession(runArgs(t, root, "s-1", "hello"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	code := runSession(context.Background(), options, sessionDeps{stdout: &stdout, stderr: &stderr})
	if code != exitFailure {
		t.Fatalf("want a failure without a provider, got %d", code)
	}
	if !strings.Contains(stderr.String(), "provider") {
		t.Fatalf("the failure must name the missing configuration: %q", stderr.String())
	}
}

// A cancelled context is reported as an interruption, not as a failure.
func TestSessionInterruptionHasItsOwnExitCode(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	options, err := parseSession(runArgs(t, root, "s-1", "hello"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code := runSession(ctx, options, sessionDeps{
		provider: &scriptedProvider{text: "x"}, stdout: &stdout, stderr: &stderr,
	})
	if code != exitInterrupted {
		t.Fatalf("want %d, got %d (%s)", exitInterrupted, code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "interrupted") {
		t.Fatalf("the report must say it was interrupted: %q", stderr.String())
	}
}

// A signal delivered while the run is active stops it and reports interruption.
func TestSessionSignalStopsTheRun(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	options, err := parseSession(runArgs(t, root, "s-1", "hello"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	signals := make(chan os.Signal, 1)
	signals <- os.Interrupt
	code := runSession(context.Background(), options, sessionDeps{
		provider: &scriptedProvider{text: "x"}, stdout: &stdout, stderr: &stderr, signals: signals,
	})
	if code != exitInterrupted {
		t.Fatalf("want %d, got %d (%s)", exitInterrupted, code, stderr.String())
	}
}

// A provider failure is reported as a failure with its reason.
func TestSessionProviderFailureIsReported(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	options, err := parseSession(runArgs(t, root, "s-1", "hello"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	code := runSession(context.Background(), options, sessionDeps{
		provider: &scriptedProvider{err: errors.New("endpoint refused")}, stdout: &stdout, stderr: &stderr,
	})
	if code != exitFailure {
		t.Fatalf("want a failure, got %d", code)
	}
	if !strings.Contains(stderr.String(), "endpoint refused") {
		t.Fatalf("the reason must be reported: %q", stderr.String())
	}
}

// The command line is validated before anything runs.
func TestSessionCommandLineValidation(t *testing.T) {
	var stderr bytes.Buffer
	cases := [][]string{
		{"-session", "s", "-prompt", "p"},
		{"-workspace", "/tmp", "-prompt", "p"},
		{"-workspace", "/tmp", "-session", "s"},
		{"-workspace", "/tmp", "-session", "s", "-prompt", "p", "-headless", "invented"},
	}
	for _, args := range cases {
		if _, err := parseSession(args, &stderr); err == nil {
			t.Fatalf("args %v must be rejected", args)
		} else if !errors.As(err, &usageError{}) {
			t.Fatalf("args %v must fail as a usage error, got %v", args, err)
		}
	}
	// The default headless policy is deny, which never grants.
	options, err := parseSession(runArgs(t, t.TempDir(), "s", "p"), &stderr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if options.headless != "deny" {
		t.Fatalf("the default policy must be deny, got %q", options.headless)
	}
}

// The session subcommand is dispatched by name, and the existing configuration
// behaviour is unchanged for a flag-shaped command line.
func TestSessionSubcommandDispatch(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"session", "-workspace", t.TempDir(), "-session", "s-1", "-prompt", "p"}, &stdout, &stderr)
	// With no configured endpoint the run fails, but it must fail as a session
	// run (exit code 1) rather than as a missing -config option (exit code 2).
	if err == nil {
		t.Fatal("a run without a configured endpoint must fail")
	}
	var usage usageError
	if errors.As(err, &usage) {
		t.Fatalf("a session run must not be reported as a usage error: %v", err)
	}
	// A command line without the subcommand still requires -config.
	configErr := run(nil, &stdout, &stderr)
	if configErr == nil {
		t.Fatal("the configuration mode still requires -config")
	}
	var usageErr usageError
	if !errors.As(configErr, &usageErr) {
		t.Fatalf("a missing -config must be a usage error, got %v", configErr)
	}
}

// The endpoint description never carries a credential.
func TestEndpointDescriptionNeverCarriesASecret(t *testing.T) {
	t.Setenv("DSH_LLM_API_KEY", "sk-secret-value-0123456789abcdef")
	t.Setenv("DSH_LLM_BASE_URL", "https://api.example.com")
	t.Setenv("DSH_LLM_MODEL", "m-1")
	description := endpointDescription()
	if strings.Contains(description, "sk-secret-value") {
		t.Fatalf("the description leaked the credential: %q", description)
	}
	if !strings.Contains(description, "m-1") {
		t.Fatalf("the description must name the model: %q", description)
	}
}
