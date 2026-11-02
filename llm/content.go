package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ContentBlockType is the discriminator of one content block. It is a plain
// string rather than an integer enum so that an unrecognized value from a newer
// writer stays readable and round-trips unchanged.
type ContentBlockType string

// The content block discriminators this package understands.
const (
	// BlockTypeText is visible text.
	BlockTypeText ContentBlockType = "text"
	// BlockTypeReasoning is thinking content, distinct from visible text.
	BlockTypeReasoning ContentBlockType = "reasoning"
	// BlockTypeImage is a durable raster image reference.
	BlockTypeImage ContentBlockType = "image"
	// BlockTypeFile is a durable verbatim file reference.
	BlockTypeFile ContentBlockType = "file"
	// BlockTypeToolCall is a model-requested tool invocation.
	BlockTypeToolCall ContentBlockType = "tool-call"
	// BlockTypeToolAddition activates a tool from a referenced request header.
	BlockTypeToolAddition ContentBlockType = "tool-addition"
	// BlockTypeToolRemoval records the removal of a tool by session-local name.
	BlockTypeToolRemoval ContentBlockType = "tool-removal"
)

// ContentBlock is one piece of message content.
//
// It is a sealed interface: the concrete types below are the only
// implementations, so a caller can switch on the concrete type and a decoder
// can reject a value it cannot classify. An unknown discriminator is not an
// error at this level — it becomes an UnknownBlock that preserves the original
// JSON.
type ContentBlock interface {
	// BlockType returns the block's discriminator.
	BlockType() ContentBlockType
	// Validate reports whether the block is a complete value.
	Validate() error
	// contentBlock seals the interface to this package.
	contentBlock()
}

// TextBlock is plain text visible to the end user.
type TextBlock struct {
	// Text is the visible text. It may be empty.
	Text string `json:"text"`
}

// BlockType returns BlockTypeText.
func (TextBlock) BlockType() ContentBlockType { return BlockTypeText }

// Validate reports whether the block is complete. Any text is acceptable,
// including the empty string.
func (TextBlock) Validate() error { return nil }

func (TextBlock) contentBlock() {}

// ReasoningBlock is reasoning content: what the model thought, kept separate
// from the text it decided to show.
type ReasoningBlock struct {
	// Text is the reasoning text. It may be empty.
	Text string `json:"text"`
}

// BlockType returns BlockTypeReasoning.
func (ReasoningBlock) BlockType() ContentBlockType { return BlockTypeReasoning }

// Validate reports whether the block is complete.
func (ReasoningBlock) Validate() error { return nil }

func (ReasoningBlock) contentBlock() {}

// ToolCallBlock is a tool invocation the model requested.
//
// Arguments stay a raw JSON string exactly as the model produced them. This
// package never parses them: decoding belongs to the adapter that knows the
// provider's dialect, and a malformed argument string must remain observable
// rather than being silently replaced.
type ToolCallBlock struct {
	// ID is the provider-issued call id that a result correlates with.
	ID ToolCallID `json:"id"`
	// Name is the requested tool name. The reference tolerates an empty name on
	// a partial stream, so this package does too; the assembler layer guarantees
	// a usable name before execution.
	Name string `json:"name"`
	// Arguments is the raw JSON string as produced by the model.
	Arguments string `json:"arguments"`
}

// BlockType returns BlockTypeToolCall.
func (ToolCallBlock) BlockType() ContentBlockType { return BlockTypeToolCall }

// Validate reports whether the block is structurally usable. An empty call id is
// accepted: the reference's stream assembler synthesizes one for a tolerant
// stream that never supplied it, so rejecting it here would refuse a record the
// reference stores. Result correlation is checked where the pairing is made.
func (b ToolCallBlock) Validate() error { return nil }

func (ToolCallBlock) contentBlock() {}

// ToolAdditionBlock activates one tool from a request header cited by the
// developer message that carries it.
type ToolAdditionBlock struct {
	// ToolName names exactly one tool in the referenced historical header.
	ToolName string `json:"toolName"`
}

// BlockType returns BlockTypeToolAddition.
func (ToolAdditionBlock) BlockType() ContentBlockType { return BlockTypeToolAddition }

// Validate reports whether the block is structurally usable. An empty tool name is
// accepted for the same reason as an empty call id: the reference enforces the
// name's meaning against a cited request header at its session boundary, not in
// the block vocabulary.
func (b ToolAdditionBlock) Validate() error { return nil }

func (ToolAdditionBlock) contentBlock() {}

// ToolRemovalBlock records the removal of a tool by its session-local name.
type ToolRemovalBlock struct {
	// ToolName is the session-local name of the removed tool.
	ToolName string `json:"toolName"`
}

// BlockType returns BlockTypeToolRemoval.
func (ToolRemovalBlock) BlockType() ContentBlockType { return BlockTypeToolRemoval }

// Validate reports whether the block is structurally usable. An empty tool name is
// accepted for the same reason as an empty call id: the reference enforces the
// name's meaning against a cited request header at its session boundary, not in
// the block vocabulary.
func (b ToolRemovalBlock) Validate() error { return nil }

func (ToolRemovalBlock) contentBlock() {}

// ImageMediaType is the media type of a durable image attachment.
type ImageMediaType string

// The image media types the vocabulary admits.
const (
	// MediaTypePNG is image/png.
	MediaTypePNG ImageMediaType = "image/png"
	// MediaTypeJPEG is image/jpeg.
	MediaTypeJPEG ImageMediaType = "image/jpeg"
	// MediaTypeWebP is image/webp.
	MediaTypeWebP ImageMediaType = "image/webp"
	// MediaTypeGIF is image/gif.
	MediaTypeGIF ImageMediaType = "image/gif"
)

// ImageDimensions is a width and height pair.
type ImageDimensions struct {
	// Width is the pixel width.
	Width int `json:"width"`
	// Height is the pixel height.
	Height int `json:"height"`
}

// ImageAttachmentRef is a durable raster image reference: content-addressed
// bytes plus the metadata a renderer needs. It is never a path or a URL, so a
// logged reference cannot point outside the store that owns the bytes.
type ImageAttachmentRef struct {
	// AttachmentID is the content address, conventionally "sha256:<hex>".
	AttachmentID string `json:"attachmentId"`
	// MediaType is the image's encoding.
	MediaType ImageMediaType `json:"mediaType"`
	// Bytes is the byte length of the referenced image.
	Bytes int `json:"bytes"`
	// Width is the intrinsic pixel width.
	Width int `json:"width"`
	// Height is the intrinsic pixel height.
	Height int `json:"height"`
	// Name is an optional display name supplied by the producer.
	Name string `json:"name,omitempty"`
	// OriginalDimensions is the pre-scaling size, when the producer scaled it.
	OriginalDimensions *ImageDimensions `json:"originalDimensions,omitempty"`
}

// Validate reports whether the reference is a representable value.
//
// It performs no identity or media-type check: the reference accepts a missing
// content address and any media type string at its storage boundary and refuses
// an unsupported encoding later, at the provider that cannot send it. A stricter
// Go rule here would make this codec fail to round-trip a record the reference
// stores, which is the opposite of the fidelity this package exists to provide.
func (r ImageAttachmentRef) Validate() error { return nil }

// FileAttachmentRef is a durable verbatim file reference.
//
// No provider ever receives the bytes: request assembly projects every
// occurrence to deterministic handle text, so the durable record keeps the
// structured reference while a model sees a name and a size.
type FileAttachmentRef struct {
	// AttachmentID is the content address, conventionally "sha256:<hex>".
	AttachmentID string `json:"attachmentId"`
	// Name is the file's display name.
	Name string `json:"name"`
	// Bytes is the byte length of the referenced file.
	Bytes int `json:"bytes"`
}

// Validate reports whether the reference is a representable value. See
// ImageAttachmentRef.Validate for why no identity check happens here.
func (r FileAttachmentRef) Validate() error { return nil }

// ImageBlock is a durable raster image reference in message content.
//
// The block is role-neutral so that a future route may put one in assistant
// content, but the adapters this project targets declare text-only output: in
// practice only user content carries an image.
type ImageBlock struct {
	// Attachment is the referenced image.
	Attachment ImageAttachmentRef `json:"attachment"`
	// Offloaded marks an image a durable offload decision replaced with
	// placeholder text. It is a pointer so that a written false round-trips as
	// false instead of being dropped: absent means "not offloaded", and this
	// package preserves what a writer actually recorded.
	Offloaded *bool `json:"offloaded,omitempty"`
}

// BlockType returns BlockTypeImage.
func (ImageBlock) BlockType() ContentBlockType { return BlockTypeImage }

// Validate reports whether the referenced attachment is complete.
func (b ImageBlock) Validate() error { return b.Attachment.Validate() }

func (ImageBlock) contentBlock() {}

// FileBlock is a durable verbatim file reference in user content.
type FileBlock struct {
	// Attachment is the referenced file.
	Attachment FileAttachmentRef `json:"attachment"`
}

// BlockType returns BlockTypeFile.
func (FileBlock) BlockType() ContentBlockType { return BlockTypeFile }

// Validate reports whether the referenced attachment is complete.
func (b FileBlock) Validate() error { return b.Attachment.Validate() }

func (FileBlock) contentBlock() {}

// UnknownBlock is a content block whose discriminator this build does not
// recognize.
//
// It exists so that vocabulary growth does not force a lossy rewrite: a log
// written by a newer producer survives a decode and re-encode unchanged, and a
// consumer that cannot interpret the block refuses it at the boundary that
// actually needs the semantics (a provider adapter) instead of the log turning
// it into text. The reference treats unknown block kinds exactly this way.
type UnknownBlock struct {
	// Type is the unrecognized discriminator.
	Type ContentBlockType
	// Raw is the complete original JSON object.
	Raw json.RawMessage
}

// BlockType returns the unrecognized discriminator.
func (b UnknownBlock) BlockType() ContentBlockType { return b.Type }

// Validate reports whether the preserved payload is a JSON object. An unknown
// block is otherwise accepted by definition: this package cannot judge a shape
// it does not know.
func (b UnknownBlock) Validate() error {
	if strings.TrimSpace(string(b.Type)) == "" {
		return fmt.Errorf("%w: unknown block requires a type", ErrInvalidValue)
	}
	if len(b.Raw) == 0 {
		return fmt.Errorf("%w: unknown block %q requires its original JSON", ErrInvalidValue, b.Type)
	}
	var probe map[string]any
	if err := json.Unmarshal(b.Raw, &probe); err != nil {
		return fmt.Errorf("%w: unknown block %q: %w", ErrInvalidValue, b.Type, err)
	}
	return nil
}

func (UnknownBlock) contentBlock() {}

// ValidateContent reports whether every block in the slice is complete. A nil
// slice is valid and means "no content"; recall that a message with no content
// derives to no model-visible message at all.
func ValidateContent(blocks []ContentBlock) error {
	for index, block := range blocks {
		if block == nil {
			return fmt.Errorf("%w: content block %d is nil", ErrInvalidValue, index)
		}
		if err := block.Validate(); err != nil {
			return fmt.Errorf("content block %d: %w", index, err)
		}
	}
	return nil
}

// TextContent is a convenience constructor for a single-text-block content
// slice. It returns an empty slice for empty text, matching the rule that a
// message with no content produces no model-visible message.
func TextContent(text string) []ContentBlock {
	if text == "" {
		return nil
	}
	return []ContentBlock{TextBlock{Text: text}}
}

// ContentText concatenates the text of every text block. It is a presentation
// helper for diagnostics and examples; it deliberately ignores reasoning and
// unknown blocks rather than flattening them into visible text.
func ContentText(blocks []ContentBlock) string {
	var out strings.Builder
	for _, block := range blocks {
		if text, ok := block.(TextBlock); ok {
			out.WriteString(text.Text)
		}
	}
	return out.String()
}
