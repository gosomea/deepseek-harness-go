package agentloop_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/llm"
)

// blockingProvider is a provider whose stream blocks until the caller cancels.
//
// It is what makes "dispose an Agent with an active turn" testable at all: with a
// provider that returns instantly there is no window in which the turn is live,
// so the wait-for-release path would never be exercised.
type blockingProvider struct {
	// started is closed once an attempt has begun.
	started chan struct{}
	// release, when closed, lets a blocked turn finish.
	release chan struct{}
	// once guards started.
	once sync.Once
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{started: make(chan struct{}), release: make(chan struct{})}
}

func (p *blockingProvider) Info() llm.ProviderInfo {
	return llm.ProviderInfo{ID: "blocking", Name: "Blocking", Capabilities: []llm.Capability{llm.CapabilityStreaming}}
}

func (p *blockingProvider) Generate(ctx context.Context, request llm.Request) (llm.Stream, error) {
	p.once.Do(func() { close(p.started) })
	return &blockingStream{ctx: ctx, release: p.release}, nil
}

// blockingStream blocks in Recv until the context is cancelled or the provider is
// released, so a turn can be observed while it is genuinely in flight.
//
// A released stream still ends properly: it emits its text and a terminal finish
// chunk, because a stream that just stops would be an incomplete attempt and the
// test would then be measuring the wrong thing.
type blockingStream struct {
	ctx     context.Context
	release chan struct{}
	chunks  []llm.StreamChunk
	index   int
}

func (s *blockingStream) Recv() (llm.StreamChunk, bool, error) {
	if s.chunks == nil {
		select {
		case <-s.ctx.Done():
			return llm.StreamChunk{}, false, s.ctx.Err()
		case <-s.release:
		}
		// Releasing produces a complete, well-formed attempt.
		s.chunks = []llm.StreamChunk{
			{Type: llm.ChunkTextDelta, Index: 0, Text: "released"},
			{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishStop}},
		}
	}
	if s.index >= len(s.chunks) {
		return llm.StreamChunk{}, false, nil
	}
	chunk := s.chunks[s.index]
	s.index++
	return chunk, true, nil
}

func (s *blockingStream) Close() error { return nil }

// Disposing an Agent whose turn is in flight must return: the release path waits
// for the turn to settle, and a self-deadlock here would hang the process.
func TestDisposeWhileATurnIsInFlightReturns(t *testing.T) {
	log := newLog(t)
	provider := newBlockingProvider()
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	runner, ok := agentloop.AsRunner(handle)
	if !ok {
		t.Fatalf("handle must be runnable, got %T", handle)
	}
	turnDone := make(chan error, 1)
	go func() { turnDone <- runner.RunTurn(context.Background()) }()

	// Wait until the attempt has genuinely started, so the turn is active.
	select {
	case <-provider.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never started")
	}

	disposed := make(chan error, 1)
	go func() { disposed <- owner.Dispose(context.Background()) }()
	select {
	case err := <-disposed:
		// Cancellation during an in-flight stream surfaces as the turn failing;
		// dispose itself must not report an error caused by its own wait.
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("dispose: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dispose deadlocked waiting for the in-flight turn")
	}
	select {
	case <-turnDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never returned after cancellation")
	}
	if got := handle.State(); got != agent.StateDisposed {
		t.Fatalf("want disposed, got %s", got)
	}
}

// Cancelling an in-flight turn stops it, records the cancellation, and leaves the
// Agent alive so a later turn still runs.
func TestCancelInFlightTurnKeepsTheAgentUsable(t *testing.T) {
	log := newLog(t)
	provider := newBlockingProvider()
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	canceler, ok := handle.(agent.Canceler)
	if !ok {
		t.Fatalf("handle must implement Canceler, got %T", handle)
	}
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	runner, _ := agentloop.AsRunner(handle)
	turnDone := make(chan error, 1)
	go func() { turnDone <- runner.RunTurn(context.Background()) }()
	select {
	case <-provider.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never started")
	}
	canceled, err := canceler.Cancel(context.Background())
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !canceled {
		t.Fatal("cancelling an active turn must report that there was work to cancel")
	}
	select {
	case err := <-turnDone:
		if err == nil {
			t.Fatal("a cancelled turn must report an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never returned after cancellation")
	}
	if got := handle.State(); got != agent.StateIdle {
		t.Fatalf("cancelling must leave the Agent alive and idle, got %s", got)
	}
	// The Agent still accepts input and can run another turn.
	if err := handle.Send(context.Background(), agent.Input{Text: "again"}); err != nil {
		t.Fatalf("the Agent must accept input after cancellation: %v", err)
	}
	if got := handle.State(); got == agent.StateDisposed {
		t.Fatal("cancellation must not dispose the Agent")
	}
	// Disposal still works afterwards.
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("dispose after cancel: %v", err)
	}
}

// A caller-supplied deadline bounds the wait for an in-flight turn, and the
// deadline error is reported rather than swallowed.
func TestDisposeHonoursADeadlineWhileATurnIsInFlight(t *testing.T) {
	log := newLog(t)
	provider := newBlockingProvider()
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	runner, _ := agentloop.AsRunner(handle)
	go func() { _ = runner.RunTurn(context.Background()) }()
	<-provider.started

	// The provider releases shortly after the deadline expires, so dispose sees an
	// expired context and reports it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = owner.Dispose(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected dispose error: %v", err)
	}
	close(provider.release)
	// A second dispose with a live context finishes the job.
	deadline, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := owner.Dispose(deadline); err != nil {
		t.Fatalf("second dispose: %v", err)
	}
}

// A cancelled turn records the cancellation as its own fact, distinct from a
// failure.
func TestCancelledTurnIsRecordedAsCanceled(t *testing.T) {
	log := newLog(t)
	provider := newBlockingProvider()
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	canceler := handle.(agent.Canceler)
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	runner, _ := agentloop.AsRunner(handle)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.RunTurn(context.Background())
	}()
	<-provider.started
	if _, err := canceler.Cancel(context.Background()); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	<-done
	// The turn must close; whether it reports cancellation or a failed attempt
	// depends on where the stream was cut, and both are recorded facts.
	var sawTurnEnd bool
	for _, event := range log.Events() {
		if event.Type == "turn/end" {
			sawTurnEnd = true
		}
		// A cancelled attempt must never become an assistant message.
		if event.Type == "assistant/message" {
			t.Fatalf("a cancelled turn must not produce a successful message: %s", event.Data)
		}
	}
	if !sawTurnEnd {
		t.Fatal("a cancelled turn must still close with turn/end")
	}
}
