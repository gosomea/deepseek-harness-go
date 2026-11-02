package llm

import "fmt"

// LlmFailure describes a provider or transport failure in provider-neutral
// terms. It is a value rather than an error so that it can be stored in a
// session log, compared between attempts and replayed: the policy layer, not
// the transport, decides whether a failure is retryable.
type LlmFailure struct {
	// Message is the human-readable failure description.
	Message string `json:"message"`
	// Code is the stable machine-routing code, for example "rate-limited".
	Code string `json:"code"`
	// Status is the HTTP status returned by the provider, when available.
	Status *int `json:"status,omitempty"`
	// ProviderRetryAfterMs is the provider's requested delay, when valid.
	ProviderRetryAfterMs *int `json:"providerRetryAfterMs,omitempty"`
	// RequestID is the provider-issued request identifier for diagnostics.
	RequestID string `json:"requestId,omitempty"`
}

// Validate reports whether the failure carries the two facts every consumer
// needs. A failure without a code cannot be routed by policy, and one without a
// message cannot be shown; both are refused here rather than at display time.
func (f LlmFailure) Validate() error {
	if f.Message == "" {
		return fmt.Errorf("%w: failure requires a message", ErrInvalidValue)
	}
	if f.Code == "" {
		return fmt.Errorf("%w: failure requires a code", ErrInvalidValue)
	}
	return nil
}

// Error lets a failure be returned where a plain error is expected, so callers
// have one representation instead of two.
func (f LlmFailure) Error() string {
	if f.Code == "" {
		return "llm: " + f.Message
	}
	return "llm: " + f.Code + ": " + f.Message
}

// FailureError wraps a failure as an error that errors.As can recover.
type FailureError struct {
	// Failure is the described provider or transport failure.
	Failure LlmFailure
}

// Error implements the error interface. It uses a value receiver so that both
// FailureError and *FailureError satisfy error: a caller that builds the value
// directly should not have to take its address to return it.
func (e FailureError) Error() string { return e.Failure.Error() }

// Unwrap exposes the failure for errors.As.
func (e FailureError) Unwrap() error { return e.Failure }
