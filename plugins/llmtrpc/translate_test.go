package llmtrpc_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/plugins/llmtrpc"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func buildMessage(t *testing.T, role llm.Role, content []llm.ContentBlock, source llm.MessageSource) llm.Message {
	t.Helper()
	message, err := llm.NewMessage(role, content, source)
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	return message
}

// Every role must arrive on the wire with its meaning intact: a developer
// instruction sent as user content would change what the model treats as an
// instruction, and a tool result that lost its call id would be attributed to
// the wrong call.
func TestToRequestPreservesRoleContentAndToolIdentity(t *testing.T) {
	user := buildMessage(t, llm.RoleUser, []llm.ContentBlock{llm.TextBlock{Text: "ask"}}, llm.MessageSource{Kind: llm.SourceUser})
	developer := buildMessage(t, llm.RoleDeveloper, []llm.ContentBlock{llm.TextBlock{Text: "rule"}}, llm.MessageSource{Kind: llm.SourceUser})
	assistant := buildMessage(t, llm.RoleAssistant, []llm.ContentBlock{
		llm.ReasoningBlock{Text: "thought"},
		llm.TextBlock{Text: "answer"},
		llm.ToolCallBlock{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.txt"}`},
	}, llm.MessageSource{Kind: llm.SourceModel, Provider: "p", Model: "m"})
	toolSource := llm.MessageSource{Kind: llm.SourceTool, CallID: "call-1", Extra: map[string]any{"toolName": "read_file"}}
	tool, err := llm.NewMessage(llm.RoleTool, []llm.ContentBlock{llm.TextBlock{Text: "contents"}}, toolSource)
	if err != nil {
		t.Fatalf("build tool message: %v", err)
	}
	limit := 128
	temperature := 0.25
	request := llm.Request{
		Provider:        "p",
		Model:           "m",
		Messages:        []llm.Message{user, developer, assistant, tool},
		MaxOutputTokens: &limit,
		Temperature:     &temperature,
		ReasoningEffort: "high",
	}
	converted, err := llmtrpc.ToRequest(request)
	if err != nil {
		t.Fatalf("to request: %v", err)
	}
	if !converted.GenerationConfig.Stream {
		t.Fatal("the adapter streams: the config must request it")
	}
	if converted.GenerationConfig.MaxTokens == nil || *converted.GenerationConfig.MaxTokens != 128 {
		t.Fatalf("token cap lost: %v", converted.GenerationConfig.MaxTokens)
	}
	if converted.GenerationConfig.Temperature == nil || *converted.GenerationConfig.Temperature != 0.25 {
		t.Fatalf("temperature lost: %v", converted.GenerationConfig.Temperature)
	}
	if converted.GenerationConfig.ReasoningEffort == nil || *converted.GenerationConfig.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort lost: %v", converted.GenerationConfig.ReasoningEffort)
	}
	if len(converted.Messages) != 4 {
		t.Fatalf("want 4 messages, got %d", len(converted.Messages))
	}
	if converted.Messages[0].Role != model.RoleUser || converted.Messages[0].Content != "ask" {
		t.Fatalf("user message altered: %+v", converted.Messages[0])
	}
	// The framework has no developer role; instructions become a system message
	// rather than user content.
	if converted.Messages[1].Role != model.RoleSystem || converted.Messages[1].Content != "rule" {
		t.Fatalf("developer message must become a system message: %+v", converted.Messages[1])
	}
	if converted.Messages[2].Role != model.RoleAssistant || converted.Messages[2].Content != "answer" || converted.Messages[2].ReasoningContent != "thought" {
		t.Fatalf("assistant message altered: %+v", converted.Messages[2])
	}
	if len(converted.Messages[2].ToolCalls) != 1 {
		t.Fatalf("tool calls lost: %+v", converted.Messages[2])
	}
	call := converted.Messages[2].ToolCalls[0]
	if call.ID != "call-1" || call.Function.Name != "read_file" || string(call.Function.Arguments) != `{"path":"a.txt"}` {
		t.Fatalf("tool call altered: %+v", call)
	}
	if converted.Messages[3].Role != model.RoleTool || converted.Messages[3].ToolID != "call-1" || converted.Messages[3].ToolName != "read_file" {
		t.Fatalf("tool result identity lost: %+v", converted.Messages[3])
	}
}

// A block this adapter cannot express must fail loudly and name the block,
// because dropping it would let the caller believe the model saw it.
func TestToRequestRefusesUnrepresentableBlocks(t *testing.T) {
	cases := []struct {
		name  string
		block llm.ContentBlock
	}{
		{"image", llm.ImageBlock{Attachment: llm.ImageAttachmentRef{AttachmentID: "sha256:x", MediaType: llm.MediaTypePNG}}},
		{"file", llm.FileBlock{Attachment: llm.FileAttachmentRef{AttachmentID: "sha256:y", Name: "a.txt"}}},
		{"tool addition", llm.ToolAdditionBlock{ToolName: "t"}},
		{"tool removal", llm.ToolRemovalBlock{ToolName: "t"}},
		{"unknown", llm.UnknownBlock{Type: "audio", Raw: []byte(`{"type":"audio"}`)}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			message := buildMessage(t, llm.RoleUser, []llm.ContentBlock{testCase.block}, llm.MessageSource{Kind: llm.SourceUser})
			_, err := llmtrpc.ToRequest(llm.Request{Provider: "p", Model: "m", Messages: []llm.Message{message}})
			var unsupported *llmtrpc.ErrUnsupported
			if !errors.As(err, &unsupported) {
				t.Fatalf("want ErrUnsupported, got %v", err)
			}
			if !strings.Contains(unsupported.Error(), string(testCase.block.BlockType())) {
				t.Fatalf("the error must name the block: %v", unsupported)
			}
		})
	}
}

func TestToRequestRefusesAnInvalidRequest(t *testing.T) {
	if _, err := llmtrpc.ToRequest(llm.Request{Model: "m"}); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a request without a provider, got %v", err)
	}
}

// Translate must order usage before the terminal finish and mark the response
// that ends the stream, because the framework signals completion by clearing
// IsPartial rather than by setting Done.
func TestTranslateReportsFinalAndUsageSeparately(t *testing.T) {
	finish := "stop"
	response := &model.Response{
		IsPartial: false,
		Choices: []model.Choice{{
			Delta:        model.Message{Content: "done", ReasoningContent: "why"},
			FinishReason: &finish,
		}},
		Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
	}
	translation, err := llmtrpc.Translate(response)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if !translation.Final {
		t.Fatal("a response with IsPartial cleared must be reported as final")
	}
	if translation.Finish == nil || translation.Finish.Kind != llm.FinishStop {
		t.Fatalf("unexpected finish %+v", translation.Finish)
	}
	if translation.Usage == nil || translation.Usage.InputTokens != 5 {
		t.Fatalf("usage lost: %+v", translation.Usage)
	}
	if len(translation.Chunks) != 2 {
		t.Fatalf("want reasoning then text, got %+v", translation.Chunks)
	}
	if translation.Chunks[0].Index == translation.Chunks[1].Index {
		t.Fatal("reasoning and text must not share a block index, or they would merge")
	}
}

// An unrecognized stop reason must not look like a clean completion.
func TestTranslateMapsUnrecognizedFinishToAFailure(t *testing.T) {
	odd := "provider_quirk"
	response := &model.Response{IsPartial: false, Choices: []model.Choice{{FinishReason: &odd}}}
	translation, err := llmtrpc.Translate(response)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if translation.Finish == nil || translation.Finish.Kind != llm.FinishError {
		t.Fatalf("want a failure finish, got %+v", translation.Finish)
	}
	if translation.Finish.Failure.Code != "unrecognized-finish" {
		t.Fatalf("unexpected code %q", translation.Finish.Failure.Code)
	}
	// The content filter is a failure too, and must be named.
	filtered := "content_filter"
	translation, err = llmtrpc.Translate(&model.Response{IsPartial: false, Choices: []model.Choice{{FinishReason: &filtered}}})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if translation.Finish.Failure.Code != "content-filter" {
		t.Fatalf("unexpected content-filter code %q", translation.Finish.Failure.Code)
	}
	// The max-token and tool-call reasons are normal terminations.
	for reason, kind := range map[string]llm.FinishKind{"length": llm.FinishMaxTokens, "function_call": llm.FinishToolCalls} {
		value := reason
		translation, err := llmtrpc.Translate(&model.Response{IsPartial: false, Choices: []model.Choice{{FinishReason: &value}}})
		if err != nil {
			t.Fatalf("translate %s: %v", reason, err)
		}
		if translation.Finish.Kind != kind {
			t.Fatalf("want %s, got %s", kind, translation.Finish.Kind)
		}
	}
}

func TestTranslateHandlesNilAndErrorResponses(t *testing.T) {
	if translation, err := llmtrpc.Translate(nil); err != nil || translation.Final || len(translation.Chunks) != 0 {
		t.Fatalf("a nil response must translate to nothing, got %+v err=%v", translation, err)
	}
	code := "quota"
	translation, err := llmtrpc.Translate(&model.Response{Error: &model.ResponseError{
		Message: "http://example/v1/chat/completions: 400 Bad Request",
		Type:    "stream_error",
		Code:    &code,
	}})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if translation.Failure == nil {
		t.Fatal("an error response must become a failure")
	}
	if translation.Failure.Status == nil || *translation.Failure.Status != 400 {
		t.Fatalf("the HTTP status must be recovered, got %v", translation.Failure.Status)
	}
	if translation.Failure.RequestID != "quota" {
		t.Fatalf("the provider code must be preserved, got %q", translation.Failure.RequestID)
	}
}

func TestMarshalRequestRendersJSON(t *testing.T) {
	converted, err := llmtrpc.ToRequest(llm.Request{
		Provider: "p",
		Model:    "m",
		Messages: []llm.Message{buildMessage(t, llm.RoleUser, []llm.ContentBlock{llm.TextBlock{Text: "hi"}}, llm.MessageSource{Kind: llm.SourceUser})},
	})
	if err != nil {
		t.Fatalf("to request: %v", err)
	}
	encoded, err := llmtrpc.MarshalRequest(converted)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"messages"`) {
		t.Fatalf("unexpected rendering %s", encoded)
	}
}
