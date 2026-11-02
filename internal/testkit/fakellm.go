package testkit

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// FakeProvider is a deterministic llm.Provider for tests and examples.
//
// It exists so that the whole request/stream/assembly path can be exercised
// without a model endpoint, credentials or network: a test scripts the exact
// chunks it wants, including malformed ones, and an example can run anywhere.
// The provider never guesses: it replays the scripted chunks for the attempt it
// is asked for and reports a missing script as an observable error.
type FakeProvider struct {
	mu sync.Mutex
	// id is the route key this provider registers under.
	id string
	// name is the human-readable provider name.
	name string
	// capabilities is the declared feature set.
	capabilities []llm.Capability
	// scripts holds one chunk script per attempt, consumed in order.
	scripts [][]llm.StreamChunk
	// attempts records the requests received, in order.
	attempts []llm.Request
	// failure, when set, is returned instead of starting an attempt.
	failure error
	// closeErr, when set, is returned by Close.
	closeErr error
	// closed records whether Close was called.
	closed bool
}

// FakeProviderOption configures a FakeProvider.
type FakeProviderOption func(*FakeProvider)

// FakeProviderID sets the route key. The default is "fake".
func FakeProviderID(id string) FakeProviderOption {
	return func(p *FakeProvider) { p.id = id }
}

// FakeProviderName sets the human-readable provider name.
func FakeProviderName(name string) FakeProviderOption {
	return func(p *FakeProvider) { p.name = name }
}

// FakeProviderCapabilities replaces the declared capability set. Passing no
// capability declares none, which makes a route that refuses requests.
func FakeProviderCapabilities(capabilities ...llm.Capability) FakeProviderOption {
	return func(p *FakeProvider) {
		if capabilities == nil {
			p.capabilities = nil
			return
		}
		p.capabilities = append([]llm.Capability(nil), capabilities...)
	}
}

// FakeProviderFailure makes every attempt fail to start with the given error.
func FakeProviderFailure(err error) FakeProviderOption {
	return func(p *FakeProvider) { p.failure = err }
}

// FakeProviderCloseError makes Close report the given error.
func FakeProviderCloseError(err error) FakeProviderOption {
	return func(p *FakeProvider) { p.closeErr = err }
}

// NewFakeProvider returns a provider that serves the given attempt scripts in
// order. Each script is the exact chunk sequence of one attempt, so a test that
// needs two attempts passes two scripts.
func NewFakeProvider(scripts ...[]llm.StreamChunk) *FakeProvider {
	return &FakeProvider{
		id:           "fake",
		name:         "Fake Provider",
		capabilities: []llm.Capability{llm.CapabilityStreaming, llm.CapabilityUsage, llm.CapabilityToolCalls},
		scripts:      scripts,
	}
}

// NewFakeProviderWithOptions returns a provider configured by options, serving
// no attempt scripts. It is for tests that exercise registry behaviour or a
// capability refusal.
//
// It declares the same default capabilities as NewFakeProvider so that a test
// which only wants a reachable route does not accidentally get one that refuses
// every request. Pass FakeProviderCapabilities() with no capability to build a
// route that declares nothing.
func NewFakeProviderWithOptions(options ...FakeProviderOption) *FakeProvider {
	provider := &FakeProvider{
		id:           "fake",
		name:         "Fake Provider",
		capabilities: []llm.Capability{llm.CapabilityStreaming, llm.CapabilityUsage, llm.CapabilityToolCalls},
	}
	for _, option := range options {
		option(provider)
	}
	return provider
}

// Info describes the route and declares its capabilities.
func (p *FakeProvider) Info() llm.ProviderInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return llm.ProviderInfo{
		ID:           p.id,
		Name:         p.name,
		Capabilities: append([]llm.Capability(nil), p.capabilities...),
	}
}

// Generate starts one attempt, replaying the next scripted chunk sequence.
//
// It records the request it received so a test can assert what the assembler
// and prompt layers produced, which is how "the request is derivable from the
// log" is checked without a network call.
func (p *FakeProvider) Generate(ctx context.Context, request llm.Request) (llm.Stream, error) {
	p.mu.Lock()
	if p.failure != nil {
		err := p.failure
		p.mu.Unlock()
		return nil, err
	}
	p.attempts = append(p.attempts, request.Clone())
	var script []llm.StreamChunk
	if len(p.scripts) > 0 {
		script = p.scripts[0]
		p.scripts = p.scripts[1:]
	}
	p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return NewFakeStream(script), nil
}

// Attempts returns a copy of the requests this provider received, in order.
func (p *FakeProvider) Attempts() []llm.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]llm.Request(nil), p.attempts...)
}

// Close releases the provider and reports a configured close error.
func (p *FakeProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return p.closeErr
}

// Closed reports whether Close was called.
func (p *FakeProvider) Closed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// FakeStream replays one scripted chunk sequence.
//
// It is the controllable side of the model contract: a test scripts an
// interrupted stream, an out-of-order delta or a missing terminal finish and
// observes how the consumer reacts. An error scripted into the stream is
// returned once, after the chunks before it.
type FakeStream struct {
	mu sync.Mutex
	// chunks is the remaining script.
	chunks []llm.StreamChunk
	// err is reported after the chunks are exhausted.
	err error
	// index is the read position.
	index int
	// closed records whether Close was called.
	closed bool
	// closeErr is returned by Close.
	closeErr error
}

// NewFakeStream returns a stream that replays the given chunks and then ends.
func NewFakeStream(chunks []llm.StreamChunk) *FakeStream {
	return &FakeStream{chunks: append([]llm.StreamChunk(nil), chunks...)}
}

// NewFakeStreamWithError returns a stream that replays the given chunks and then
// reports the error instead of a clean end. It is how a mid-stream transport
// failure is reproduced without a real connection.
func NewFakeStreamWithError(chunks []llm.StreamChunk, err error) *FakeStream {
	return &FakeStream{chunks: append([]llm.StreamChunk(nil), chunks...), err: err}
}

// Recv returns the next chunk, or the scripted error once the chunks run out.
func (s *FakeStream) Recv() (llm.StreamChunk, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index < len(s.chunks) {
		chunk := s.chunks[s.index]
		s.index++
		return chunk, true, nil
	}
	if s.err != nil {
		err := s.err
		s.err = nil
		return llm.StreamChunk{}, false, err
	}
	return llm.StreamChunk{}, false, nil
}

// Close releases the stream. It is idempotent.
func (s *FakeStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.closeErr
}

// Closed reports whether Close was called.
func (s *FakeStream) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// ScriptTextAttempt builds the chunk script of one plain text answer: a text
// block opened, its text delivered in the given fragments, closed, followed by
// usage and a stop finish.
//
// It exists so a test's intent is visible: the fragments are the only varying
// input, and every script produced by this helper has the same shape as a real
// adapter's.
func ScriptTextAttempt(fragments []string) []llm.StreamChunk {
	chunks := []llm.StreamChunk{{Type: llm.ChunkBlockStart, Index: 0, BlockType: llm.BlockTypeText}}
	text := ""
	for _, fragment := range fragments {
		chunks = append(chunks, llm.StreamChunk{Type: llm.ChunkTextDelta, Index: 0, Text: fragment})
		text += fragment
	}
	chunks = append(chunks,
		llm.StreamChunk{Type: llm.ChunkBlockEnd, Index: 0, Block: llm.TextBlock{Text: text}},
		llm.StreamChunk{Type: llm.ChunkUsage, Usage: &llm.Usage{InputTokens: 3, OutputTokens: 2}},
		llm.StreamChunk{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishStop}},
	)
	return chunks
}

// ScriptToolCallAttempt builds the chunk script of one answer that requests a
// single tool call, with the arguments split across the given fragments.
//
// Splitting the arguments is the point: it is what a real provider does, and it
// is the case that a naive assembler mishandles by mixing concurrent calls.
func ScriptToolCallAttempt(callID llm.ToolCallID, name string, argumentFragments []string) []llm.StreamChunk {
	chunks := []llm.StreamChunk{{Type: llm.ChunkBlockStart, Index: 0, BlockType: llm.BlockTypeToolCall}}
	chunks = append(chunks, llm.StreamChunk{Type: llm.ChunkToolCallDelta, Index: 0, ID: callID, Name: &name})
	arguments := ""
	for _, fragment := range argumentFragments {
		chunks = append(chunks, llm.StreamChunk{Type: llm.ChunkToolCallDelta, Index: 0, ID: callID, ArgumentsDelta: fragment})
		arguments += fragment
	}
	chunks = append(chunks,
		llm.StreamChunk{Type: llm.ChunkBlockEnd, Index: 0, Block: llm.ToolCallBlock{ID: callID, Name: name, Arguments: arguments}},
		llm.StreamChunk{Type: llm.ChunkUsage, Usage: &llm.Usage{InputTokens: 5, OutputTokens: 4}},
		llm.StreamChunk{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishToolCalls}},
	)
	return chunks
}

// Drain reads a stream to exhaustion, returning every chunk and the final
// error. It is the shared consumer used by tests and examples, so "how a
// consumer reads a stream" is written once.
func Drain(stream llm.Stream) ([]llm.StreamChunk, error) {
	defer func() { _ = stream.Close() }()
	var chunks []llm.StreamChunk
	for {
		chunk, ok, err := stream.Recv()
		if err != nil {
			return chunks, err
		}
		if !ok {
			return chunks, nil
		}
		chunks = append(chunks, chunk)
	}
}

// RequireTerminalFinish reports an error when the given chunks contain no
// terminal finish chunk. It turns "the provider never finished" into a named
// failure, so a test asserts the incomplete-attempt case explicitly instead of
// reading a zero reason as a successful stop.
func RequireTerminalFinish(chunks []llm.StreamChunk) error {
	if _, ok := llm.Finish(chunks); ok {
		return nil
	}
	return fmt.Errorf("testkit: stream ended without a terminal finish chunk")
}
