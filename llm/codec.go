package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// contentBlockEnvelope is the wire shape of one content block. Every known block
// is decoded through this struct so that an unrecognized discriminator can fall
// back to UnknownBlock with its complete original JSON.
type contentBlockEnvelope struct {
	// Type is the block discriminator.
	Type ContentBlockType `json:"type"`
}

// MarshalContent encodes content blocks as a JSON array.
//
// Known blocks are encoded from their own fields. An UnknownBlock is emitted
// with its preserved payload verbatim, so a record written by a newer producer
// survives a decode and re-encode unchanged. The result is deterministic: field
// order follows each struct's definition and no map is involved.
func MarshalContent(blocks []ContentBlock) ([]byte, error) {
	if err := ValidateContent(blocks); err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return []byte("[]"), nil
	}
	array := make([]json.RawMessage, 0, len(blocks))
	for index, block := range blocks {
		raw, err := marshalBlock(block)
		if err != nil {
			return nil, fmt.Errorf("content block %d: %w", index, err)
		}
		array = append(array, raw)
	}
	return json.Marshal(array)
}

// marshalBlock encodes one block into its tagged JSON object.
//
// A known block is encoded from its own struct fields in declaration order with
// the discriminator leading, so the bytes are deterministic and the shape is
// defined by the Go type rather than by a map iteration.
func marshalBlock(block ContentBlock) (json.RawMessage, error) {
	if unknown, ok := block.(UnknownBlock); ok {
		return encodeUnknownBlock(unknown)
	}
	payload, err := json.Marshal(block)
	if err != nil {
		return nil, err
	}
	tag, err := json.Marshal(block.BlockType())
	if err != nil {
		return nil, err
	}
	return spliceType(payload, tag)
}

// encodeUnknownBlock re-emits a preserved payload with its discriminator.
//
// When the preserved object already carries the matching discriminator the
// original bytes are emitted verbatim, so an unknown block keeps its own key
// order and a round trip is byte-exact no matter who wrote it. Only a manually
// built block without the tag has one spliced in.
func encodeUnknownBlock(block UnknownBlock) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(block.Raw, &fields); err != nil {
		return nil, fmt.Errorf("%w: unknown block %q: %w", ErrInvalidValue, block.Type, err)
	}
	tag, err := json.Marshal(block.Type)
	if err != nil {
		return nil, err
	}
	if existing, ok := fields["type"]; ok && string(existing) == string(tag) {
		preserved := make(json.RawMessage, len(block.Raw))
		copy(preserved, block.Raw)
		return preserved, nil
	}
	return spliceType(block.Raw, tag)
}

// spliceType returns the object with a "type" field added or replaced in the
// leading position, leaving every other field's bytes and order untouched.
func spliceType(object json.RawMessage, tag json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(object)
	if len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}' {
		body := bytes.TrimSpace(trimmed[1 : len(trimmed)-1])
		for _, field := range splitFields(body) {
			name, value, ok := bytes.Cut(field, []byte(":"))
			if !ok {
				continue
			}
			if string(bytes.TrimSpace(name)) != `"type"` {
				continue
			}
			// A tagged object already carries a discriminator. Keep the declared
			// one, but place it first for a stable classification prefix.
			if string(bytes.TrimSpace(value)) == string(tag) && bytes.HasPrefix(body, []byte(`"type":`)) {
				return append(json.RawMessage(nil), trimmed...), nil
			}
			return buildTagged(tag, removeTypeField(body)), nil
		}
		return buildTagged(tag, body), nil
	}
	return nil, fmt.Errorf("%w: block must be a JSON object", ErrInvalidValue)
}

// splitFields splits an object body on top-level commas, ignoring commas nested
// inside arrays, objects or strings.
func splitFields(body []byte) [][]byte {
	var fields [][]byte
	depth, inString, escaped, start := 0, false, false, 0
	for index, char := range body {
		switch {
		case escaped:
			escaped = false
		case char == '\\' && inString:
			escaped = true
		case char == '"':
			inString = !inString
		case inString:
		case char == '{' || char == '[':
			depth++
		case char == '}' || char == ']':
			depth--
		case char == ',' && depth == 0:
			fields = append(fields, body[start:index])
			start = index + 1
		}
	}
	return append(fields, body[start:])
}

// removeTypeField drops one leading "type" field from an object body.
func removeTypeField(body []byte) []byte {
	fields := splitFields(body)
	var kept [][]byte
	skipped := false
	for _, field := range fields {
		name, _, ok := bytes.Cut(field, []byte(":"))
		if ok && !skipped && string(bytes.TrimSpace(name)) == `"type"` {
			skipped = true
			continue
		}
		kept = append(kept, field)
	}
	return bytes.Join(kept, []byte(","))
}

// buildTagged writes a tagged object with the discriminator first.
func buildTagged(tag json.RawMessage, body []byte) json.RawMessage {
	out := make([]byte, 0, len(body)+len(tag)+10)
	out = append(out, `{"type":`...)
	out = append(out, tag...)
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 {
		out = append(out, ',')
		out = append(out, trimmed...)
	}
	out = append(out, '}')
	return out
}

// marshalSortedFields encodes a field map with the named key leading and the
// remaining keys in name order, so encoding the same value twice yields the same
// bytes even though the map has no order of its own.
func marshalSortedFields(fields map[string]json.RawMessage, first string) (json.RawMessage, error) {
	names := make([]string, 0, len(fields))
	for name := range fields {
		if name != first {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	order := append([]string{first}, names...)
	out := make([]byte, 0, 64)
	out = append(out, '{')
	written := 0
	for _, name := range order {
		value, ok := fields[name]
		if !ok {
			continue
		}
		if written > 0 {
			out = append(out, ',')
		}
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		out = append(out, key...)
		out = append(out, ':')
		out = append(out, value...)
		written++
	}
	out = append(out, '}')
	return out, nil
}

// UnmarshalContent decodes a JSON array of content blocks.
//
// A recognized discriminator is decoded into its concrete type. An unrecognized
// one becomes an UnknownBlock that keeps the original object, so the caller can
// still re-encode the exact value it read. A nil or empty array decodes to no
// content.
func UnmarshalContent(data []byte) ([]ContentBlock, error) {
	trimmed, err := trimmedJSON(data)
	if err != nil {
		return nil, err
	}
	if len(trimmed) == 0 {
		return nil, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("%w: content must be an array: %w", ErrInvalidValue, err)
	}
	blocks := make([]ContentBlock, 0, len(raw))
	for index, item := range raw {
		block, err := decodeBlock(item)
		if err != nil {
			return nil, fmt.Errorf("content block %d: %w", index, err)
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

// decodeBlock decodes one content block from its tagged object.
func decodeBlock(data []byte) (ContentBlock, error) {
	var envelope contentBlockEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: content block must be an object: %w", ErrInvalidValue, err)
	}
	if envelope.Type == "" {
		return nil, fmt.Errorf("%w: content block requires a type", ErrInvalidValue)
	}
	var block ContentBlock
	switch envelope.Type {
	case BlockTypeText:
		block = &TextBlock{}
	case BlockTypeReasoning:
		block = &ReasoningBlock{}
	case BlockTypeToolCall:
		block = &ToolCallBlock{}
	case BlockTypeToolAddition:
		block = &ToolAdditionBlock{}
	case BlockTypeToolRemoval:
		block = &ToolRemovalBlock{}
	case BlockTypeImage:
		block = &ImageBlock{}
	case BlockTypeFile:
		block = &FileBlock{}
	default:
		// The original object is the value for an unknown block: keep a copy so a
		// later re-encode cannot be affected by the caller's buffer changing.
		preserved := make(json.RawMessage, len(data))
		copy(preserved, data)
		block = &UnknownBlock{Type: envelope.Type, Raw: preserved}
	}
	// Unmarshal into a fresh pointer, then return the value so a decoded block is
	// immutable once returned and two decodes compare by value, not address.
	if err := json.Unmarshal(data, block); err != nil {
		return nil, fmt.Errorf("%w: block %q: %w", ErrInvalidValue, envelope.Type, err)
	}
	if err := block.Validate(); err != nil {
		return nil, err
	}
	return derefBlock(block), nil
}

// derefBlock converts a pointer block into its value form. UnknownBlock already
// carries its preserved payload, so decoding it again would be a no-op.
func derefBlock(block ContentBlock) ContentBlock {
	switch typed := block.(type) {
	case *TextBlock:
		return *typed
	case *ReasoningBlock:
		return *typed
	case *ToolCallBlock:
		return *typed
	case *ToolAdditionBlock:
		return *typed
	case *ToolRemovalBlock:
		return *typed
	case *ImageBlock:
		return *typed
	case *FileBlock:
		return *typed
	case *UnknownBlock:
		return *typed
	default:
		return block
	}
}

// jsonMessage is the wire shape of one message. Content stays raw so that
// unknown blocks survive without this struct having to model them, and Source
// stays raw so a foreign source keeps its extra fields.
type jsonMessage struct {
	ID         MessageID       `json:"id"`
	Role       Role            `json:"role"`
	Content    json.RawMessage `json:"content"`
	Source     json.RawMessage `json:"source"`
	ToolCallID ToolCallID      `json:"toolCallId,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
}

// MarshalMessage encodes one message as a JSON object.
//
// Content and source are encoded through their own encoders so that unknown
// blocks and unknown source fields round-trip. The message is validated first:
// a value that cannot be reconstructed is rejected at the boundary that writes
// it rather than at the boundary that reads it.
func MarshalMessage(message Message) ([]byte, error) {
	if err := message.Validate(); err != nil {
		return nil, err
	}
	content, err := MarshalContent(message.Content)
	if err != nil {
		return nil, err
	}
	source, err := MarshalSource(message.Source)
	if err != nil {
		return nil, err
	}
	wire := jsonMessage{
		ID:         message.ID,
		Role:       message.Role,
		Content:    content,
		Source:     source,
		ToolCallID: message.ToolCallID,
		IsError:    message.IsError,
	}
	return json.Marshal(wire)
}

// UnmarshalMessage decodes one message from its JSON object.
func UnmarshalMessage(data []byte) (Message, error) {
	var wire jsonMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		return Message{}, fmt.Errorf("%w: message: %w", ErrInvalidValue, err)
	}
	content, err := UnmarshalContent(wire.Content)
	if err != nil {
		return Message{}, err
	}
	source, err := UnmarshalSource(wire.Source)
	if err != nil {
		return Message{}, err
	}
	message := Message{
		ID:         wire.ID,
		Role:       wire.Role,
		Content:    content,
		Source:     source,
		ToolCallID: wire.ToolCallID,
		IsError:    wire.IsError,
	}
	if err := message.Validate(); err != nil {
		return Message{}, err
	}
	return message, nil
}

// MarshalSource encodes producer attribution, keeping any fields this build does
// not recognize so that a foreign source survives a round trip.
func MarshalSource(source MessageSource) ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(source.Extra)+4)
	encode := func(name string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		fields[name] = raw
		return nil
	}
	if err := encode("kind", source.Kind); err != nil {
		return nil, err
	}
	if source.Provider != "" {
		if err := encode("provider", source.Provider); err != nil {
			return nil, err
		}
	}
	if source.Model != "" {
		if err := encode("model", source.Model); err != nil {
			return nil, err
		}
	}
	if !source.CallID.IsZero() {
		if err := encode("callId", source.CallID); err != nil {
			return nil, err
		}
	}
	for name, value := range source.Extra {
		if _, known := fields[name]; known {
			continue
		}
		if name == "kind" {
			continue
		}
		if err := encode(name, value); err != nil {
			return nil, err
		}
	}
	return marshalSortedFields(fields, "kind")
}

// UnmarshalSource decodes producer attribution. An unrecognized field is kept in
// Extra rather than dropped.
func UnmarshalSource(data []byte) (MessageSource, error) {
	trimmed, err := trimmedJSON(data)
	if err != nil {
		return MessageSource{}, err
	}
	if len(trimmed) == 0 {
		return MessageSource{}, fmt.Errorf("%w: message requires a source", ErrInvalidValue)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &all); err != nil {
		return MessageSource{}, fmt.Errorf("%w: source must be an object: %w", ErrInvalidValue, err)
	}
	var source MessageSource
	if raw, ok := all["kind"]; ok {
		if err := json.Unmarshal(raw, &source.Kind); err != nil {
			return MessageSource{}, fmt.Errorf("%w: source kind: %w", ErrInvalidValue, err)
		}
		delete(all, "kind")
	}
	if raw, ok := all["provider"]; ok {
		if err := json.Unmarshal(raw, &source.Provider); err != nil {
			return MessageSource{}, fmt.Errorf("%w: source provider: %w", ErrInvalidValue, err)
		}
		delete(all, "provider")
	}
	if raw, ok := all["model"]; ok {
		if err := json.Unmarshal(raw, &source.Model); err != nil {
			return MessageSource{}, fmt.Errorf("%w: source model: %w", ErrInvalidValue, err)
		}
		delete(all, "model")
	}
	if raw, ok := all["callId"]; ok {
		if err := json.Unmarshal(raw, &source.CallID); err != nil {
			return MessageSource{}, fmt.Errorf("%w: source callId: %w", ErrInvalidValue, err)
		}
		delete(all, "callId")
	}
	if len(all) > 0 {
		source.Extra = make(map[string]any, len(all))
		for name, raw := range all {
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return MessageSource{}, fmt.Errorf("%w: source field %q: %w", ErrInvalidValue, name, err)
			}
			source.Extra[name] = value
		}
	}
	if err := source.Validate(); err != nil {
		return MessageSource{}, err
	}
	return source, nil
}

// MarshalMessages encodes a message slice as a JSON array. The order is
// preserved and no message is reordered or dropped.
func MarshalMessages(messages []Message) ([]byte, error) {
	array := make([]json.RawMessage, 0, len(messages))
	for index, message := range messages {
		raw, err := MarshalMessage(message)
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", index, err)
		}
		array = append(array, raw)
	}
	return json.Marshal(array)
}

// UnmarshalMessages decodes a JSON array of messages.
func UnmarshalMessages(data []byte) ([]Message, error) {
	trimmed, err := trimmedJSON(data)
	if err != nil {
		return nil, err
	}
	if len(trimmed) == 0 {
		return nil, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("%w: messages must be an array: %w", ErrInvalidValue, err)
	}
	messages := make([]Message, 0, len(raw))
	for index, item := range raw {
		message, err := UnmarshalMessage(item)
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", index, err)
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// trimmedJSON rejects trailing content after the first JSON value and returns
// the value's bytes. A caller that feeds a decoder a whole document should not
// have a second value silently ignored.
func trimmedJSON(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var probe json.RawMessage
	if err := decoder.Decode(&probe); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidValue, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing content after the first JSON value", ErrInvalidValue)
	}
	return probe, nil
}
