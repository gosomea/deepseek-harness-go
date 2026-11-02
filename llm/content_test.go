package llm

import (
	"strings"
	"testing"
)

// TestBlockValidateAcceptsStructurallyUsableValues pins that the block vocabulary
// is structural only. The reference accepts an empty call id, an empty tool name
// and an unrecognized media type at this layer: it stores them and refuses them
// where the meaning is actually needed (the stream assembler and the provider
// adapter). Rejecting them here would refuse a record the reference stores.
func TestBlockValidateAcceptsStructurallyUsableValues(t *testing.T) {
	cases := []struct {
		name  string
		block ContentBlock
	}{
		{"text", TextBlock{}},
		{"reasoning", ReasoningBlock{}},
		{"tool-call without id", ToolCallBlock{Name: "read", Arguments: "{}"}},
		{"tool-call without any name", ToolCallBlock{}},
		{"tool-addition without name", ToolAdditionBlock{}},
		{"tool-removal without name", ToolRemovalBlock{}},
		{"image without attachment id", ImageBlock{}},
		{"file without any attachment", FileBlock{}},
		{"image with an unrecognized media type", ImageBlock{Attachment: ImageAttachmentRef{AttachmentID: "sha256:a", MediaType: "image/tiff"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.block.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want the vocabulary to accept it", err)
			}
		})
	}
}

// TestMediaTypeIsNotAClosedSet pins that an unrecognized encoding is stored
// rather than refused here. The reference admits any media type string and lets a
// provider reject what it cannot send, so a closed Go enum would be stricter than
// the behaviour being reproduced.
func TestMediaTypeIsNotAClosedSet(t *testing.T) {
	ref := ImageAttachmentRef{AttachmentID: "sha256:aa", MediaType: "image/tiff", Bytes: 4, Width: 1, Height: 1}
	if err := ref.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want an open media type", err)
	}
	for _, mediaType := range []ImageMediaType{MediaTypePNG, MediaTypeJPEG, MediaTypeWebP, MediaTypeGIF} {
		ok := ImageAttachmentRef{AttachmentID: "sha256:aa", MediaType: mediaType}
		if err := ok.Validate(); err != nil {
			t.Fatalf("Validate(%s) = %v, want nil", mediaType, err)
		}
	}
}

// TestContentRoundTripIsLossless pins the property the whole durable log rests
// on: decode then encode returns the same bytes for every known block.
func TestContentRoundTripIsLossless(t *testing.T) {
	original := `[{"type":"text","text":"hi"},{"type":"reasoning","text":"because"},` +
		`{"type":"tool-call","id":"c1","name":"read","arguments":"{\"path\":\"a\"}"},` +
		`{"type":"image","attachment":{"attachmentId":"sha256:aa","mediaType":"image/png","bytes":9,"width":2,"height":3}},` +
		`{"type":"tool-addition","toolName":"read"}]`
	blocks, err := UnmarshalContent([]byte(original))
	if err != nil {
		t.Fatalf("UnmarshalContent() error = %v", err)
	}
	encoded, err := MarshalContent(blocks)
	if err != nil {
		t.Fatalf("MarshalContent() error = %v", err)
	}
	if string(encoded) != original {
		t.Fatalf("round trip changed bytes\nwant: %s\ngot:  %s", original, encoded)
	}
}

// TestUnknownBlockSurvivesRoundTrip pins the deliberate policy that vocabulary
// growth is not lossy: an unrecognized block keeps its type and every field, and
// is never rewritten into text.
func TestUnknownBlockSurvivesRoundTrip(t *testing.T) {
	original := `[{"type":"audio-note","seconds":3,"transcript":"hi","nested":{"a":[1,2]}}]`
	blocks, err := UnmarshalContent([]byte(original))
	if err != nil {
		t.Fatalf("UnmarshalContent() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	unknown, ok := blocks[0].(UnknownBlock)
	if !ok {
		t.Fatalf("block type = %T, want UnknownBlock", blocks[0])
	}
	if unknown.Type != "audio-note" {
		t.Fatalf("Type = %q, want audio-note", unknown.Type)
	}
	encoded, err := MarshalContent(blocks)
	if err != nil {
		t.Fatalf("MarshalContent() error = %v", err)
	}
	if string(encoded) != original {
		t.Fatalf("unknown block round trip changed bytes\nwant: %s\ngot:  %s", original, encoded)
	}
	// A caller holding only the text helper must not see the unknown block
	// flattened into visible text.
	if text := ContentText(blocks); text != "" {
		t.Fatalf("ContentText() = %q, want empty: unknown blocks are not text", text)
	}
}

// TestUnknownBlockNeedsPayload pins that an unknown block without its original
// bytes is rejected: it could not be re-encoded, so keeping it would silently
// lose the value.
func TestUnknownBlockNeedsPayload(t *testing.T) {
	if err := (UnknownBlock{Type: "x"}).Validate(); err == nil {
		t.Fatal("Validate() = nil, want error for a payload-less unknown block")
	}
	if err := (UnknownBlock{Type: "x", Raw: []byte(`[1]`)}).Validate(); err == nil {
		t.Fatal("Validate() = nil, want error for a non-object unknown block")
	}
}

// TestDecodeBlockRejectsMalformedInput pins the read-side failures: content that
// is not an array, a block that is not an object, and a block without a
// discriminator are all refused instead of being coerced.
func TestDecodeBlockRejectsMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"not an array", `{"type":"text"}`, "must be an array"},
		{"block not an object", `[[]]`, "must be an object"},
		{"block without type", `[{"text":"hi"}]`, "requires a type"},
		{"known block with wrong field type", `[{"type":"text","text":7}]`, "cannot unmarshal"},
		{"trailing content", `[] []`, "trailing content"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := UnmarshalContent([]byte(testCase.data))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("UnmarshalContent() = %v, want error containing %q", err, testCase.want)
			}
		})
	}
}

// TestMarshalContentRejectsValuesWithNoRepresentation pins that the only content
// the encoder refuses is a value it cannot write at all: a nil block.
func TestMarshalContentRejectsInvalidBlocks(t *testing.T) {
	if _, err := MarshalContent([]ContentBlock{nil}); err == nil {
		t.Fatal("MarshalContent() = nil, want a nil block rejected")
	}
	// A structurally incomplete but representable block is written, not refused.
	encoded, err := MarshalContent([]ContentBlock{ToolCallBlock{Name: "read"}})
	if err != nil {
		t.Fatalf("MarshalContent() = %v, want an incomplete block written", err)
	}
	if !strings.Contains(string(encoded), `"type":"tool-call"`) {
		t.Fatalf("encoded = %s, want the block written", encoded)
	}
}

// TestEmptyContentEncodesAsEmptyArray pins that no content is a valid value and
// encodes as an empty array rather than null, so a stored message keeps its
// place without inventing a block.
func TestEmptyContentEncodesAsEmptyArray(t *testing.T) {
	encoded, err := MarshalContent(nil)
	if err != nil {
		t.Fatalf("MarshalContent(nil) error = %v", err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("MarshalContent(nil) = %s, want []", encoded)
	}
	blocks, err := UnmarshalContent([]byte("[]"))
	if err != nil {
		t.Fatalf("UnmarshalContent([]) error = %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("UnmarshalContent([]) = %d blocks, want 0", len(blocks))
	}
}

// TestTextContentDropsEmptyText pins the constructor rule that empty text means
// no content, which a later projection reads as "produces no model message".
func TestTextContentDropsEmptyText(t *testing.T) {
	if got := TextContent(""); got != nil {
		t.Fatalf("TextContent(\"\") = %v, want nil", got)
	}
	if got := ContentText(TextContent("a")); got != "a" {
		t.Fatalf("ContentText(TextContent(\"a\")) = %q, want a", got)
	}
	mixed := []ContentBlock{TextBlock{Text: "a"}, ReasoningBlock{Text: "b"}, UnknownBlock{Type: "x", Raw: []byte(`{"type":"x"}`)}}
	if got := ContentText(mixed); got != "a" {
		t.Fatalf("ContentText(mixed) = %q, want only visible text", got)
	}
}

// TestUsageValidateIsStructuralOnly pins that accounting is recorded, not
// range-checked. Reported absence stays nil so "not disclosed" is distinguishable
// from a reported zero, which is the distinction a later budget calculation needs.
func TestUsageValidateIsStructuralOnly(t *testing.T) {
	negative := -1
	if err := (Usage{TotalTokens: &negative}).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a negative count stored", err)
	}
	zero, total := 0, 5
	reported := Usage{InputTokens: zero, OutputTokens: zero, TotalTokens: &total}
	if err := reported.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil for reported zeros", err)
	}
	if reported.CacheReadTokens != nil {
		t.Fatal("an unreported counter must stay nil, not become zero")
	}
	if (Usage{}).CacheReadTokens != nil {
		t.Fatal("an absent counter must stay nil")
	}
}
