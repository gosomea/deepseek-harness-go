package llm

import (
	"fmt"
	"strings"
)

// Request is one provider-neutral model request.
//
// It is the value every provider adapter receives, which is why it holds no
// transport, retry or credential field: those belong to the adapter that knows
// the provider's dialect. A request is derived from the session surface, so the
// same logged history always produces the same request.
type Request struct {
	// Provider is the route key the caller selected, for example "openai".
	Provider string `json:"provider"`
	// Model is the provider model identifier.
	Model string `json:"model"`
	// Messages is the model-visible conversation in order. It must be non-empty:
	// a request with no message has nothing to send and is a caller bug rather
	// than a provider condition.
	Messages []Message `json:"messages"`
	// MaxOutputTokens caps the generated tokens when the caller sets it. Absence
	// means the route's own default, which is not the same as a zero cap.
	MaxOutputTokens *int `json:"maxOutputTokens,omitempty"`
	// Temperature overrides the route sampling temperature when set.
	Temperature *float64 `json:"temperature,omitempty"`
	// ReasoningEffort selects a reasoning effort the route declares.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// Validate reports whether the request can be handed to an adapter.
//
// Every message must be valid on its own, because a request that reaches a
// provider with a malformed message would fail there with a transport error,
// hiding a caller bug behind a provider failure.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Provider) == "" {
		return fmt.Errorf("%w: request requires a provider", ErrInvalidValue)
	}
	if strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("%w: request requires a model", ErrInvalidValue)
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("%w: request requires at least one message", ErrInvalidValue)
	}
	for index, message := range r.Messages {
		if err := message.Validate(); err != nil {
			return fmt.Errorf("message %d: %w", index, err)
		}
	}
	return nil
}

// Clone returns a deep copy of the request.
//
// The copy exists so that a provider that mutates the request it was given
// cannot change the caller's history: the message slice and each message's
// content slice are freshly allocated.
func (r Request) Clone() Request {
	clone := r
	if r.Messages != nil {
		clone.Messages = make([]Message, len(r.Messages))
		for index, message := range r.Messages {
			copied := message
			if message.Content != nil {
				copied.Content = append([]ContentBlock(nil), message.Content...)
			}
			clone.Messages[index] = copied
		}
	}
	if r.MaxOutputTokens != nil {
		value := *r.MaxOutputTokens
		clone.MaxOutputTokens = &value
	}
	if r.Temperature != nil {
		value := *r.Temperature
		clone.Temperature = &value
	}
	return clone
}
