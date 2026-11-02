package llm

import (
	"errors"
	"fmt"
	"sort"
)

// ErrIncompleteStream reports a stream that ended without a terminal finish
// chunk. The attempt produced chunks, but the provider never said it was done,
// so the assembled message must not be treated as a successful answer.
var ErrIncompleteStream = errors.New("llm: stream ended without a terminal finish")

// ErrToolCallNotClosed reports a tool call whose arguments were still streaming
// when the attempt ended. A half-received argument string is not a usable call,
// so it is refused rather than executed.
var ErrToolCallNotClosed = errors.New("llm: tool call arguments were not completed")

// partialBlock is the accumulator for one block index.
type partialBlock struct {
	blockType ContentBlockType
	text      string
	callID    ToolCallID
	name      string
	hasName   bool
	arguments string
	// closed records that block-end froze this block. Deltas arriving after it
	// are ignored so a misbehaving adapter cannot corrupt or grow a completed
	// block.
	closed bool
	block  ContentBlock
}

// Assembler turns a chunk sequence into content blocks and a finished message.
//
// It is the single assembly algorithm: the agent loop feeds it while logging
// the raw chunks, then reads the assembled result. Two properties matter and
// are tested: interleaved tool calls are bucketed by call id so their argument
// fragments never mix, and a block closed by block-end is frozen so later
// deltas cannot change it.
//
// The zero value is not usable; call NewAssembler.
type Assembler struct {
	// partials holds one accumulator per block index.
	partials map[int]*partialBlock
	// order records first-seen indexes so block order is the stream's own order
	// rather than Go map order.
	order []int
	// usage accumulates every reported usage chunk.
	usage UsageDelta
	// finish is the terminal reason, once seen.
	finish *FinishReason
}

// NewAssembler returns an empty assembler.
func NewAssembler() *Assembler {
	return &Assembler{partials: make(map[int]*partialBlock)}
}

// Push feeds one chunk into the assembly state.
//
// A chunk that fails its own validation is refused before it can alter state,
// so a malformed chunk cannot leave the assembler half-updated.
func (a *Assembler) Push(chunk StreamChunk) error {
	if err := chunk.Validate(); err != nil {
		return err
	}
	switch chunk.Type {
	case ChunkBlockStart:
		partial := a.partial(chunk.Index)
		// A second block-start for the same index must not reset accumulated
		// text: the reference ignores the duplicate rather than discarding work.
		if partial.blockType == "" {
			partial.blockType = chunk.BlockType
		}
		return nil
	case ChunkTextDelta:
		partial := a.partial(chunk.Index)
		if partial.closed {
			return nil
		}
		partial.text += chunk.Text
		if partial.blockType == "" {
			partial.blockType = BlockTypeText
		}
		return nil
	case ChunkReasoningDelta:
		partial := a.partial(chunk.Index)
		if partial.closed {
			return nil
		}
		partial.text += chunk.Text
		if partial.blockType == "" {
			partial.blockType = BlockTypeReasoning
		}
		return nil
	case ChunkToolCallDelta:
		partial := a.partial(chunk.Index)
		if partial.closed {
			return nil
		}
		if partial.blockType == "" {
			partial.blockType = BlockTypeToolCall
		}
		// The id is taken from the first delta that carries one. A later
		// differing id is ignored rather than silently rebinding accumulated
		// arguments to another call.
		if partial.callID.IsZero() && !chunk.ID.IsZero() {
			partial.callID = chunk.ID
		}
		if chunk.Name != nil && !partial.hasName {
			partial.name = *chunk.Name
			partial.hasName = true
		}
		partial.arguments += chunk.ArgumentsDelta
		return nil
	case ChunkBlockEnd:
		partial := a.partial(chunk.Index)
		partial.closed = true
		partial.block = chunk.Block
		return nil
	case ChunkUsage:
		a.usage.Add(*chunk.Usage)
		return nil
	case ChunkFinish:
		if a.finish != nil {
			// A second terminal chunk is a protocol violation. The first one
			// wins, so an adapter cannot rewrite how the attempt ended.
			return fmt.Errorf("%w: stream already finished with %s", ErrInvalidValue, a.finish.Kind)
		}
		reason := *chunk.Reason
		a.finish = &reason
		return nil
	default:
		// An unknown chunk kind is preserved by the stream layer and ignored
		// here: the assembler cannot interpret a block it does not know.
		return nil
	}
}

// partial returns the accumulator for an index, creating it in stream order.
func (a *Assembler) partial(index int) *partialBlock {
	if partial, ok := a.partials[index]; ok {
		return partial
	}
	partial := &partialBlock{}
	a.partials[index] = partial
	a.order = append(a.order, index)
	return partial
}

// Blocks returns the assembled content blocks in stream order.
//
// A block frozen by block-end is authoritative: its carried value is returned
// unchanged. A block never closed is still returned, assembled from its deltas,
// because a cancelled attempt's partial output is real and must be inspectable.
func (a *Assembler) Blocks() []ContentBlock {
	indexes := append([]int(nil), a.order...)
	sort.Ints(indexes)
	blocks := make([]ContentBlock, 0, len(indexes))
	for _, index := range indexes {
		partial := a.partials[index]
		if partial.block != nil {
			blocks = append(blocks, partial.block)
			continue
		}
		switch partial.blockType {
		case BlockTypeText:
			blocks = append(blocks, TextBlock{Text: partial.text})
		case BlockTypeReasoning:
			blocks = append(blocks, ReasoningBlock{Text: partial.text})
		case BlockTypeToolCall:
			blocks = append(blocks, ToolCallBlock{ID: partial.callID, Name: partial.name, Arguments: partial.arguments})
		default:
			// An index that only ever saw block-start with an unknown type has
			// no assembleable value, so it contributes nothing rather than an
			// empty text block that a model would see.
		}
	}
	return blocks
}

// Usage returns the accumulated usage and whether anything was reported.
func (a *Assembler) Usage() (Usage, bool) { return a.usage.Total() }

// FinishReason returns the terminal reason, reporting whether one was seen.
func (a *Assembler) FinishReason() (FinishReason, bool) {
	if a.finish == nil {
		return FinishReason{}, false
	}
	return *a.finish, true
}

// Closed reports whether a terminal finish chunk was seen.
func (a *Assembler) Closed() bool { return a.finish != nil }

// Assemble reads a whole stream and returns its blocks, usage and finish reason.
//
// It is the consumer the agent loop uses. The stream is always closed. A stream
// that ends without a terminal finish chunk returns ErrIncompleteStream along
// with whatever was assembled, so a caller can show partial output while still
// refusing to treat the attempt as successful.
func (a *Assembler) Assemble(stream Stream) ([]ContentBlock, error) {
	defer func() { _ = stream.Close() }()
	for {
		chunk, ok, err := stream.Recv()
		if err != nil {
			return a.Blocks(), err
		}
		if !ok {
			break
		}
		if err := a.Push(chunk); err != nil {
			return a.Blocks(), err
		}
	}
	if !a.Closed() {
		return a.Blocks(), ErrIncompleteStream
	}
	if reason, _ := a.FinishReason(); reason.Kind == FinishAborted || reason.Kind == FinishError {
		return a.Blocks(), &FailureError{Failure: reason.Failure}
	}
	return a.Blocks(), nil
}

// OpenToolCalls returns the tool calls that are still streaming arguments when
// the attempt ended, in block order.
//
// It exists because "the model stopped mid-call" must be detected before
// execution: running a tool with half an argument string would produce a real
// side effect from a truncated request.
func (a *Assembler) OpenToolCalls() []ToolCallID {
	var open []ToolCallID
	for _, index := range a.order {
		partial := a.partials[index]
		if partial.blockType != BlockTypeToolCall || partial.closed {
			continue
		}
		open = append(open, partial.callID)
	}
	return open
}

// Message builds the assistant message for the assembled attempt.
//
// It refuses to mint a message when the stream was incomplete, when a tool call
// was left open, or when the attempt failed: an unfinished attempt must not
// become a successful message, which is the failure mode the reference forbids.
func (a *Assembler) Message(provider, model string) (Message, error) {
	reason, ok := a.FinishReason()
	if !ok {
		return Message{}, ErrIncompleteStream
	}
	if open := a.OpenToolCalls(); len(open) > 0 {
		return Message{}, fmt.Errorf("%w: %v", ErrToolCallNotClosed, open)
	}
	switch reason.Kind {
	case FinishAborted, FinishError:
		return Message{}, &FailureError{Failure: reason.Failure}
	}
	return NewAssistantMessage(provider, model, a.Blocks())
}

// AssembleMessage is the one-call form used by the loop and by examples: it
// drains the stream, then builds the message.
func AssembleMessage(stream Stream, provider, model string) (Message, []ContentBlock, Usage, bool, error) {
	assembler := NewAssembler()
	blocks, err := assembler.Assemble(stream)
	usage, reported := assembler.Usage()
	if err != nil {
		return Message{}, blocks, usage, reported, err
	}
	message, err := assembler.Message(provider, model)
	return message, blocks, usage, reported, err
}
