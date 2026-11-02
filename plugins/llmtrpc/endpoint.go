package llmtrpc

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// SmokeStatus is the outcome of an optional live-endpoint check.
//
// It exists because "the live check did not run" and "the live check passed" are
// different facts, and collapsing them into a boolean makes a skipped run look
// like a successful one. This is the vocabulary the report uses.
type SmokeStatus string

// The smoke outcomes this package reports.
const (
	// SmokePassed means a real endpoint answered as expected.
	SmokePassed SmokeStatus = "passed"
	// SmokeSkippedNoCredential means no credential was configured, so the check
	// did not run. It is NOT a pass.
	SmokeSkippedNoCredential SmokeStatus = "skipped-no-credential"
	// SmokeSkippedByRequest means the caller did not ask for a live check.
	SmokeSkippedByRequest SmokeStatus = "skipped-not-requested"
	// SmokeFailed means a live endpoint answered incorrectly.
	SmokeFailed SmokeStatus = "failed"
)

// Verifiable reports whether the status represents evidence of working.
//
// Only SmokePassed does: a skipped run produced no evidence, so a report that
// counted it as a pass would be claiming something it never observed.
func (s SmokeStatus) Verifiable() bool { return s == SmokePassed }

// SmokeResult records what a live-endpoint check observed.
type SmokeResult struct {
	// Status is the outcome.
	Status SmokeStatus
	// Endpoint is the redacted endpoint, so a report can say what was tried.
	Endpoint string
	// Model is the model that was asked for.
	Model string
	// Detail explains a failure or a skip.
	Detail string
	// Duration is how long the attempt took.
	Duration time.Duration
}

// Describe renders the result for a report, with no secret in it.
func (r SmokeResult) Describe() string {
	line := fmt.Sprintf("smoke=%s endpoint=%s model=%s", r.Status, r.Endpoint, r.Model)
	if r.Detail != "" {
		line += " detail=" + r.Detail
	}
	return line
}

// SmokeOptions configures a live-endpoint check.
type SmokeOptions struct {
	// Endpoint is the resolved endpoint to try.
	Endpoint ResolvedEndpoint
	// Prompt is the user message to send. Empty uses a fixed one.
	Prompt string
	// Timeout bounds the attempt.
	Timeout time.Duration
	// RequireCredential makes a missing credential a skip rather than a failure.
	//
	// It defaults to true, which is the honest default: without a credential there
	// is nothing to test, and reporting that as a failure would train a reader to
	// ignore the result.
	RequireCredential bool
}

// Smoke asks a real endpoint for one short answer.
//
// It is deliberately separate from the offline tests: those run in `make check`
// and must never need a network or a credential, while this check is run
// explicitly and its status is recorded. Nothing here writes to the repository.
func Smoke(ctx context.Context, options SmokeOptions) SmokeResult {
	start := time.Now()
	result := SmokeResult{Endpoint: options.Endpoint.Redacted(), Model: options.Endpoint.Model}
	if options.Endpoint.APIKey == "" {
		result.Status = SmokeSkippedNoCredential
		result.Detail = "no credential was configured"
		result.Duration = time.Since(start)
		return result
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	adapter := New(FromResolved(options.Endpoint, "smoke", "live smoke"))
	defer func() { _ = adapter.Close() }()

	prompt := options.Prompt
	if strings.TrimSpace(prompt) == "" {
		prompt = "Reply with the single word: ready"
	}
	message, err := llm.NewUserMessage(prompt)
	if err != nil {
		result.Status = SmokeFailed
		result.Detail = "cannot build the request: " + err.Error()
		result.Duration = time.Since(start)
		return result
	}
	stream, err := adapter.Generate(runCtx, llm.Request{
		Provider: "smoke", Model: options.Endpoint.Model, Messages: []llm.Message{message},
	})
	if err != nil {
		result.Status = SmokeFailed
		result.Detail = "the endpoint could not be reached: " + err.Error()
		result.Duration = time.Since(start)
		return result
	}
	defer func() { _ = stream.Close() }()
	var text strings.Builder
	for {
		chunk, ok, err := stream.Recv()
		if err != nil {
			result.Status = SmokeFailed
			result.Detail = "the stream failed: " + err.Error()
			result.Duration = time.Since(start)
			return result
		}
		if !ok {
			break
		}
		text.WriteString(chunk.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		// An empty answer is a failure of the live check: it proves the endpoint
		// answered but not that the pipeline produced anything.
		result.Status = SmokeFailed
		result.Detail = "the endpoint returned no text"
		result.Duration = time.Since(start)
		return result
	}
	result.Status = SmokePassed
	result.Duration = time.Since(start)
	return result
}

// SmokeConfigFromEnvironment builds a smoke configuration from the environment.
//
// It returns the status a caller should record when the environment is not
// complete, rather than an error: "not configured" is a result of the check, not
// a failure of the program.
func SmokeConfigFromEnvironment(lookup Lookup) (SmokeOptions, SmokeStatus, error) {
	if lookup == nil {
		lookup = OSEnvironment
	}
	apiKey, hasKey := lookup(EnvAPIKey)
	if !hasKey || strings.TrimSpace(apiKey) == "" {
		return SmokeOptions{}, SmokeSkippedNoCredential, nil
	}
	baseURL := strings.TrimSpace(os.Getenv(EnvBaseURL))
	if fromLookup, ok := lookup(EnvBaseURL); ok && strings.TrimSpace(fromLookup) != "" {
		baseURL = strings.TrimSpace(fromLookup)
	}
	model := ""
	if fromLookup, ok := lookup(EnvModel); ok {
		model = strings.TrimSpace(fromLookup)
	}
	resolved, err := ResolveEndpoint(EndpointConfig{
		BaseURL: baseURL, APIKey: apiKey, Model: model,
	}, lookup)
	if err != nil {
		return SmokeOptions{}, SmokeFailed, err
	}
	return SmokeOptions{Endpoint: resolved}, "", nil
}
