package llm

import (
	"fmt"
	"strings"
)

// Role is a message's position in the conversation. The set is closed: every
// model-visible role needs a durable stored form and a provider projection, so
// adding one is a deliberate vocabulary change rather than an extension point.
type Role string

// The conversation roles this package understands.
const (
	// RoleSystem is a rendered system prompt.
	RoleSystem Role = "system"
	// RoleDeveloper carries incremental changes to the agent's instructions.
	RoleDeveloper Role = "developer"
	// RoleUser is a user-side message.
	RoleUser Role = "user"
	// RoleAssistant is a model-produced message.
	RoleAssistant Role = "assistant"
	// RoleTool carries the result of one tool invocation.
	RoleTool Role = "tool"
)

// SourceKind says who produced a message. It answers "where did this come
// from", which is independent of the role: several producers can share one
// role, and one producer may emit more than one kind over a conversation.
type SourceKind string

// The message source kinds this package understands.
const (
	// SourceUser is a direct human prompt or a producer-attributed
	// user-role injection.
	SourceUser SourceKind = "user"
	// SourceModel is a routed model response.
	SourceModel SourceKind = "model"
	// SourceTool is a tool result.
	SourceTool SourceKind = "tool"
	// SourceSystemPrompt is the system-prompt producer.
	SourceSystemPrompt SourceKind = "system-prompt"
)

// MessageSource is producer attribution.
//
// It is a struct with a discriminator rather than one type per kind because a
// consumer must be able to read a source it does not recognize: an unknown kind
// keeps its discriminator and its extra fields instead of being dropped. The
// reference treats a foreign source kind the same way.
type MessageSource struct {
	// Kind is the producer attribution.
	Kind SourceKind `json:"kind"`
	// Provider is the route that produced a model message. It is empty for
	// other kinds.
	Provider string `json:"provider,omitempty"`
	// Model is the model id that produced a model message. It is empty for
	// other kinds.
	Model string `json:"model,omitempty"`
	// CallID is the tool call a tool message answers. It duplicates the message's
	// ToolCallID on purpose: the durable form records the link in both places so
	// either reader finds it, and validation requires them to agree.
	CallID ToolCallID `json:"callId,omitempty"`
	// Extra carries the discriminator's unrecognized fields verbatim, so a
	// producer's own source shape survives a round trip.
	Extra map[string]any `json:"-"`
}

// Validate reports whether the source is complete for its kind.
func (s MessageSource) Validate() error {
	switch s.Kind {
	case SourceUser, SourceSystemPrompt:
		return nil
	case SourceModel:
		if strings.TrimSpace(s.Provider) == "" {
			return fmt.Errorf("%w: model source requires a provider", ErrInvalidValue)
		}
		if strings.TrimSpace(s.Model) == "" {
			return fmt.Errorf("%w: model source requires a model", ErrInvalidValue)
		}
		return nil
	case SourceTool:
		if s.CallID.IsZero() {
			return fmt.Errorf("%w: tool source requires a callId", ErrInvalidValue)
		}
		return nil
	case "":
		return fmt.Errorf("%w: source requires a kind", ErrInvalidValue)
	default:
		// A foreign kind is a producer's own vocabulary. It is preserved rather
		// than rejected, so a reader never has to understand every producer.
		return nil
	}
}

// Usage reports token accounting for one model response.
//
// The counts are disjoint: InputTokens counts uncached input only, and the
// cached counters are reported separately, so a caller that wants the billed
// input total adds them. A value that reports zero input and zero output is a
// reported zero, not a missing value; absence is the nil pointer.
type Usage struct {
	// InputTokens is the uncached input token count.
	InputTokens int `json:"inputTokens"`
	// OutputTokens is the output token count.
	OutputTokens int `json:"outputTokens"`
	// TotalTokens is the provider's full-call total, when disclosed.
	TotalTokens *int `json:"totalTokens,omitempty"`
	// CacheReadTokens is the cache-read token count, when disclosed.
	CacheReadTokens *int `json:"cacheReadTokens,omitempty"`
	// CacheWriteTokens is the cache-write token count, when disclosed.
	CacheWriteTokens *int `json:"cacheWriteTokens,omitempty"`
	// ReasoningTokens is the reasoning token count, when disclosed.
	ReasoningTokens *int `json:"reasoningTokens,omitempty"`
}

// Validate reports whether the counts are a representable value. It does not
// range-check them: the reference accepts negative or inconsistent accounting at
// its storage boundary, and refusing it here would break a round trip. Reported
// absence stays the nil pointer, which is how "not disclosed" is distinguished
// from a reported zero.
func (u Usage) Validate() error { return nil }

// Message is one conversation message.
//
// It is a struct with a role discriminator rather than one interface per role
// because the durable form must be a single recognized shape: a reader that does
// not know a role still has to preserve it, and a decoder has to reject a
// message missing the fields its role requires. ToolCallID and IsError are only
// meaningful for a tool message; Source.Kind decides which optional blocks
// apply.
type Message struct {
	// ID is the message's stable identity, preserved across every
	// representation boundary.
	ID MessageID `json:"id"`
	// Role is the message's conversation position.
	Role Role `json:"role"`
	// Content is the exact model-facing content, in order.
	Content []ContentBlock `json:"content"`
	// Source is the producer attribution.
	Source MessageSource `json:"source"`
	// ToolCallID is the call a tool message answers. It must equal
	// Source.CallID when either is set.
	ToolCallID ToolCallID `json:"toolCallId,omitempty"`
	// IsError marks a failed tool invocation so a consumer can present it
	// without inspecting the content.
	IsError bool `json:"isError,omitempty"`
}

// Validate reports whether the message is a complete, self-consistent value.
//
// The rules here are the ones a later subsystem relies on, and they are exactly
// the ones the reference enforces when a session event enters its log: a message
// must carry an identifier, a role this package understands, a source matching
// that role (a model route for an assistant message, a system-prompt attribution
// for a system message, a call id for a tool message), and a tool message's call
// identifier must agree in both places it is recorded. Optional fields a role
// does not use are tolerated rather than rejected, because the reference stores
// them and a codec must round-trip what it accepts.
func (m Message) Validate() error {
	if strings.TrimSpace(string(m.ID)) == "" {
		return fmt.Errorf("%w: message requires an id", ErrInvalidValue)
	}
	switch m.Role {
	case RoleSystem:
		if m.Source.Kind != SourceSystemPrompt {
			return fmt.Errorf("%w: system message requires a system-prompt source, got %q", ErrInvalidValue, m.Source.Kind)
		}
	case RoleDeveloper:
		if m.Source.Kind == "" {
			return fmt.Errorf("%w: developer message requires a source kind", ErrInvalidValue)
		}
	case RoleUser:
		if m.Source.Kind == "" {
			return fmt.Errorf("%w: user message requires a source kind", ErrInvalidValue)
		}
	case RoleAssistant:
		if m.Source.Kind != SourceModel {
			return fmt.Errorf("%w: assistant message requires a model source, got %q", ErrInvalidValue, m.Source.Kind)
		}
	case RoleTool:
		if m.ToolCallID.IsZero() {
			return fmt.Errorf("%w: tool message requires a toolCallId", ErrInvalidValue)
		}
		if m.Source.Kind != SourceTool {
			return fmt.Errorf("%w: tool message requires a tool source, got %q", ErrInvalidValue, m.Source.Kind)
		}
		if m.Source.CallID != m.ToolCallID {
			return fmt.Errorf("%w: tool message source callId %q does not match toolCallId %q",
				ErrInvalidValue, m.Source.CallID, m.ToolCallID)
		}
	case "":
		return fmt.Errorf("%w: message requires a role", ErrInvalidValue)
	default:
		return fmt.Errorf("%w: unknown message role %q", ErrInvalidValue, m.Role)
	}
	if err := m.Source.Validate(); err != nil {
		return err
	}
	return ValidateContent(m.Content)
}

// Empty reports whether the message carries no content. An empty system,
// developer or assistant message keeps its place in the log but derives to no
// model-visible message, so callers ask this instead of guessing.
func (m Message) Empty() bool { return len(m.Content) == 0 }

// NewMessage builds a validated message, minting an identifier when the caller
// does not supply one.
func NewMessage(role Role, content []ContentBlock, source MessageSource) (Message, error) {
	id, err := NewMessageID()
	if err != nil {
		return Message{}, err
	}
	message := Message{ID: id, Role: role, Content: content, Source: source}
	if role == RoleTool && !source.CallID.IsZero() {
		message.ToolCallID = source.CallID
	}
	if err := message.Validate(); err != nil {
		return Message{}, err
	}
	return message, nil
}

// NewUserMessage builds a user message from text.
func NewUserMessage(text string) (Message, error) {
	return NewMessage(RoleUser, TextContent(text), MessageSource{Kind: SourceUser})
}

// NewSystemMessage builds a system message from rendered prompt text. Empty text
// produces a message with no content, which records "no system prompt" rather
// than an empty one.
func NewSystemMessage(text string) (Message, error) {
	return NewMessage(RoleSystem, TextContent(text), MessageSource{Kind: SourceSystemPrompt})
}

// NewAssistantMessage builds an assistant message for the named route.
func NewAssistantMessage(provider, model string, content []ContentBlock) (Message, error) {
	return NewMessage(RoleAssistant, content, MessageSource{Kind: SourceModel, Provider: provider, Model: model})
}

// NewToolResultMessage builds the tool message that answers one call.
//
// The call identifier is recorded in both the source and the message's own
// field, and Validate rejects a construction where the two disagree.
func NewToolResultMessage(callID ToolCallID, content []ContentBlock, isError bool) (Message, error) {
	message, err := NewMessage(RoleTool, content, MessageSource{Kind: SourceTool, CallID: callID})
	if err != nil {
		return Message{}, err
	}
	message.IsError = isError
	return message, nil
}
