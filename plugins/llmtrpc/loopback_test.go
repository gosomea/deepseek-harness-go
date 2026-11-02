package llmtrpc_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/plugins/llmtrpc"
)

// sseServer answers one chat-completions request with a fixed SSE body. It is a
// loopback fixture rather than a stub: the adapter's real HTTP client, real
// decoder and real translation run, and only the endpoint is local, so the
// contract is exercised without a credential or external network.
func sseServer(t *testing.T, body string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		atomic.AddInt32(&calls, 1)
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(writer, body)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// sseChunk renders one OpenAI-compatible streaming chunk.
func sseChunk(payload string) string {
	return "data: " + payload + "\n\n"
}

const textReasoningToolBody = `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{"reasoning_content":"think "}}]}

data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{"reasoning_content":"hard"}}]}

data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{"content":"Hel"}}]}

data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{"content":"lo"}}]}

data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"read_file"}}]}}]}

data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"pa"}}]}}]}

data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.txt\"}"}}]}}]}

data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":3,"cache_read_tokens":2},"completion_tokens_details":{"reasoning_tokens":4}}}

data: [DONE]

`

func loopbackAdapter(t *testing.T, baseURL string) *llmtrpc.Adapter {
	t.Helper()
	return llmtrpc.New(llmtrpc.Config{ID: "loopback", Name: "Loopback", Model: "fixture", BaseURL: baseURL + "/v1"})
}

func userRequest(t *testing.T) llm.Request {
	t.Helper()
	message, err := llm.NewUserMessage("hello")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	return llm.Request{Provider: "loopback", Model: "fixture", Messages: []llm.Message{message}}
}

// The adapter must translate reasoning, text and split tool arguments through a
// real HTTP stream into the same chunk vocabulary the fake provider produces.
func TestAdapterStreamsReasoningTextAndSplitToolArgumentsOverLoopbackSSE(t *testing.T) {
	server, calls := sseServer(t, textReasoningToolBody)
	adapter := loopbackAdapter(t, server.URL)
	registry := llm.NewProviderRegistry()
	if err := registry.Register(adapter); err != nil {
		t.Fatalf("register: %v", err)
	}
	stream, err := registry.Generate(context.Background(), userRequest(t))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	assembler := llm.NewAssembler()
	blocks, err := assembler.Assemble(stream)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("want exactly one HTTP request, got %d", got)
	}
	var reasoning, text string
	var tool llm.ToolCallBlock
	for _, block := range blocks {
		switch typed := block.(type) {
		case llm.ReasoningBlock:
			reasoning += typed.Text
		case llm.TextBlock:
			text += typed.Text
		case llm.ToolCallBlock:
			tool = typed
		}
	}
	if reasoning != "think hard" {
		t.Fatalf("reasoning lost or merged: %q", reasoning)
	}
	if text != "Hello" {
		t.Fatalf("text lost or merged: %q", text)
	}
	if tool.ID != "call-1" || tool.Name != "read_file" || tool.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("tool call fragments were not joined correctly: %+v", tool)
	}
	reason, ok := assembler.FinishReason()
	if !ok || reason.Kind != llm.FinishToolCalls {
		t.Fatalf("want tool-calls finish, got %v (ok=%v)", reason, ok)
	}
}

// The framework folds cache hits into the prompt total; the harness vocabulary
// is disjoint, so the adapter must subtract them out and keep the optional
// counters absent when the provider did not disclose them.
func TestAdapterConvertsUsageToDisjointCounts(t *testing.T) {
	server, _ := sseServer(t, textReasoningToolBody)
	adapter := loopbackAdapter(t, server.URL)
	stream, err := adapter.Generate(context.Background(), userRequest(t))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	assembler := llm.NewAssembler()
	if _, err := assembler.Assemble(stream); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	usage, reported := assembler.Usage()
	if !reported {
		t.Fatal("the fixture reports usage, so it must be reported")
	}
	// The fixture reports prompt_tokens 11 with cached_tokens 3 (and the
	// framework parses cache_read_tokens as 0), so the uncached input is 8 and
	// the disjoint vocabulary must not report the folded total.
	if usage.InputTokens != 8 || usage.OutputTokens != 5 {
		t.Fatalf("want disjoint 8/5, got %+v", usage)
	}
	if usage.CacheReadTokens == nil || *usage.CacheReadTokens != 3 {
		t.Fatalf("want 3 cache-read tokens, got %v", usage.CacheReadTokens)
	}
	if usage.ReasoningTokens == nil || *usage.ReasoningTokens != 4 {
		t.Fatalf("want 4 reasoning tokens, got %v", usage.ReasoningTokens)
	}
	if usage.TotalTokens == nil || *usage.TotalTokens != 16 {
		t.Fatalf("want the provider total 16, got %v", usage.TotalTokens)
	}
}

// An API-level error arrives inside a successful HTTP response and must become a
// terminal failure, never a successful empty answer.
func TestAdapterMapsStreamErrorToAFailedAttempt(t *testing.T) {
	body := sseChunk(`{"id":"1","object":"error","created":1,"model":"fixture","choices":[],"error":{"message":"rate limited","type":"rate_limit_error"}}`) + "data: [DONE]\n\n"
	server, _ := sseServer(t, body)
	adapter := loopbackAdapter(t, server.URL)
	stream, err := adapter.Generate(context.Background(), userRequest(t))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	assembler := llm.NewAssembler()
	if _, err := assembler.Assemble(stream); err == nil {
		t.Fatal("an API error must fail the attempt")
	} else {
		var failure *llm.FailureError
		if !errors.As(err, &failure) {
			t.Fatalf("want a FailureError, got %v", err)
		}
		if failure.Failure.Code != "rate_limit_error" || failure.Failure.Message != "rate limited" {
			t.Fatalf("unexpected failure %+v", failure.Failure)
		}
	}
	if _, err := assembler.Message("loopback", "fixture"); err == nil {
		t.Fatal("a failed attempt must not become a message")
	}
}

// A connection that cannot be established is reported inside the stream, not by
// Generate.
//
// This is a recorded framework difference: the framework's GenerateContent
// returns a channel for a transport failure and delivers the dial error as the
// first response, so the adapter must surface it as a terminal failure. A test
// that expected an inline error would be asserting behaviour the framework does
// not have. The assertion is what matters to a caller: no successful answer.
func TestAdapterReportsConnectionFailureInsideTheStream(t *testing.T) {
	adapter := llmtrpc.New(llmtrpc.Config{ID: "dead", Name: "Dead", Model: "fixture", BaseURL: "http://127.0.0.1:1/v1"})
	stream, err := adapter.Generate(context.Background(), userRequest(t))
	if err != nil {
		// An inline failure is also acceptable: it is still observable and still
		// cannot be mistaken for a successful answer.
		return
	}
	assembler := llm.NewAssembler()
	if _, err := assembler.Assemble(stream); err == nil {
		t.Fatal("an unreachable endpoint must not produce a successful answer")
	} else {
		var failure *llm.FailureError
		if !errors.As(err, &failure) {
			t.Fatalf("want a FailureError, got %v", err)
		}
		if failure.Failure.Code != "transport-error" {
			t.Fatalf("want a transport error code, got %q", failure.Failure.Code)
		}
	}
}

// Cancellation must surface as an error rather than a clean end of stream.
func TestAdapterHonoursCancellation(t *testing.T) {
	server, _ := sseServer(t, textReasoningToolBody)
	adapter := loopbackAdapter(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := adapter.Generate(ctx, userRequest(t))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cancel()
	_, err = llm.NewAssembler().Assemble(stream)
	if err == nil {
		t.Fatal("a canceled stream must report an error")
	}
}

// A closed adapter refuses new attempts and Close is idempotent.
func TestAdapterCloseRefusesLaterAttempts(t *testing.T) {
	server, _ := sseServer(t, textReasoningToolBody)
	adapter := loopbackAdapter(t, server.URL)
	if err := adapter.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
	if !adapter.Closed() {
		t.Fatal("Close must be observable")
	}
	if _, err := adapter.Generate(context.Background(), userRequest(t)); !errors.Is(err, llmtrpc.ErrClosed) {
		t.Fatalf("want ErrClosed, got %v", err)
	}
}

// A route without a base URL is a configuration error, reported before any
// request is attempted.
func TestAdapterRefusesAMisconfiguredRoute(t *testing.T) {
	bare := llmtrpc.New(llmtrpc.Config{ID: "no-url"})
	if _, err := bare.Generate(context.Background(), userRequest(t)); err == nil {
		t.Fatal("a route without a base URL must be refused")
	}
	nameless := llmtrpc.New(llmtrpc.Config{BaseURL: "http://127.0.0.1:1/v1"})
	if _, err := nameless.Generate(context.Background(), userRequest(t)); err == nil {
		t.Fatal("a route without an id must be refused")
	}
}

// The adapter declares exactly the capabilities its translation preserves, so a
// caller can depend on the declaration.
func TestAdapterDeclaresPreservedCapabilities(t *testing.T) {
	adapter := llmtrpc.New(llmtrpc.Config{ID: "r", Name: "R", Model: "m", BaseURL: "http://127.0.0.1:1/v1"})
	info := adapter.Info()
	if info.ID != "r" || info.Name != "R" {
		t.Fatalf("unexpected info %+v", info)
	}
	for _, capability := range []llm.Capability{
		llm.CapabilityStreaming, llm.CapabilityReasoning, llm.CapabilityToolCalls, llm.CapabilityUsage,
	} {
		if !info.Supports(capability) {
			t.Fatalf("declared capability %s is missing", capability)
		}
	}
	override := llmtrpc.New(llmtrpc.Config{ID: "r", BaseURL: "http://x", Capabilities: []llm.Capability{llm.CapabilityStreaming}})
	if override.Info().Supports(llm.CapabilityToolCalls) {
		t.Fatal("an explicit capability override must be honoured")
	}
}
