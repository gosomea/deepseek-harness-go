package llm_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
)

func TestChunkValidateAcceptsEveryKnownShape(t *testing.T) {
	name := "read_file"
	cases := []struct {
		name  string
		chunk llm.StreamChunk
	}{
		{"block-start", llm.StreamChunk{Type: llm.ChunkBlockStart, Index: 0, BlockType: llm.BlockTypeText}},
		{"text-delta", llm.StreamChunk{Type: llm.ChunkTextDelta, Index: 1, Text: "hi"}},
		{"reasoning-delta", llm.StreamChunk{Type: llm.ChunkReasoningDelta, Index: 2, Text: "why"}},
		{"tool-call-delta", llm.StreamChunk{Type: llm.ChunkToolCallDelta, Index: 3, ID: "call-1", Name: &name, ArgumentsDelta: "{"}},
		{"block-end", llm.StreamChunk{Type: llm.ChunkBlockEnd, Index: 0, Block: llm.TextBlock{Text: "done"}}},
		{"usage", llm.StreamChunk{Type: llm.ChunkUsage, Usage: &llm.Usage{InputTokens: 1}}},
		{"finish", llm.StreamChunk{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishStop}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.chunk.Validate(); err != nil {
				t.Fatalf("valid chunk refused: %v", err)
			}
		})
	}
}

func TestChunkValidateRefusesIncompleteShapes(t *testing.T) {
	cases := []struct {
		name  string
		chunk llm.StreamChunk
	}{
		{"missing type", llm.StreamChunk{Index: 0}},
		{"negative index", llm.StreamChunk{Type: llm.ChunkTextDelta, Index: -1}},
		{"block-start without blockType", llm.StreamChunk{Type: llm.ChunkBlockStart, Index: 0}},
		{"tool-call-delta without payload", llm.StreamChunk{Type: llm.ChunkToolCallDelta, Index: 0}},
		{"block-end without block", llm.StreamChunk{Type: llm.ChunkBlockEnd, Index: 0}},
		{"usage without usage", llm.StreamChunk{Type: llm.ChunkUsage, Index: 0}},
		{"finish without reason", llm.StreamChunk{Type: llm.ChunkFinish, Index: 0}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.chunk.Validate(); !errors.Is(err, llm.ErrInvalidValue) {
				t.Fatalf("want ErrInvalidValue, got %v", err)
			}
		})
	}
}

// A provider-specific chunk kind must survive rather than be refused: the
// reference's chunk map is merge-extensible, so an adapter may legally report a
// kind this build does not know.
func TestUnknownChunkKindIsPreservedNotRefused(t *testing.T) {
	chunk := llm.StreamChunk{Type: llm.ChunkType("audio-delta"), Index: 0}
	if err := chunk.Validate(); err != nil {
		t.Fatalf("unknown chunk kind refused: %v", err)
	}
	if chunk.IsTerminal() {
		t.Fatal("an unknown chunk must not be treated as terminal")
	}
}

func TestFinishReasonRequiresFailureFactsWhenFailing(t *testing.T) {
	failing := []llm.FinishKind{llm.FinishAborted, llm.FinishError}
	for _, kind := range failing {
		reason := llm.FinishReason{Kind: kind}
		if err := reason.Validate(); !errors.Is(err, llm.ErrInvalidValue) {
			t.Fatalf("%s without failure: want ErrInvalidValue, got %v", kind, err)
		}
		reason.Failure = llm.LlmFailure{Message: "connection reset", Code: "transport"}
		if err := reason.Validate(); err != nil {
			t.Fatalf("%s with failure refused: %v", kind, err)
		}
	}
	for _, kind := range []llm.FinishKind{llm.FinishStop, llm.FinishToolCalls, llm.FinishMaxTokens} {
		if err := (llm.FinishReason{Kind: kind}).Validate(); err != nil {
			t.Fatalf("successful kind %s refused: %v", kind, err)
		}
	}
	if err := (llm.FinishReason{Kind: ""}).Validate(); !errors.Is(err, llm.ErrInvalidValue) {
		t.Fatalf("empty kind: want ErrInvalidValue, got %v", err)
	}
	if err := (llm.FinishReason{Kind: llm.FinishKind("provider-quirk")}).Validate(); err != nil {
		t.Fatalf("provider-specific finish kind refused: %v", err)
	}
}

// A stream cut off before its terminal chunk is an incomplete attempt. The
// helper must say so, because a zero FinishReason otherwise looks like a stop.
func TestFinishReportsWhetherATerminalChunkWasSeen(t *testing.T) {
	complete := []llm.StreamChunk{
		{Type: llm.ChunkTextDelta, Index: 0, Text: "hi"},
		{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishStop}},
	}
	reason, ok := llm.Finish(complete)
	if !ok || reason.Kind != llm.FinishStop {
		t.Fatalf("want stop finish, got %v (ok=%v)", reason, ok)
	}
	truncated := []llm.StreamChunk{{Type: llm.ChunkTextDelta, Index: 0, Text: "hi"}}
	if _, ok := llm.Finish(truncated); ok {
		t.Fatal("a stream without a terminal chunk must not report a finish")
	}
}

func TestFailureErrorRoundTripsThroughErrorsAs(t *testing.T) {
	failure := llm.LlmFailure{Message: "rate limited", Code: "rate-limited"}
	err := error(&llm.FailureError{Failure: failure})
	var recovered *llm.FailureError
	if !errors.As(err, &recovered) {
		t.Fatalf("errors.As did not recover the failure from %v", err)
	}
	if recovered.Failure.Code != "rate-limited" {
		t.Fatalf("unexpected code %q", recovered.Failure.Code)
	}
	if err.Error() != "llm: rate-limited: rate limited" {
		t.Fatalf("unexpected message %q", err.Error())
	}
}
