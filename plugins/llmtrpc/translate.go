package llmtrpc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gosomea/deepseek-harness-go/llm"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ErrUnsupported reports a value this adapter cannot express on the wire.
//
// It exists because silently dropping a field would let a caller believe a
// capability was requested when the provider never saw it. Every unsupported
// input is named instead.
type ErrUnsupported struct {
	// What names the unsupported value, for example "content block image".
	What string
	// Detail explains why it cannot be represented.
	Detail string
}

// Error implements the error interface.
func (e *ErrUnsupported) Error() string {
	if e.Detail == "" {
		return "llmtrpc: unsupported " + e.What
	}
	return "llmtrpc: unsupported " + e.What + ": " + e.Detail
}

// ToRequest converts one harness request into a framework request.
//
// Every message is translated by its role, and any content block this adapter
// cannot express returns an error naming the block rather than being dropped.
// Tool calls and tool results are preserved with their identifiers, because a
// result whose call id was lost would be attributed to the wrong call.
func ToRequest(request llm.Request) (*model.Request, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	converted := &model.Request{
		Messages: make([]model.Message, 0, len(request.Messages)),
	}
	converted.GenerationConfig.Stream = true
	if request.MaxOutputTokens != nil {
		value := *request.MaxOutputTokens
		converted.GenerationConfig.MaxTokens = &value
	}
	if request.Temperature != nil {
		value := *request.Temperature
		converted.GenerationConfig.Temperature = &value
	}
	if request.ReasoningEffort != "" {
		value := request.ReasoningEffort
		converted.GenerationConfig.ReasoningEffort = &value
	}
	for index, message := range request.Messages {
		convertedMessage, err := toMessage(message)
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", index, err)
		}
		converted.Messages = append(converted.Messages, convertedMessage)
	}
	return converted, nil
}

// toMessage converts one message, preserving role, content and tool identity.
func toMessage(message llm.Message) (model.Message, error) {
	converted := model.Message{Role: toRole(message.Role)}
	var textParts []string
	var reasoningParts []string
	var toolCalls []model.ToolCall
	for _, block := range message.Content {
		switch typed := block.(type) {
		case llm.TextBlock:
			textParts = append(textParts, typed.Text)
		case llm.ReasoningBlock:
			reasoningParts = append(reasoningParts, typed.Text)
		case llm.ToolCallBlock:
			toolCalls = append(toolCalls, toToolCall(typed))
		case llm.ImageBlock, llm.FileBlock:
			// Attachments are projected to placeholder text by the context
			// layer (M8); the model layer never receives raw bytes, so sending
			// one here would silently change what the model sees.
			return model.Message{}, &ErrUnsupported{
				What:   "content block " + string(block.BlockType()),
				Detail: "attachments are projected to text before a request is built",
			}
		case llm.ToolAdditionBlock, llm.ToolRemovalBlock:
			return model.Message{}, &ErrUnsupported{
				What:   "content block " + string(block.BlockType()),
				Detail: "tool activation is expressed through the request's tool list",
			}
		case llm.UnknownBlock:
			return model.Message{}, &ErrUnsupported{
				What:   "content block " + string(typed.Type),
				Detail: "this build cannot express the block on the wire",
			}
		default:
			return model.Message{}, &ErrUnsupported{
				What:   fmt.Sprintf("content block %T", block),
				Detail: "this build cannot express the block on the wire",
			}
		}
	}
	converted.Content = strings.Join(textParts, "")
	converted.ReasoningContent = strings.Join(reasoningParts, "")
	converted.ToolCalls = toolCalls
	if message.Role == llm.RoleTool {
		converted.ToolID = message.ToolCallID.String()
		if name := toolNameForToolMessage(message); name != "" {
			converted.ToolName = name
		}
	}
	return converted, nil
}

// toolNameForToolMessage returns the tool name recorded in a tool result's
// source, when the producer set one.
func toolNameForToolMessage(message llm.Message) string {
	if name, ok := message.Source.Extra["toolName"].(string); ok {
		return name
	}
	return ""
}

// toToolCall converts one tool call, keeping the raw argument string.
//
// Arguments stay a byte slice of the original JSON: parsing and re-encoding
// them here would change whitespace and key order, and could quietly repair a
// malformed argument string the caller must see fail.
func toToolCall(block llm.ToolCallBlock) model.ToolCall {
	return model.ToolCall{
		Type: "function",
		ID:   block.ID.String(),
		Function: model.FunctionDefinitionParam{
			Name:      block.Name,
			Arguments: []byte(block.Arguments),
		},
	}
}

// toRole maps a harness role onto the framework's role vocabulary.
func toRole(role llm.Role) model.Role {
	switch role {
	case llm.RoleSystem:
		return model.RoleSystem
	case llm.RoleDeveloper, llm.RoleUser:
		// The framework has no developer role. A developer message carries
		// instructions, so it is sent as a system message rather than being
		// dropped or mislabelled as user content.
		if role == llm.RoleDeveloper {
			return model.RoleSystem
		}
		return model.RoleUser
	case llm.RoleAssistant:
		return model.RoleAssistant
	case llm.RoleTool:
		return model.RoleTool
	default:
		return model.RoleUser
	}
}

// Block indexes this adapter assigns to the block kinds of one attempt.
//
// They are fixed rather than derived from arrival order because reasons, text
// and tool calls may interleave: a single running counter would let a reasoning
// fragment and a text fragment land on the same index, and the assembler would
// merge them into one block. Tool calls start after the two fixed kinds and
// follow the framework's own tool-call index, so every fragment of one call
// shares an index and two calls never do.
const (
	// reasoningIndex is the block index of the reasoning block.
	reasoningIndex = 0
	// textIndex is the block index of the visible text block.
	textIndex = 1
	// toolCallIndexBase is the first block index used by tool calls.
	toolCallIndexBase = 2
)

// Translation is the result of translating one framework response: the deltas
// it carried, and whether the provider signalled the end of the stream.
type Translation struct {
	// Chunks are the harness chunks for this response, in order.
	Chunks []llm.StreamChunk
	// Finish, when set, is the reason the provider reported for ending.
	Finish *llm.FinishReason
	// Final reports that this response was the last of the stream. The
	// framework marks it by clearing IsPartial, not by setting Done: Done stays
	// false for a complete stream, so treating it as the terminal signal would
	// make every attempt look cut off.
	Final bool
	// Usage is the reported accounting, when the response carried any.
	Usage *llm.Usage
	// Failure, when set, is an error the provider reported inside the stream.
	Failure *llm.LlmFailure
}

// Translate converts one framework response into harness chunks.
//
// An error response becomes a Failure rather than an empty chunk list, because
// a failed attempt must not be mistaken for a successful empty answer. A
// response that ends the stream reports Final, so the caller can order usage
// before the terminal finish exactly once.
func Translate(response *model.Response) (Translation, error) {
	if response == nil {
		return Translation{}, nil
	}
	if response.Error != nil {
		failure := fromResponseError(response.Error)
		return Translation{Failure: &failure, Final: true}, nil
	}
	translation := Translation{Final: !response.IsPartial}
	for _, choice := range response.Choices {
		translation.Chunks = append(translation.Chunks, fromMessage(choice.Delta)...)
		if choice.FinishReason != nil && translation.Finish == nil {
			reason := finishReason(*choice.FinishReason)
			translation.Finish = &reason
		}
	}
	if response.Usage != nil {
		usage := fromUsage(response.Usage)
		translation.Usage = &usage
	}
	return translation, nil
}

// FromResponse converts one framework response into harness chunks, dropping
// the framing facts that Translate reports. It exists for callers that only
// need the deltas; the stream uses Translate so usage and the terminal finish
// are ordered correctly.
func FromResponse(response *model.Response) ([]llm.StreamChunk, error) {
	translation, err := Translate(response)
	if err != nil {
		return nil, err
	}
	return translation.Chunks, nil
}

// fromMessage converts the reasoning, text and tool-call content of one delta
// message into harness chunks at their fixed block indexes.
func fromMessage(message model.Message) []llm.StreamChunk {
	var chunks []llm.StreamChunk
	if message.ReasoningContent != "" {
		chunks = append(chunks, llm.StreamChunk{Type: llm.ChunkReasoningDelta, Index: reasoningIndex, Text: message.ReasoningContent})
	}
	if message.Content != "" {
		chunks = append(chunks, llm.StreamChunk{Type: llm.ChunkTextDelta, Index: textIndex, Text: message.Content})
	}
	for _, call := range message.ToolCalls {
		id := llm.ToolCallID(call.ID)
		name := call.Function.Name
		index := toolCallIndexBase
		if call.Index != nil && *call.Index >= 0 {
			index += *call.Index
		}
		chunk := llm.StreamChunk{
			Type:           llm.ChunkToolCallDelta,
			Index:          index,
			ID:             id,
			ArgumentsDelta: string(call.Function.Arguments),
		}
		if name != "" {
			chunk.Name = &name
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// fromUsage converts framework accounting into harness accounting.
//
// The framework reports a prompt total that folds cache hits in, while the
// harness vocabulary is disjoint: inputTokens counts uncached input only. The
// cached counts are therefore subtracted out, and an inconsistent negative
// result is clamped to zero rather than reported as a negative count.
func fromUsage(usage *model.Usage) llm.Usage {
	cached := usage.PromptTokensDetails.CachedTokens + usage.PromptTokensDetails.CacheCreationTokens + usage.PromptTokensDetails.CacheReadTokens
	uncached := usage.PromptTokens - cached
	if uncached < 0 {
		uncached = 0
	}
	converted := llm.Usage{
		InputTokens:  uncached,
		OutputTokens: usage.CompletionTokens,
	}
	total := usage.TotalTokens
	converted.TotalTokens = &total
	if cached > 0 {
		read := usage.PromptTokensDetails.CachedTokens + usage.PromptTokensDetails.CacheReadTokens
		converted.CacheReadTokens = &read
	}
	if usage.CompletionTokensDetails.ReasoningTokens > 0 {
		reasoning := usage.CompletionTokensDetails.ReasoningTokens
		converted.ReasoningTokens = &reasoning
	}
	return converted
}

// fromResponseError converts a framework API error into harness failure facts.
//
// The framework funnels both transport failures and provider API errors into
// one error type named "stream_error" and leaves Code empty, so the concrete
// kind must be recovered from the message: an HTTP status makes the failure
// classifiable, and a dial failure is named as a transport error instead of
// being reported as a provider rejection. This translation loss is recorded in
// the adapter's divergence list rather than hidden.
func fromResponseError(err *model.ResponseError) llm.LlmFailure {
	code := err.Type
	if code == "" {
		code = "api-error"
	}
	failure := llm.LlmFailure{Message: err.Message, Code: code}
	if err.Code != nil && *err.Code != "" {
		failure.RequestID = *err.Code
	}
	if status := httpStatus(err.Message); status != 0 {
		failure.Status = &status
		// The embedded provider error, when present, carries the provider's own
		// type and is a better routing key than the framework's generic name.
		applyEmbeddedProviderError(&failure, err.Message)
		if failure.Code == "stream_error" {
			failure.Code = "http-error"
		}
		return failure
	}
	if isTransportMessage(err.Message) {
		failure.Code = "transport-error"
		return failure
	}
	applyEmbeddedProviderError(&failure, err.Message)
	return failure
}

// applyEmbeddedProviderError recovers the provider's own error type and message
// from the JSON the framework embeds in its message.
//
// The framework collapses both transport and API failures into one type named
// "stream_error" with an empty Code, so without this recovery a caller could not
// distinguish a rate limit from a malformed request. Only the provider's own
// type and message are taken: the provider's "code" is not used because
// providers disagree on its type (a number in one, a string in another), and
// the HTTP status is recovered separately from the status line.
func applyEmbeddedProviderError(failure *llm.LlmFailure, message string) {
	start := strings.Index(message, "{")
	if start < 0 {
		return
	}
	var decoded struct {
		Message string          `json:"message"`
		Type    string          `json:"type"`
		Code    json.RawMessage `json:"code"`
	}
	if err := json.Unmarshal([]byte(message[start:]), &decoded); err != nil {
		// The embedded object may be followed by trailing text, so the
		// remaining brace-delimited suffix is tried before giving up.
		if end := strings.LastIndex(message, "}"); end > start {
			if err := json.Unmarshal([]byte(message[start:end+1]), &decoded); err != nil {
				return
			}
		} else {
			return
		}
	}
	if decoded.Type != "" {
		failure.Code = decoded.Type
	}
	if decoded.Message != "" {
		failure.Message = decoded.Message
	}
}

// httpStatus extracts an HTTP status code from a framework message, so a
// failure carries the provider's status even though the framework does not
// expose it as a field.
//
// A status is only credible in a message that names a request, and it is always
// a field that is exactly three digits. Both conditions matter: a dial error
// also contains digits (the port in "127.0.0.1:1"), and reading those as a
// status would misclassify an unreachable endpoint as an HTTP rejection.
func httpStatus(message string) int {
	if !strings.Contains(message, "http") && !strings.Contains(message, "HTTP") {
		return 0
	}
	for _, field := range strings.Fields(message) {
		if status, ok := parseStatus(field); ok {
			return status
		}
	}
	return 0
}

// parseStatus reports whether a field is exactly a three-digit HTTP status.
func parseStatus(field string) (int, bool) {
	if len(field) != 3 {
		return 0, false
	}
	for _, r := range field {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	status := int(field[0]-'0')*100 + int(field[1]-'0')*10 + int(field[2]-'0')
	if status < 100 || status > 599 {
		return 0, false
	}
	return status, true
}

// isTransportMessage reports whether a framework message describes a failure to
// reach the endpoint rather than a provider rejection.
func isTransportMessage(message string) bool {
	for _, needle := range []string{"dial tcp", "connect: ", "connection refused", "no such host", "i/o timeout", "EOF"} {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}

// finishReason maps the framework's stop reason onto harness finish kinds.
//
// An unrecognized reason is reported as a failure rather than as a stop: a
// provider that stopped for a reason this build cannot name must not look like
// a clean completion.
func finishReason(reason string) llm.FinishReason {
	switch reason {
	case "stop":
		return llm.FinishReason{Kind: llm.FinishStop}
	case "tool_calls", "function_call":
		return llm.FinishReason{Kind: llm.FinishToolCalls}
	case "length":
		return llm.FinishReason{Kind: llm.FinishMaxTokens}
	case "content_filter":
		return llm.FinishReason{Kind: llm.FinishError, Failure: llm.LlmFailure{
			Message: "the provider stopped the response with a content filter",
			Code:    "content-filter",
		}}
	default:
		return llm.FinishReason{Kind: llm.FinishError, Failure: llm.LlmFailure{
			Message: "the provider stopped with an unrecognized reason: " + reason,
			Code:    "unrecognized-finish",
		}}
	}
}

// MarshalRequest renders a framework request as JSON for a fixture or a
// diagnostic. It never runs in the request path.
func MarshalRequest(request *model.Request) ([]byte, error) {
	return json.Marshal(request)
}
