package llm

import (
	"fmt"
	"strings"
)

// ChunkType is the discriminator of one stream chunk.
//
// This vocabulary follows the reference's raw streaming protocol: block
// indexes correlate interleaved deltas, `block-end` carries the assembled
// block, adapters emit usage before the terminal finish and nothing afterward.
type ChunkType string

// The stream chunk discriminators this package understands.
const (
	// ChunkBlockStart opens a block at an index.
	ChunkBlockStart ChunkType = "block-start"
	// ChunkTextDelta appends text to the block at an index.
	ChunkTextDelta ChunkType = "text-delta"
	// ChunkReasoningDelta appends reasoning text to the block at an index.
	ChunkReasoningDelta ChunkType = "reasoning-delta"
	// ChunkToolCallDelta appends to a tool call's id, name or arguments.
	ChunkToolCallDelta ChunkType = "tool-call-delta"
	// ChunkBlockEnd closes a block and carries the authoritative value.
	ChunkBlockEnd ChunkType = "block-end"
	// ChunkUsage reports token accounting for the attempt.
	ChunkUsage ChunkType = "usage"
	// ChunkFinish is the terminal chunk: exactly one ends a stream.
	ChunkFinish ChunkType = "finish"
)

// FinishKind says why a model response stopped.
type FinishKind string

// The finish reasons this package understands.
const (
	// FinishStop means the model stopped on its own.
	FinishStop FinishKind = "stop"
	// FinishToolCalls means the model requested one or more tool calls.
	FinishToolCalls FinishKind = "tool-calls"
	// FinishMaxTokens means the output cap was reached.
	FinishMaxTokens FinishKind = "max-tokens"
	// FinishAborted means the caller cancelled the attempt.
	FinishAborted FinishKind = "aborted"
	// FinishError means the attempt failed.
	FinishError FinishKind = "error"
)

// FinishReason says why an attempt stopped, with the failure facts when the
// stop was not a success.
type FinishReason struct {
	// Kind is the reason the response ended.
	Kind FinishKind `json:"kind"`
	// Failure describes an aborted or failed attempt. It is empty for the
	// successful kinds, and required for the failing ones.
	Failure LlmFailure `json:"failure,omitempty"`
}

// Validate reports whether the reason is complete. A failing kind without
// failure facts would reach policy with nothing to route on, so it is refused
// at the boundary that produced it.
func (r FinishReason) Validate() error {
	switch r.Kind {
	case FinishStop, FinishToolCalls, FinishMaxTokens:
		return nil
	case FinishAborted, FinishError:
		return r.Failure.Validate()
	case "":
		return fmt.Errorf("%w: finish requires a kind", ErrInvalidValue)
	default:
		// A provider-specific reason is a real value: the reference makes the
		// finish map merge-extensible so an adapter can surface its own kind.
		// Refusing it here would make the vocabulary unable to store what an
		// adapter legitimately reports.
		return nil
	}
}

// StreamChunk is one chunk of a model stream.
//
// It is a struct with a type discriminator rather than one interface per kind
// because the reference's chunk map is merge-extensible: an adapter may emit a
// kind this build does not know, and a reader must preserve it. Fields that do
// not apply to the chunk's type are ignored rather than cleared, so a chunk
// survives a round trip through a reader that understands fewer kinds.
type StreamChunk struct {
	// Type is the chunk discriminator.
	Type ChunkType `json:"type"`
	// Index is the block index deltas correlate on.
	Index int `json:"index"`
	// BlockType names the block kind a block-start opens.
	BlockType ContentBlockType `json:"blockType,omitempty"`
	// Text is the appended text of a text or reasoning delta.
	Text string `json:"text,omitempty"`
	// ID is the tool call id a tool-call delta carries.
	ID ToolCallID `json:"id,omitempty"`
	// Name is the tool name a tool-call delta carries. A pointer distinguishes
	// "the provider did not name it" from "the provider named it empty", which
	// the reference also distinguishes.
	Name *string `json:"name,omitempty"`
	// ArgumentsDelta is the raw JSON fragment a tool-call delta appends.
	ArgumentsDelta string `json:"argumentsDelta,omitempty"`
	// Block is the authoritative assembled block a block-end carries.
	Block ContentBlock `json:"-"`
	// Usage is the token accounting a usage chunk reports.
	Usage *Usage `json:"usage,omitempty"`
	// Reason is why an attempt stopped, on the terminal finish chunk.
	Reason *FinishReason `json:"reason,omitempty"`
}

// Validate reports whether the chunk is structurally usable.
//
// The rules are the ones an assembler relies on: indexes are non-negative, a
// delta carries the field its type appends to, and a finish chunk carries a
// valid reason. A chunk whose type this build does not know is preserved rather
// than refused, matching the merge-extensible chunk map.
func (c StreamChunk) Validate() error {
	if c.Index < 0 {
		return fmt.Errorf("%w: chunk index must not be negative, got %d", ErrInvalidValue, c.Index)
	}
	switch c.Type {
	case ChunkBlockStart:
		if strings.TrimSpace(string(c.BlockType)) == "" {
			return fmt.Errorf("%w: block-start requires a blockType", ErrInvalidValue)
		}
		return nil
	case ChunkTextDelta, ChunkReasoningDelta:
		return nil
	case ChunkToolCallDelta:
		if c.ID.IsZero() && c.Name == nil && c.ArgumentsDelta == "" {
			return fmt.Errorf("%w: tool-call-delta carries no id, name or arguments", ErrInvalidValue)
		}
		return nil
	case ChunkBlockEnd:
		if c.Block == nil {
			return fmt.Errorf("%w: block-end requires the assembled block", ErrInvalidValue)
		}
		return c.Block.Validate()
	case ChunkUsage:
		if c.Usage == nil {
			return fmt.Errorf("%w: usage chunk requires usage", ErrInvalidValue)
		}
		return c.Usage.Validate()
	case ChunkFinish:
		if c.Reason == nil {
			return fmt.Errorf("%w: finish chunk requires a reason", ErrInvalidValue)
		}
		return c.Reason.Validate()
	case "":
		return fmt.Errorf("%w: chunk requires a type", ErrInvalidValue)
	default:
		return nil
	}
}

// IsTerminal reports whether the chunk ends the stream. An attempt has exactly
// one terminal chunk, which is what lets a consumer distinguish "the stream
// ended" from "the stream was cut off".
func (c StreamChunk) IsTerminal() bool { return c.Type == ChunkFinish }

// Stream is one model attempt's chunk source.
//
// Recv reports io.EOF-style exhaustion through its boolean rather than an
// error, and a non-nil error always ends the stream. The caller reads until the
// boolean is false; a stream that ends without a terminal finish chunk is an
// incomplete attempt, which the caller must treat as failed rather than as an
// empty success.
type Stream interface {
	// Recv returns the next chunk. The boolean is false when the stream is
	// exhausted, and then the error is either nil or the reason it ended.
	Recv() (StreamChunk, bool, error)
	// Close releases the stream. It is safe to call more than once and after a
	// stream has already ended.
	Close() error
}

// Finish returns the terminal reason of a completed chunk sequence, reporting
// whether one was seen.
//
// It exists so that "the provider never finished" is an explicit answer instead
// of a zero value a caller might mistake for a successful stop.
func Finish(chunks []StreamChunk) (FinishReason, bool) {
	for index := len(chunks) - 1; index >= 0; index-- {
		if chunks[index].IsTerminal() {
			return *chunks[index].Reason, true
		}
	}
	return FinishReason{}, false
}
