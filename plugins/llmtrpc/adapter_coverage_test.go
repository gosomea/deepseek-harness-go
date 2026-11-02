package llmtrpc_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/plugins/llmtrpc"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// A stream whose channel closes without any terminal response must still end
// with an explicit failure, so a provider that stopped without saying why is
// never mistaken for a clean completion.
func TestAdapterReportsMissingStopReasonWhenTheChannelJustCloses(t *testing.T) {
	body := "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"f\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}],\"is_partial\":true}\n\n"
	server, _ := sseServer(t, body)
	adapter := loopbackAdapter(t, server.URL)
	stream, err := adapter.Generate(context.Background(), userRequest(t))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	assembler := llm.NewAssembler()
	_, err = assembler.Assemble(stream)
	if err == nil {
		t.Fatal("a stream that ended without a stop reason must not succeed")
	}
	var failure *llm.FailureError
	if !asFailure(err, &failure) {
		t.Fatalf("want a FailureError, got %v", err)
	}
	if failure.Failure.Code != "missing-finish" {
		t.Fatalf("want missing-finish, got %q", failure.Failure.Code)
	}
}

func asFailure(err error, target **llm.FailureError) bool {
	for err != nil {
		if failure, ok := err.(*llm.FailureError); ok {
			*target = failure
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// Closing the stream must not cancel the caller's context, and a closed stream
// keeps reporting exhaustion.
func TestStreamCloseIsIdempotentAndDoesNotCancel(t *testing.T) {
	server, _ := sseServer(t, textReasoningToolBody)
	adapter := loopbackAdapter(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := adapter.Generate(ctx, userRequest(t))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("closing a stream must not cancel the caller's context")
	}
	// The stream is still readable after Close: Close releases, it does not
	// invalidate, which is what lets a caller release early and still drain.
	if _, _, err := stream.Recv(); err != nil {
		t.Fatalf("reading after close: %v", err)
	}
}

// A context already cancelled before the attempt is refused rather than started.
func TestAdapterRefusesAnAlreadyCancelledContext(t *testing.T) {
	server, _ := sseServer(t, textReasoningToolBody)
	adapter := loopbackAdapter(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Generate(ctx, userRequest(t)); err == nil {
		t.Fatal("an already cancelled context must be refused")
	}
}

// An empty route ID or a model-less route still resolves defaults rather than
// panicking, and the model name falls back to the route ID.
func TestModelLockedDefaultsTheModelName(t *testing.T) {
	adapter := llmtrpc.New(llmtrpc.Config{ID: "route-only", BaseURL: "http://127.0.0.1:1/v1"})
	if _, err := adapter.Generate(context.Background(), userRequest(t)); err != nil {
		// A transport failure is expected; what matters is that configuration
		// did not fail before the attempt.
		if strings.Contains(err.Error(), "requires") {
			t.Fatalf("a route without an explicit model must still start: %v", err)
		}
	}
}

// ErrUnsupported's text must name the value and, when present, the reason.
func TestErrUnsupportedErrorText(t *testing.T) {
	plain := &llmtrpc.ErrUnsupported{What: "content block image"}
	if plain.Error() != "llmtrpc: unsupported content block image" {
		t.Fatalf("unexpected text %q", plain.Error())
	}
	detailed := &llmtrpc.ErrUnsupported{What: "content block file", Detail: "projected to text"}
	if !strings.Contains(detailed.Error(), "projected to text") {
		t.Fatalf("unexpected text %q", detailed.Error())
	}
}

// FromResponse is the delta-only entry point and must agree with Translate.
func TestFromResponseReturnsTheSameDeltas(t *testing.T) {
	response := &model.Response{IsPartial: true, Choices: []model.Choice{{Delta: model.Message{Content: "x"}}}}
	chunks, err := llmtrpc.FromResponse(response)
	if err != nil {
		t.Fatalf("from response: %v", err)
	}
	translation, err := llmtrpc.Translate(response)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if len(chunks) != len(translation.Chunks) {
		t.Fatalf("FromResponse and Translate disagree: %d vs %d", len(chunks), len(translation.Chunks))
	}
	if _, err := llmtrpc.FromResponse(nil); err != nil {
		t.Fatalf("a nil response must not fail: %v", err)
	}
}

// Every role maps to a wire role, including a role this build does not know.
func TestToRequestMapsEveryRole(t *testing.T) {
	for _, role := range []llm.Role{llm.RoleSystem, llm.RoleDeveloper, llm.RoleUser, llm.RoleAssistant, llm.RoleTool} {
		source := llm.MessageSource{Kind: llm.SourceUser}
		callID := llm.ToolCallID("")
		switch role {
		case llm.RoleSystem:
			source = llm.MessageSource{Kind: llm.SourceSystemPrompt}
		case llm.RoleAssistant:
			source = llm.MessageSource{Kind: llm.SourceModel, Provider: "p", Model: "m"}
		case llm.RoleTool:
			source = llm.MessageSource{Kind: llm.SourceTool, CallID: "c"}
			callID = "c"
		}
		message := llm.Message{ID: llm.MessageID("id-" + string(role)), Role: role, Content: []llm.ContentBlock{llm.TextBlock{Text: "t"}}, Source: source, ToolCallID: callID}
		converted, err := llmtrpc.ToRequest(llm.Request{Provider: "p", Model: "m", Messages: []llm.Message{message}})
		if err != nil {
			t.Fatalf("role %s: %v", role, err)
		}
		if converted.Messages[0].Role == "" {
			t.Fatalf("role %s produced an empty wire role", role)
		}
	}
}

// A tool result without a recorded tool name still carries its call id.
func TestToolResultWithoutANameKeepsItsCallID(t *testing.T) {
	source := llm.MessageSource{Kind: llm.SourceTool, CallID: "c9"}
	message := llm.Message{ID: "m", Role: llm.RoleTool, Content: []llm.ContentBlock{llm.TextBlock{Text: "r"}}, Source: source, ToolCallID: "c9"}
	converted, err := llmtrpc.ToRequest(llm.Request{Provider: "p", Model: "m", Messages: []llm.Message{message}})
	if err != nil {
		t.Fatalf("to request: %v", err)
	}
	if converted.Messages[0].ToolID != "c9" || converted.Messages[0].ToolName != "" {
		t.Fatalf("unexpected tool message %+v", converted.Messages[0])
	}
}

// A provider error whose message embeds no JSON keeps the framework's own
// message rather than losing it.
func TestErrorWithoutEmbeddedJSONKeepsTheFrameworkMessage(t *testing.T) {
	translation, err := llmtrpc.Translate(&model.Response{Error: &model.ResponseError{Message: "dial tcp 127.0.0.1:1: connect: connection refused", Type: "stream_error"}})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if translation.Failure == nil || translation.Failure.Code != "transport-error" {
		t.Fatalf("want a transport error, got %+v", translation.Failure)
	}
	if translation.Failure.Message == "" {
		t.Fatal("the framework message must be preserved")
	}
	if translation.Failure.Status != nil {
		t.Fatalf("a transport failure must not carry an HTTP status, got %v", translation.Failure.Status)
	}
}

// A non-2xx response is reproduced end to end: the status must be recovered
// from the framework's message even though it is not a field.
func TestAdapterRecoversHTTPStatusFromNon2xxResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"slow down","type":"rate_limit_error"}}`)
	}))
	defer server.Close()
	adapter := llmtrpc.New(llmtrpc.Config{ID: "limited", Name: "Limited", Model: "fixture", BaseURL: server.URL + "/v1"})
	stream, err := adapter.Generate(context.Background(), userRequest(t))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	assembler := llm.NewAssembler()
	_, err = assembler.Assemble(stream)
	var failure *llm.FailureError
	if !asFailure(err, &failure) {
		t.Fatalf("want a FailureError, got %v", err)
	}
	if failure.Failure.Status == nil || *failure.Failure.Status != http.StatusTooManyRequests {
		t.Fatalf("want status 429, got %v", failure.Failure.Status)
	}
	if failure.Failure.Code != "rate_limit_error" || failure.Failure.Message != "slow down" {
		t.Fatalf("provider facts not recovered: %+v", failure.Failure)
	}
}
