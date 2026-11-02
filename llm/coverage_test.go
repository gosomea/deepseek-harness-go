package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEveryKnownBlockReportsItsDiscriminator pins the discriminator each block
// reports, which the codec writes and a reader classifies by.
func TestEveryKnownBlockReportsItsDiscriminator(t *testing.T) {
	cases := []struct {
		block ContentBlock
		want  ContentBlockType
	}{
		{TextBlock{}, BlockTypeText},
		{ReasoningBlock{}, BlockTypeReasoning},
		{ToolCallBlock{}, BlockTypeToolCall},
		{ToolAdditionBlock{}, BlockTypeToolAddition},
		{ToolRemovalBlock{}, BlockTypeToolRemoval},
		{ImageBlock{}, BlockTypeImage},
		{FileBlock{}, BlockTypeFile},
		{UnknownBlock{Type: "custom"}, "custom"},
	}
	for _, testCase := range cases {
		if got := testCase.block.BlockType(); got != testCase.want {
			t.Fatalf("%T.BlockType() = %q, want %q", testCase.block, got, testCase.want)
		}
	}
}

// TestBlockSealMethodsAreNoOps pins that the interface seal carries no behaviour:
// a caller cannot make a block do work by holding it as the interface.
func TestBlockSealMethodsAreNoOps(t *testing.T) {
	blocks := []ContentBlock{
		TextBlock{}, ReasoningBlock{}, ToolCallBlock{}, ToolAdditionBlock{},
		ToolRemovalBlock{}, ImageBlock{}, FileBlock{}, UnknownBlock{},
	}
	for _, block := range blocks {
		block.contentBlock()
	}
}

// TestUnknownBlockDecoderBuildsTaggedPayload pins the path that re-adds a
// discriminator: a preserved object whose own "type" disagrees with the block's
// declared type is rebuilt with the declared one leading.
func TestUnknownBlockDecoderBuildsTaggedPayload(t *testing.T) {
	encoded, err := MarshalContent([]ContentBlock{UnknownBlock{Type: "audio", Raw: []byte(`{"type":"video","x":1}`)}})
	if err != nil {
		t.Fatalf("MarshalContent() error = %v", err)
	}
	if got := string(encoded); got != `[{"type":"audio","x":1}]` {
		t.Fatalf("encoded = %s, want the declared type leading and the payload kept", got)
	}
	// An object with no discriminator at all also needs one spliced in.
	encoded, err = MarshalContent([]ContentBlock{UnknownBlock{Type: "audio", Raw: []byte(`{"x":1}`)}})
	if err != nil {
		t.Fatalf("MarshalContent() error = %v", err)
	}
	if got := string(encoded); got != `[{"type":"audio","x":1}]` {
		t.Fatalf("encoded = %s, want the type spliced in front", got)
	}
	// An empty object is still a valid object.
	encoded, err = MarshalContent([]ContentBlock{UnknownBlock{Type: "audio", Raw: []byte(`{}`)}})
	if err != nil {
		t.Fatalf("MarshalContent() error = %v", err)
	}
	if got := string(encoded); got != `[{"type":"audio"}]` {
		t.Fatalf("encoded = %s, want a tag-only object", got)
	}
	if _, err := MarshalContent([]ContentBlock{UnknownBlock{Type: "audio", Raw: []byte(`"scalar"`)}}); err == nil {
		t.Fatal("MarshalContent() = nil, want a non-object payload rejected")
	}
}

// TestSplitFieldsHandlesNestedAndEscapedCommas pins the object splitter the
// encoder uses: a comma inside a nested value or a string is not a field
// boundary, so a preserved payload is never cut in half.
func TestSplitFieldsHandlesNestedAndEscapedCommas(t *testing.T) {
	body := []byte(`"a":{"b":[1,2],"c":"x,y"},"d":"quote\"comma,","e":3`)
	fields := splitFields(body)
	if len(fields) != 3 {
		t.Fatalf("splitFields() = %d fields, want 3: %q", len(fields), fields)
	}
	for _, field := range fields {
		if !json.Valid(append(append([]byte{'{'}, field...), '}')) {
			t.Fatalf("field %q is not a standalone member", field)
		}
	}

	// Round-tripping a payload with nested commas and escaped quotes must not
	// change its bytes.
	raw := []byte(`{"type":"weird","a":{"b":[1,2],"c":"x,y"},"d":"quote\"comma,"}`)
	encoded, err := MarshalContent([]ContentBlock{UnknownBlock{Type: "weird", Raw: raw}})
	if err != nil {
		t.Fatalf("MarshalContent() error = %v", err)
	}
	if got := string(encoded); got != "["+string(raw)+"]" {
		t.Fatalf("encoded = %s, want the payload bytes preserved", got)
	}
}

// TestRemoveTypeFieldLeavesOtherFieldsInOrder pins the helper that drops a
// stray discriminator without reordering anything else.
func TestRemoveTypeFieldLeavesOtherFieldsInOrder(t *testing.T) {
	got := string(removeTypeField([]byte(`"type":"video","a":1,"b":2`)))
	if got != `"a":1,"b":2` {
		t.Fatalf("removeTypeField() = %s, want the other fields in order", got)
	}
	// A body whose only field is the discriminator becomes empty.
	if got := string(removeTypeField([]byte(`"type":"video"`))); got != "" {
		t.Fatalf("removeTypeField() = %q, want empty", got)
	}
	// A field that merely starts with "type" is not the discriminator.
	got = string(removeTypeField([]byte(`"typeName":"video","a":1`)))
	if got != `"typeName":"video","a":1` {
		t.Fatalf("removeTypeField() = %s, want an unrelated field kept", got)
	}
}

// TestSourceValidateCoversEveryKind pins each source kind's rule, including the
// unknown-kind fall-through that keeps foreign attribution readable.
func TestSourceValidateCoversEveryKind(t *testing.T) {
	cases := []struct {
		name   string
		source MessageSource
		wantOK bool
	}{
		{"user", MessageSource{Kind: SourceUser}, true},
		{"system-prompt", MessageSource{Kind: SourceSystemPrompt}, true},
		{"model complete", MessageSource{Kind: SourceModel, Provider: "p", Model: "m"}, true},
		{"model without provider", MessageSource{Kind: SourceModel, Model: "m"}, false},
		{"model without model", MessageSource{Kind: SourceModel, Provider: "p"}, false},
		{"tool complete", MessageSource{Kind: SourceTool, CallID: "c"}, true},
		{"tool without call", MessageSource{Kind: SourceTool}, false},
		{"empty kind", MessageSource{}, false},
		{"foreign kind", MessageSource{Kind: "plugin:x"}, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.source.Validate()
			if testCase.wantOK && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !testCase.wantOK && err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
		})
	}
}

// TestAttachmentValidateIsStructuralOnly pins that the reference's permissive
// accounting is reproduced: a negative byte count or dimension is stored rather
// than refused, while a missing content address still is not usable.
func TestAttachmentValidateIsStructuralOnly(t *testing.T) {
	negativeImage := ImageAttachmentRef{AttachmentID: "sha256:a", MediaType: MediaTypePNG, Bytes: -1, Width: -1}
	if err := negativeImage.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want negative counts stored", err)
	}
	negativeFile := FileAttachmentRef{AttachmentID: "sha256:a", Name: "f", Bytes: -1}
	if err := negativeFile.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a negative byte count stored", err)
	}
}

// TestUnmarshalSourceKeepsForeignExtraFields pins that a producer's own fields
// survive even when the whole source is foreign.
func TestUnmarshalSourceKeepsForeignExtraFields(t *testing.T) {
	source, err := UnmarshalSource([]byte(`{"kind":"plugin:x","nested":{"a":1},"list":[1,2]}`))
	if err != nil {
		t.Fatalf("UnmarshalSource() error = %v", err)
	}
	if source.Kind != "plugin:x" {
		t.Fatalf("Kind = %q, want plugin:x", source.Kind)
	}
	if len(source.Extra) != 2 {
		t.Fatalf("Extra = %v, want two preserved fields", source.Extra)
	}
	encoded, err := MarshalSource(source)
	if err != nil {
		t.Fatalf("MarshalSource() error = %v", err)
	}
	if !strings.HasPrefix(string(encoded), `{"kind":"plugin:x"`) {
		t.Fatalf("encoded = %s, want kind first", encoded)
	}
	again, err := UnmarshalSource(encoded)
	if err != nil {
		t.Fatalf("UnmarshalSource() error = %v", err)
	}
	if len(again.Extra) != 2 {
		t.Fatalf("Extra after round trip = %v, want two fields", again.Extra)
	}
}

// TestUnmarshalMessagesRejectsInvalidEntries pins that one bad message fails the
// whole slice instead of being dropped, which would silently shorten history.
func TestUnmarshalMessagesRejectsInvalidEntries(t *testing.T) {
	_, err := UnmarshalMessages([]byte(`[{"id":"m1","role":"user","content":[],"source":{"kind":"user"}},{"id":"","role":"user","content":[],"source":{"kind":"user"}}]`))
	if err == nil || !strings.Contains(err.Error(), "message 1") {
		t.Fatalf("UnmarshalMessages() = %v, want the second message blamed", err)
	}
	if _, err := UnmarshalMessages([]byte(`{"id":"m1"}`)); err == nil {
		t.Fatal("UnmarshalMessages() = nil, want a non-array rejected")
	}
}

// TestMarshalSourceIgnoresExtraCollidingWithKnownFields pins that a caller
// cannot use Extra to overwrite a typed field: the typed value wins.
func TestMarshalSourceIgnoresExtraCollidingWithKnownFields(t *testing.T) {
	source := MessageSource{Kind: SourceUser, Extra: map[string]any{"kind": "model", "provider": "injected"}}
	encoded, err := MarshalSource(source)
	if err != nil {
		t.Fatalf("MarshalSource() error = %v", err)
	}
	if strings.Count(string(encoded), `"kind"`) != 1 {
		t.Fatalf("encoded = %s, want exactly one kind field", encoded)
	}
	if !strings.Contains(string(encoded), `"kind":"user"`) {
		t.Fatalf("encoded = %s, want the typed kind preserved", encoded)
	}
}

// TestDecodeCoversEveryKnownBlock pins that each known discriminator decodes to
// its own concrete type through the codec, so the reader's switch is exhaustive.
func TestDecodeCoversEveryKnownBlock(t *testing.T) {
	cases := []struct {
		payload string
		want    ContentBlockType
	}{
		{`{"type":"text","text":"t"}`, BlockTypeText},
		{`{"type":"reasoning","text":"r"}`, BlockTypeReasoning},
		{`{"type":"tool-call","id":"c","name":"n","arguments":"{}"}`, BlockTypeToolCall},
		{`{"type":"tool-addition","toolName":"n"}`, BlockTypeToolAddition},
		{`{"type":"tool-removal","toolName":"n"}`, BlockTypeToolRemoval},
		{`{"type":"image","attachment":{"attachmentId":"sha256:a","mediaType":"image/gif","bytes":1,"width":1,"height":1,"name":"n","originalDimensions":{"width":2,"height":2}}}`, BlockTypeImage},
		{`{"type":"file","attachment":{"attachmentId":"sha256:a","name":"f","bytes":1}}`, BlockTypeFile},
	}
	for _, testCase := range cases {
		blocks, err := UnmarshalContent([]byte("[" + testCase.payload + "]"))
		if err != nil {
			t.Fatalf("UnmarshalContent(%s) error = %v", testCase.payload, err)
		}
		if len(blocks) != 1 || blocks[0].BlockType() != testCase.want {
			t.Fatalf("UnmarshalContent(%s) = %#v, want %s", testCase.payload, blocks, testCase.want)
		}
	}
}

// TestImageBlockKeepsOptionalFields pins that the image's optional display name
// and original dimensions survive a round trip rather than being dropped.
func TestImageBlockKeepsOptionalFields(t *testing.T) {
	payload := `[{"type":"image","attachment":{"attachmentId":"sha256:a","mediaType":"image/png","bytes":9,"width":4,"height":3,"name":"shot.png","originalDimensions":{"width":8,"height":6}}}]`
	blocks, err := UnmarshalContent([]byte(payload))
	if err != nil {
		t.Fatalf("UnmarshalContent() error = %v", err)
	}
	image, ok := blocks[0].(ImageBlock)
	if !ok {
		t.Fatalf("block type = %T, want ImageBlock", blocks[0])
	}
	if image.Attachment.Name != "shot.png" || image.Attachment.OriginalDimensions == nil {
		t.Fatalf("attachment = %+v, want the optional fields kept", image.Attachment)
	}
	if image.Offloaded != nil {
		t.Fatal("Offloaded set, want absent to stay absent")
	}
	encoded, err := MarshalContent(blocks)
	if err != nil {
		t.Fatalf("MarshalContent() error = %v", err)
	}
	if string(encoded) != payload {
		t.Fatalf("round trip changed bytes\nwant: %s\ngot:  %s", payload, encoded)
	}
}

// TestOffloadedImageRoundTrips pins the literal-true marker an offload decision
// sets, which a later projection reads to emit placeholder text.
func TestOffloadedImageRoundTrips(t *testing.T) {
	payload := `[{"type":"image","attachment":{"attachmentId":"sha256:a","mediaType":"image/webp","bytes":1,"width":1,"height":1},"offloaded":true}]`
	blocks, err := UnmarshalContent([]byte(payload))
	if err != nil {
		t.Fatalf("UnmarshalContent() error = %v", err)
	}
	if offloaded := blocks[0].(ImageBlock).Offloaded; offloaded == nil || !*offloaded {
		t.Fatal("Offloaded not true")
	}
	encoded, err := MarshalContent(blocks)
	if err != nil {
		t.Fatalf("MarshalContent() error = %v", err)
	}
	if string(encoded) != payload {
		t.Fatalf("round trip changed bytes\nwant: %s\ngot:  %s", payload, encoded)
	}
}

// TestMessageValidateCoversEveryRoleBranch pins each role's rule, including the
// tolerance for optional fields a role does not use: the reference stores them, so
// a codec that rejected them would refuse a record it must round-trip.
func TestMessageValidateCoversEveryRoleBranch(t *testing.T) {
	user := Message{ID: "m", Role: RoleUser, Source: MessageSource{Kind: SourceUser}}
	if err := user.Validate(); err != nil {
		t.Fatalf("user Validate() = %v, want nil", err)
	}
	withTool := user
	withTool.IsError = true
	withTool.ToolCallID = "c"
	if err := withTool.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want unused tool fields tolerated", err)
	}
	system := Message{ID: "m", Role: RoleSystem, Source: MessageSource{Kind: SourceSystemPrompt}}
	if err := system.Validate(); err != nil {
		t.Fatalf("system Validate() = %v, want nil", err)
	}
	assistant := Message{ID: "m", Role: RoleAssistant, Source: MessageSource{Kind: SourceModel, Provider: "p", Model: "m"}}
	if err := assistant.Validate(); err != nil {
		t.Fatalf("assistant Validate() = %v, want nil", err)
	}
	// The rule that stays strict: a tool message's two call identifiers must agree.
	mismatch := Message{ID: "m", Role: RoleTool, Source: MessageSource{Kind: SourceTool, CallID: "c2"}, ToolCallID: "c1"}
	if err := mismatch.Validate(); err == nil {
		t.Fatal("Validate() = nil, want mismatched tool call ids rejected")
	}
}
