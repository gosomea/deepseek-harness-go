package llmtrpc

import (
	"context"
	"errors"
	"sync"

	"github.com/gosomea/deepseek-harness-go/llm"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// ErrClosed reports use of an adapter that was already closed.
var ErrClosed = errors.New("llmtrpc: adapter closed")

// Config describes one adapter route.
type Config struct {
	// ID is the route key callers select with Request.Provider.
	ID string
	// Name is the human-readable name for diagnostics.
	Name string
	// Model is the model identifier passed to the endpoint.
	Model string
	// BaseURL points at the endpoint. It is required, and in tests it points at
	// a local loopback server, so no credential or external network is needed.
	BaseURL string
	// APIKey is the credential. A local fixture endpoint accepts any value, and
	// the field being optional is what keeps the loopback test credential-free.
	APIKey string
	// Capabilities overrides the declared capability set. When empty, the
	// adapter declares the capabilities this translation layer actually keeps.
	Capabilities []llm.Capability
}

// Adapter implements llm.Provider on top of the framework's model layer.
//
// It is the only place in the project where framework types appear. The
// underlying model is created lazily so that a route with a bad endpoint fails
// at its first attempt rather than at construction, which is also what lets a
// test point the same adapter at a loopback server.
type Adapter struct {
	mu     sync.Mutex
	config Config
	model  model.Model
	closed bool
}

// New returns an adapter for one route.
func New(config Config) *Adapter {
	return &Adapter{config: config}
}

// Info describes the route and its declared capabilities.
//
// The declaration is deliberately narrow: this adapter preserves text,
// reasoning, tool calls and usage, and it refuses anything else rather than
// dropping it. Declaring a capability the translation drops would let a caller
// build a request the harness accepts and the model never sees.
func (a *Adapter) Info() llm.ProviderInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	capabilities := a.config.Capabilities
	if len(capabilities) == 0 {
		capabilities = []llm.Capability{
			llm.CapabilityStreaming,
			llm.CapabilityReasoning,
			llm.CapabilityToolCalls,
			llm.CapabilityUsage,
		}
	}
	return llm.ProviderInfo{
		ID:           a.config.ID,
		Name:         a.config.Name,
		Capabilities: append([]llm.Capability(nil), capabilities...),
	}
}

// Generate starts one attempt against the configured endpoint.
//
// A closed adapter refuses the attempt instead of racing shutdown, and a
// translation failure surfaces before any network call so an unrepresentable
// request never becomes a provider-side mystery.
func (a *Adapter) Generate(ctx context.Context, request llm.Request) (llm.Stream, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, ErrClosed
	}
	underlying, err := a.modelLocked()
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	converted, err := ToRequest(request)
	if err != nil {
		return nil, err
	}
	responses, err := underlying.GenerateContent(ctx, converted)
	if err != nil {
		return nil, err
	}
	return &stream{responses: responses, ctx: ctx}, nil
}

// modelLocked builds the underlying framework model once.
func (a *Adapter) modelLocked() (model.Model, error) {
	if a.model != nil {
		return a.model, nil
	}
	if a.config.ID == "" {
		return nil, errors.New("llmtrpc: route requires an id")
	}
	if a.config.BaseURL == "" {
		return nil, errors.New("llmtrpc: route requires a base URL")
	}
	name := a.config.Model
	if name == "" {
		name = a.config.ID
	}
	// Tool-call deltas are off by default in the framework, which would make
	// every tool invocation arrive as nothing at all; the harness needs the
	// fragments, so the option is enabled explicitly.
	options := []openai.Option{
		openai.WithBaseURL(a.config.BaseURL),
		openai.WithShowToolCallDelta(true),
	}
	if a.config.APIKey != "" {
		options = append(options, openai.WithAPIKey(a.config.APIKey))
	}
	// The framework's own retries stay off: attempt accounting belongs to the
	// harness, and a hidden retry would make one attempt look like several
	// requests the log never recorded.
	a.model = openai.New(name, options...)
	return a.model, nil
}

// Close releases the adapter and refuses later attempts. It is idempotent.
func (a *Adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	a.model = nil
	return nil
}

// Closed reports whether Close was called.
func (a *Adapter) Closed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed
}

// stream adapts the framework's response channel to the harness stream.
//
// Two framework behaviours shape it. First, the terminal signal is the channel
// closing after a response with IsPartial cleared, not the Done flag: Done stays
// false for a complete stream. Second, accounting and the terminal finish must
// be ordered so that usage precedes exactly one finish chunk. The adapter
// therefore queues usage and the finish, and emits the finish only once, at the
// end: a provider that ended without reporting a stop reason is reported as an
// incomplete attempt rather than a successful empty answer.
type stream struct {
	responses <-chan *model.Response
	ctx       context.Context
	pending   []llm.StreamChunk
	// finish is the reason the terminal chunk will carry, once known.
	finish *llm.FinishReason
	// ended records that the terminal chunk was already handed to the caller.
	ended bool
	// closed records that Close was called.
	closed bool
}

// Recv returns the next available chunk.
func (s *stream) Recv() (llm.StreamChunk, bool, error) {
	for {
		if len(s.pending) > 0 {
			chunk := s.pending[0]
			s.pending = s.pending[1:]
			if chunk.IsTerminal() {
				s.ended = true
			}
			return chunk, true, nil
		}
		if s.ended {
			return llm.StreamChunk{}, false, nil
		}
		select {
		case <-s.ctx.Done():
			s.ended = true
			return llm.StreamChunk{}, false, s.ctx.Err()
		case response, ok := <-s.responses:
			if !ok {
				return s.terminate()
			}
			translation, err := Translate(response)
			if err != nil {
				s.ended = true
				return llm.StreamChunk{}, false, err
			}
			if translation.Failure != nil {
				failure := *translation.Failure
				reason := llm.FinishReason{Kind: llm.FinishError, Failure: failure}
				s.pending = nil
				s.finish = &reason
				s.ended = true
				return llm.StreamChunk{Type: llm.ChunkFinish, Reason: &reason}, true, nil
			}
			s.pending = append(s.pending, translation.Chunks...)
			if translation.Usage != nil {
				usage := *translation.Usage
				s.pending = append(s.pending, llm.StreamChunk{Type: llm.ChunkUsage, Usage: &usage})
			}
			if translation.Finish != nil {
				reason := *translation.Finish
				s.finish = &reason
			}
			// A final response closes the stream: the queued deltas, usage and
			// finish are delivered before Recv reports exhaustion.
			if translation.Final {
				s.ended = true
				s.pending = append(s.pending, s.terminalFinish())
			}
		}
	}
}

// terminate ends the stream when the channel closes, emitting the terminal
// finish if the provider reported one.
func (s *stream) terminate() (llm.StreamChunk, bool, error) {
	s.ended = true
	if s.finish == nil {
		return llm.StreamChunk{}, false, nil
	}
	chunk := s.terminalFinish()
	s.finish = nil
	return chunk, true, nil
}

// terminalFinish builds the one terminal chunk for this attempt.
//
// A provider that ended without reporting a stop reason gets an explicit
// failure rather than a synthesized stop: an attempt whose end was not reported
// must not become a successful answer.
func (s *stream) terminalFinish() llm.StreamChunk {
	if s.finish != nil {
		reason := *s.finish
		s.finish = nil
		return llm.StreamChunk{Type: llm.ChunkFinish, Reason: &reason}
	}
	reason := llm.FinishReason{Kind: llm.FinishError, Failure: llm.LlmFailure{
		Message: "the provider ended the stream without reporting a stop reason",
		Code:    "missing-finish",
	}}
	return llm.StreamChunk{Type: llm.ChunkFinish, Reason: &reason}
}

// Close releases the stream. It is idempotent and does not cancel the caller's
// context: cancellation is the caller's decision, not a side effect of reading.
func (s *stream) Close() error {
	s.closed = true
	return nil
}
