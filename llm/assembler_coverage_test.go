package llm

import "testing"

// These tests live in the package so the coverage gate counts the assembler
// branches an external test cannot reach directly.

func TestAssemblerPartialBlocksFollowStreamOrder(t *testing.T) {
	assembler := NewAssembler()
	name := "tool"
	// Index 3 opens before index 1; blocks must come back in index order, not
	// in first-seen order.
	if err := assembler.Push(StreamChunk{Type: ChunkTextDelta, Index: 3, Text: "later"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := assembler.Push(StreamChunk{Type: ChunkTextDelta, Index: 1, Text: "earlier"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := assembler.Push(StreamChunk{Type: ChunkToolCallDelta, Index: 2, ID: "c", Name: &name, ArgumentsDelta: "{}"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	blocks := assembler.Blocks()
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(blocks))
	}
	if ContentText(blocks[0:1]) != "earlier" {
		t.Fatalf("indexes must be ordered, got %q first", ContentText(blocks[0:1]))
	}
	if _, ok := blocks[1].(ToolCallBlock); !ok {
		t.Fatalf("index 2 must be the tool call, got %T", blocks[1])
	}
	if ContentText(blocks[2:3]) != "later" {
		t.Fatalf("index 3 must be last, got %q", ContentText(blocks[2:3]))
	}
}

// A duplicate block-start must not discard already accumulated text.
func TestAssemblerIgnoresADuplicateBlockStart(t *testing.T) {
	assembler := NewAssembler()
	if err := assembler.Push(StreamChunk{Type: ChunkBlockStart, Index: 0, BlockType: BlockTypeText}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := assembler.Push(StreamChunk{Type: ChunkTextDelta, Index: 0, Text: "kept"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := assembler.Push(StreamChunk{Type: ChunkBlockStart, Index: 0, BlockType: BlockTypeText}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := ContentText(assembler.Blocks()); got != "kept" {
		t.Fatalf("a duplicate block-start must not reset the block, got %q", got)
	}
}

// A tool-call delta that arrives without any name still assembles into a call
// whose name is empty: the tolerance exists so a partial stream is inspectable.
func TestAssemblerToleratesANamelessToolCall(t *testing.T) {
	assembler := NewAssembler()
	if err := assembler.Push(StreamChunk{Type: ChunkToolCallDelta, Index: 0, ID: "c1", ArgumentsDelta: "{}"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	block, ok := assembler.Blocks()[0].(ToolCallBlock)
	if !ok {
		t.Fatalf("want a tool call, got %T", assembler.Blocks()[0])
	}
	if block.Name != "" || block.ID != "c1" {
		t.Fatalf("unexpected call %+v", block)
	}
}

// Reason-only and unknown-block-start indexes must behave predictably.
func TestAssemblerHandlesReasoningAndUnknownIndexes(t *testing.T) {
	assembler := NewAssembler()
	if err := assembler.Push(StreamChunk{Type: ChunkReasoningDelta, Index: 0, Text: "why"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	blocks := assembler.Blocks()
	if _, ok := blocks[0].(ReasoningBlock); !ok {
		t.Fatalf("want a reasoning block, got %T", blocks[0])
	}
	// A block-start whose type this build cannot assemble contributes no block.
	if err := assembler.Push(StreamChunk{Type: ChunkBlockStart, Index: 1, BlockType: ContentBlockType("audio")}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(assembler.Blocks()) != 1 {
		t.Fatalf("an unassembleable index must contribute nothing, got %d blocks", len(assembler.Blocks()))
	}
}

// Usage accumulates across several reports and stays absent when none arrives.
func TestAssemblerSumsMultipleUsageReports(t *testing.T) {
	assembler := NewAssembler()
	if err := assembler.Push(StreamChunk{Type: ChunkUsage, Usage: &Usage{InputTokens: 1, OutputTokens: 2}}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := assembler.Push(StreamChunk{Type: ChunkUsage, Usage: &Usage{InputTokens: 3, OutputTokens: 4}}); err != nil {
		t.Fatalf("push: %v", err)
	}
	usage, reported := assembler.Usage()
	if !reported || usage.InputTokens != 4 || usage.OutputTokens != 6 {
		t.Fatalf("want summed 4/6, got %+v (reported=%v)", usage, reported)
	}
}
