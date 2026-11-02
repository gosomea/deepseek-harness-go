package llm_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// A request is the value every provider receives, so it must refuse the shapes
// that would fail later, at the provider, as an opaque transport error.
func TestRequestValidateAcceptsAWellFormedRequest(t *testing.T) {
	request := userRequest(t, "fake")
	if err := request.Validate(); err != nil {
		t.Fatalf("a well-formed request must validate: %v", err)
	}
}

func TestRequestValidateRefusesMissingRouteAndEmptyHistory(t *testing.T) {
	message, err := llm.NewUserMessage("hi")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	if err := (llm.Request{Model: "m", Messages: []llm.Message{message}}).Validate(); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a provider, got %v", err)
	}
	if err := (llm.Request{Provider: "p", Messages: []llm.Message{message}}).Validate(); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a model, got %v", err)
	}
	if err := (llm.Request{Provider: "p", Model: "m"}).Validate(); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without messages, got %v", err)
	}
}

// A malformed message must be caught here rather than at the provider, where it
// would surface as a transport failure and hide the caller's bug.
func TestRequestValidateRefusesAMalformedMessage(t *testing.T) {
	request := userRequest(t, "fake")
	request.Messages = append(request.Messages, llm.Message{ID: "x", Role: llm.RoleAssistant})
	err := request.Validate()
	if !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}
}
