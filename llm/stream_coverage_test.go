package llm

import "testing"

// These tests live in the package so the coverage gate counts the chunk
// validation and usage helpers an external test cannot reach directly.

func TestChunkIsTerminalOnlyForFinish(t *testing.T) {
	if !(StreamChunk{Type: ChunkFinish, Reason: &FinishReason{Kind: FinishStop}}).IsTerminal() {
		t.Fatal("a finish chunk must be terminal")
	}
	for _, kind := range []ChunkType{ChunkBlockStart, ChunkTextDelta, ChunkReasoningDelta, ChunkToolCallDelta, ChunkBlockEnd, ChunkUsage} {
		if (StreamChunk{Type: kind}).IsTerminal() {
			t.Fatalf("%s must not be terminal", kind)
		}
	}
}

func TestValidateContentRefusesNilBlocks(t *testing.T) {
	if err := ValidateContent([]ContentBlock{nil}); err == nil {
		t.Fatal("a nil block must be refused")
	}
	if err := ValidateContent(nil); err != nil {
		t.Fatalf("a nil slice means no content and must be valid: %v", err)
	}
	if err := ValidateContent([]ContentBlock{TextBlock{Text: "ok"}}); err != nil {
		t.Fatalf("a text block must be valid: %v", err)
	}
}

func TestAddOptionalKeepsAbsenceAndSumsPresence(t *testing.T) {
	five, seven := 5, 7
	if got := addOptional(nil, nil); got != nil {
		t.Fatalf("absence plus absence must stay absent, got %v", got)
	}
	if got := addOptional(nil, &five); got == nil || *got != 5 {
		t.Fatalf("absent plus reported must be the reported value, got %v", got)
	}
	if got := addOptional(&five, nil); got == nil || *got != 5 {
		t.Fatalf("reported plus absent must keep the value, got %v", got)
	}
	if got := addOptional(&five, &seven); got == nil || *got != 12 {
		t.Fatalf("reported plus reported must sum, got %v", got)
	}
}

func TestStreamChunkValidateCoversEveryBranch(t *testing.T) {
	name := "tool"
	cases := []struct {
		name    string
		chunk   StreamChunk
		wantErr bool
	}{
		{"reasoning delta", StreamChunk{Type: ChunkReasoningDelta, Index: 0, Text: "x"}, false},
		{"tool call delta by id only", StreamChunk{Type: ChunkToolCallDelta, Index: 0, ID: "c"}, false},
		{"tool call delta by name only", StreamChunk{Type: ChunkToolCallDelta, Index: 0, Name: &name}, false},
		{"tool call delta by arguments only", StreamChunk{Type: ChunkToolCallDelta, Index: 0, ArgumentsDelta: "{"}, false},
		{"block-end with an invalid block", StreamChunk{Type: ChunkBlockEnd, Index: 0, Block: UnknownBlock{Type: "", Raw: nil}}, true},
		{"usage with invalid usage", StreamChunk{Type: ChunkUsage, Index: 0, Usage: &Usage{InputTokens: -1}}, false},
		{"finish with an empty reason", StreamChunk{Type: ChunkFinish, Index: 0, Reason: &FinishReason{Kind: ""}}, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.chunk.Validate()
			if testCase.wantErr && err == nil {
				t.Fatalf("want an error for %s", testCase.name)
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("unexpected error for %s: %v", testCase.name, err)
			}
		})
	}
}
