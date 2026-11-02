package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
)

func pushAll(t *testing.T, assembler *llm.Assembler, chunks []llm.StreamChunk) {
	t.Helper()
	for index, chunk := range chunks {
		if err := assembler.Push(chunk); err != nil {
			t.Fatalf("push chunk %d (%s): %v", index, chunk.Type, err)
		}
	}
}

func TestAssemblerJoinsTextFragmentsIntoOneBlock(t *testing.T) {
	assembler := llm.NewAssembler()
	pushAll(t, assembler, testkit.ScriptTextAttempt([]string{"Hel", "lo, ", "world"}))
	blocks := assembler.Blocks()
	if len(blocks) != 1 {
		t.Fatalf("want one block, got %d", len(blocks))
	}
	if got := llm.ContentText(blocks); got != "Hello, world" {
		t.Fatalf("unexpected text %q", got)
	}
	if !assembler.Closed() {
		t.Fatal("a stop finish must close the assembler")
	}
}

// Interleaved tool calls are the case a naive assembler gets wrong: two calls
// stream their argument fragments at overlapping indexes and must not mix.
func TestAssemblerKeepsInterleavedToolCallsSeparate(t *testing.T) {
	assembler := llm.NewAssembler()
	firstName, secondName := "read_file", "write_file"
	chunks := []llm.StreamChunk{
		{Type: llm.ChunkToolCallDelta, Index: 0, ID: "call-a", Name: &firstName, ArgumentsDelta: `{"pa`},
		{Type: llm.ChunkToolCallDelta, Index: 1, ID: "call-b", Name: &secondName, ArgumentsDelta: `{"pa`},
		{Type: llm.ChunkToolCallDelta, Index: 0, ArgumentsDelta: `th":"a.txt"}`},
		{Type: llm.ChunkToolCallDelta, Index: 1, ArgumentsDelta: `th":"b.txt"}`},
		{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishToolCalls}},
	}
	pushAll(t, assembler, chunks)
	blocks := assembler.Blocks()
	if len(blocks) != 2 {
		t.Fatalf("want two tool calls, got %d", len(blocks))
	}
	first, ok := blocks[0].(llm.ToolCallBlock)
	if !ok {
		t.Fatalf("block 0 is %T, want ToolCallBlock", blocks[0])
	}
	second, ok := blocks[1].(llm.ToolCallBlock)
	if !ok {
		t.Fatalf("block 1 is %T, want ToolCallBlock", blocks[1])
	}
	if first.ID != "call-a" || first.Name != "read_file" || first.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("first call mixed: %+v", first)
	}
	if second.ID != "call-b" || second.Name != "write_file" || second.Arguments != `{"path":"b.txt"}` {
		t.Fatalf("second call mixed: %+v", second)
	}
}

// A block closed by block-end is authoritative; later deltas must not change it.
func TestAssemblerFreezesBlocksClosedByBlockEnd(t *testing.T) {
	assembler := llm.NewAssembler()
	chunks := []llm.StreamChunk{
		{Type: llm.ChunkBlockStart, Index: 0, BlockType: llm.BlockTypeText},
		{Type: llm.ChunkTextDelta, Index: 0, Text: "draft"},
		{Type: llm.ChunkBlockEnd, Index: 0, Block: llm.TextBlock{Text: "authoritative"}},
		{Type: llm.ChunkTextDelta, Index: 0, Text: " ignored"},
		{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishStop}},
	}
	pushAll(t, assembler, chunks)
	if got := llm.ContentText(assembler.Blocks()); got != "authoritative" {
		t.Fatalf("block-end must be authoritative, got %q", got)
	}
}

// A second terminal chunk is a protocol violation and must be refused, so an
// adapter cannot rewrite how an attempt ended.
func TestAssemblerRefusesASecondFinish(t *testing.T) {
	assembler := llm.NewAssembler()
	if err := assembler.Push(llm.StreamChunk{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishStop}}); err != nil {
		t.Fatalf("first finish: %v", err)
	}
	err := assembler.Push(llm.StreamChunk{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishError, Failure: llm.LlmFailure{Message: "late", Code: "late"}}})
	if !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a second finish, got %v", err)
	}
	reason, _ := assembler.FinishReason()
	if reason.Kind != llm.FinishStop {
		t.Fatalf("the first finish must win, got %s", reason.Kind)
	}
}

func TestAssemblerRefusesMalformedChunksBeforeChangingState(t *testing.T) {
	assembler := llm.NewAssembler()
	if err := assembler.Push(llm.StreamChunk{Type: llm.ChunkTextDelta, Index: -1}); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a negative index, got %v", err)
	}
	if len(assembler.Blocks()) != 0 {
		t.Fatal("a refused chunk must not create state")
	}
}

func TestAssemblerIgnoresUnknownChunkKinds(t *testing.T) {
	assembler := llm.NewAssembler()
	if err := assembler.Push(llm.StreamChunk{Type: llm.ChunkType("audio-delta"), Index: 0}); err != nil {
		t.Fatalf("unknown chunk kind must be tolerated: %v", err)
	}
	if len(assembler.Blocks()) != 0 {
		t.Fatal("an unknown chunk kind must not produce content")
	}
}

// A stream cut off before its finish chunk is an incomplete attempt: the
// assembled blocks are still visible, but the message must be refused.
func TestAssembleReportsAnIncompleteAttempt(t *testing.T) {
	assembler := llm.NewAssembler()
	stream := testkit.NewFakeStream([]llm.StreamChunk{{Type: llm.ChunkTextDelta, Index: 0, Text: "partial"}})
	blocks, err := assembler.Assemble(stream)
	if !errors.Is(err, llm.ErrIncompleteStream) {
		t.Fatalf("want ErrIncompleteStream, got %v", err)
	}
	if llm.ContentText(blocks) != "partial" {
		t.Fatalf("partial output must stay inspectable, got %q", llm.ContentText(blocks))
	}
	if _, err := assembler.Message("p", "m"); !errors.Is(err, llm.ErrIncompleteStream) {
		t.Fatalf("Message must refuse an incomplete attempt, got %v", err)
	}
	if !stream.Closed() {
		t.Fatal("Assemble must close the stream")
	}
}

// A half-received tool call must never become a message, because executing it
// would turn a truncated request into a real side effect.
func TestAssemblerRefusesAnOpenToolCall(t *testing.T) {
	assembler := llm.NewAssembler()
	name := "danger"
	pushAll(t, assembler, []llm.StreamChunk{
		{Type: llm.ChunkToolCallDelta, Index: 0, ID: "call-x", Name: &name, ArgumentsDelta: `{"half`},
		{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishToolCalls}},
	})
	open := assembler.OpenToolCalls()
	if len(open) != 1 || open[0] != "call-x" {
		t.Fatalf("want one open tool call, got %v", open)
	}
	if _, err := assembler.Message("p", "m"); !errors.Is(err, llm.ErrToolCallNotClosed) {
		t.Fatalf("want ErrToolCallNotClosed, got %v", err)
	}
}

// A failed attempt must not be turned into a successful message.
func TestAssemblerRefusesToMintAMessageForAFailedAttempt(t *testing.T) {
	assembler := llm.NewAssembler()
	pushAll(t, assembler, []llm.StreamChunk{
		{Type: llm.ChunkTextDelta, Index: 0, Text: "half"},
		{Type: llm.ChunkFinish, Reason: &llm.FinishReason{
			Kind:    llm.FinishError,
			Failure: llm.LlmFailure{Message: "upstream 500", Code: "server-error"},
		}},
	})
	_, err := assembler.Message("p", "m")
	var failure *llm.FailureError
	if !errors.As(err, &failure) {
		t.Fatalf("want FailureError, got %v", err)
	}
	if failure.Failure.Code != "server-error" {
		t.Fatalf("unexpected code %q", failure.Failure.Code)
	}
	// The producer returns a *FailureError, so errors.As is the way to recover
	// the facts: a value target would never compare equal to a pointer, and
	// asserting otherwise would be testing the wrong thing.
	if got := err.Error(); got != "llm: server-error: upstream 500" {
		t.Fatalf("unexpected error text %q", got)
	}
}

// Unknown usage and a reported zero are different facts, and the assembler is
// where that distinction must survive.
func TestAssemblerDistinguishesUnknownUsageFromZeroUsage(t *testing.T) {
	silent := llm.NewAssembler()
	pushAll(t, silent, []llm.StreamChunk{{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishStop}}})
	if _, reported := silent.Usage(); reported {
		t.Fatal("a provider that reports nothing must not look like a reported zero")
	}

	zero := llm.NewAssembler()
	pushAll(t, zero, []llm.StreamChunk{
		{Type: llm.ChunkUsage, Usage: &llm.Usage{InputTokens: 0, OutputTokens: 0}},
		{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishStop}},
	})
	usage, reported := zero.Usage()
	if !reported {
		t.Fatal("a reported zero must count as reported")
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Fatalf("reported zero must stay zero, got %+v", usage)
	}
}

func TestAssembleMessageReturnsUsageAndMessage(t *testing.T) {
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ans", "wer"}))
	registry := llm.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	stream, err := registry.Generate(context.Background(), userRequest(t, "fake"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	message, blocks, usage, reported, err := llm.AssembleMessage(stream, "fake", "fake-1")
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if llm.ContentText(blocks) != "answer" {
		t.Fatalf("unexpected blocks %q", llm.ContentText(blocks))
	}
	if message.Role != llm.RoleAssistant || message.Source.Provider != "fake" {
		t.Fatalf("unexpected message %+v", message)
	}
	if !reported || usage.InputTokens != 3 || usage.OutputTokens != 2 {
		t.Fatalf("unexpected usage %+v (reported=%v)", usage, reported)
	}
}

// The request the provider received must be exactly what the caller derived:
// the fake records it, which is how "the request is derivable" is checked
// without a network call.
func TestAssembleMessagePreservesTheRequestItWasGiven(t *testing.T) {
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	registry := llm.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	request := userRequest(t, "fake")
	stream, err := registry.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, _, _, _, err := llm.AssembleMessage(stream, "fake", "fake-1"); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	attempts := provider.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("want 1 recorded attempt, got %d", len(attempts))
	}
	if llm.ContentText(attempts[0].Messages[0].Content) != "hello" {
		t.Fatalf("unexpected recorded request %+v", attempts[0])
	}
}
