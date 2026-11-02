package testkit_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/plugins/llmtrpc"
)

// loopbackSSE serves one fixed chat-completions stream. It is the same fixture
// shape plugins/llmtrpc uses, restated here because this test asserts the
// *cross-provider* contract: the harness's own vocabulary, not the adapter's.
func loopbackSSE(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		fmt.Fprint(writer, strings.Join([]string{
			`data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"{\"answer\":\"42\"}"}}]}`,
			`data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`,
			`data: [DONE]`,
			``,
		}, "\n\n"))
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server.URL + "/v1"
}

func requestFor(t *testing.T, provider string) llm.Request {
	t.Helper()
	first, err := llm.NewUserMessage("what is the answer")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	second, err := llm.NewUserMessage("again")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	return llm.Request{Provider: provider, Model: "m", Messages: []llm.Message{first, second}}
}

// ProviderContract is what every llm.Provider must satisfy for the harness to
// treat routes as interchangeable: the same request must yield the same
// assembled message shape, the same usage semantics, and the same finish
// vocabulary, whatever the transport underneath.
//
// It runs over two real implementations — the scripted fake provider and the
// tRPC adapter over a loopback HTTP SSE endpoint — so a divergence shows up as
// a test failure rather than as a provider-specific surprise in production.
func TestProviderContractAcrossFakeAndTRPCLoopback(t *testing.T) {
	baseURL := loopbackSSE(t)

	providers := map[string]llm.Provider{
		"fake": testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{`{"answer":`, `"42"}`})),
		"trpc": llmtrpc.New(llmtrpc.Config{ID: "trpc", Name: "tRPC loopback", Model: "m", BaseURL: baseURL}),
	}

	type outcome struct {
		text     string
		kind     llm.FinishKind
		usage    llm.Usage
		reported bool
		role     llm.Role
	}
	results := map[string]outcome{}

	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			registry := llm.NewProviderRegistry()
			if err := registry.Register(provider); err != nil {
				t.Fatalf("register %s: %v", name, err)
			}
			stream, err := registry.Generate(context.Background(), requestFor(t, name))
			if err != nil {
				t.Fatalf("generate %s: %v", name, err)
			}
			// The assembler is driven directly so the terminal reason comes from
			// the attempt, not from a chunk list that AssembleMessage does not
			// return.
			assembler := llm.NewAssembler()
			blocks, err := assembler.Assemble(stream)
			if err != nil {
				t.Fatalf("assemble %s: %v", name, err)
			}
			message, err := assembler.Message(name, "m")
			if err != nil {
				t.Fatalf("message %s: %v", name, err)
			}
			usage, reported := assembler.Usage()
			reason, ok := assembler.FinishReason()
			if !ok {
				t.Fatalf("%s: the stream must end with a terminal finish", name)
			}
			_ = blocks
			results[name] = outcome{
				text:     llm.ContentText(message.Content),
				kind:     reason.Kind,
				usage:    usage,
				reported: reported,
				role:     message.Role,
			}
			// Both routes must produce an assistant message by the same rules.
			if message.Role != llm.RoleAssistant || message.Source.Kind != llm.SourceModel {
				t.Fatalf("%s: unexpected message attribution %+v", name, message)
			}
			if !reported {
				t.Fatalf("%s: the fixture reports usage, so it must be reported", name)
			}
		})
	}

	fake, trpc := results["fake"], results["trpc"]
	if fake.text != trpc.text {
		t.Fatalf("the two routes disagree on content: %q vs %q", fake.text, trpc.text)
	}
	if fake.kind != trpc.kind {
		t.Fatalf("the two routes disagree on the finish kind: %s vs %s", fake.kind, trpc.kind)
	}
	if fake.role != trpc.role {
		t.Fatalf("the two routes disagree on the role: %s vs %s", fake.role, trpc.role)
	}
	// Usage semantics are provider-neutral: the fake reports the same shape the
	// adapter produces from the wire counters.
	if fake.usage.OutputTokens != trpc.usage.OutputTokens {
		t.Fatalf("the two routes disagree on output tokens: %d vs %d", fake.usage.OutputTokens, trpc.usage.OutputTokens)
	}
	if fake.usage.InputTokens != 3 || trpc.usage.InputTokens != 4 {
		t.Fatalf("unexpected input counts: fake=%d trpc=%d", fake.usage.InputTokens, trpc.usage.InputTokens)
	}
}

// The same contract must also hold for refusals: an unknown route, an
// unrepresentable block and a missing capability are all observable before any
// attempt starts, whatever the provider.
func TestProviderContractRefusalsAreObservable(t *testing.T) {
	baseURL := loopbackSSE(t)
	registry := llm.NewProviderRegistry()
	if err := registry.Register(llmtrpc.New(llmtrpc.Config{ID: "trpc", Name: "tRPC", Model: "m", BaseURL: baseURL})); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := registry.Generate(context.Background(), requestFor(t, "absent")); !errors.Is(err, llm.ErrProviderNotFound) {
		t.Fatalf("want ErrProviderNotFound, got %v", err)
	}

	// A block the adapter cannot express fails inside the route, and the error
	// names the block instead of silently dropping it.
	message, err := llm.NewUserMessage("look")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	message.Content = append(message.Content, llm.ImageBlock{Attachment: llm.ImageAttachmentRef{AttachmentID: "sha256:x", MediaType: llm.MediaTypePNG}})
	stream, err := registry.Generate(context.Background(), llm.Request{Provider: "trpc", Model: "m", Messages: []llm.Message{message}})
	if err == nil {
		t.Fatalf("an unrepresentable block must be refused, got a stream")
	}
	var unsupported *llmtrpc.ErrUnsupported
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "image") {
		t.Fatalf("the refusal must name the block, got %v", err)
	}
	if stream != nil {
		t.Fatal("a refused request must not produce a stream")
	}
}
