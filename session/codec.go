package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
)

// envelopeKeys is the complete set of legal envelope field names. An event with
// any other own field is refused: an unrecognized field could change how the rest
// of the envelope is read, so it cannot be ignored.
var envelopeKeys = []string{"type", "seq", "time", "data", "ignorable", "surfaceOp", "sourceEventSeqs"}

// MarshalEvent encodes one event as a JSON envelope with deterministic field
// order.
//
// The field set is fixed and no map iteration decides the order, so encoding the
// same event twice yields the same bytes. The log compares and hashes those bytes,
// so a nondeterministic encoding would make replay unverifiable.
func MarshalEvent(event Event) ([]byte, error) {
	if err := ValidateEventEnvelope(event); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteByte('{')
	written := 0
	field := func(name string, value any) error {
		if written > 0 {
			out.WriteByte(',')
		}
		key, err := json.Marshal(name)
		if err != nil {
			return err
		}
		out.Write(key)
		out.WriteByte(':')
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		out.Write(encoded)
		written++
		return nil
	}
	if err := field("type", event.Type); err != nil {
		return nil, err
	}
	if err := field("seq", uint64(event.Seq)); err != nil {
		return nil, err
	}
	if err := field("time", int64(event.Time)); err != nil {
		return nil, err
	}
	if err := field("data", json.RawMessage(event.Data)); err != nil {
		return nil, err
	}
	if event.HasIgnorable || bool(event.Ignorable) {
		if err := field("ignorable", true); err != nil {
			return nil, err
		}
	}
	if event.SurfaceOp != nil {
		encoded, err := marshalSurfaceOp(*event.SurfaceOp)
		if err != nil {
			return nil, err
		}
		if written > 0 {
			out.WriteByte(',')
		}
		out.WriteString(`"surfaceOp":`)
		out.Write(encoded)
		written++
	}
	if event.SourceEventSeqs != nil {
		if err := field("sourceEventSeqs", event.SourceEventSeqs); err != nil {
			return nil, err
		}
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// marshalSurfaceOp encodes a surface operation as the exactly-sized object its
// variant requires.
func marshalSurfaceOp(op SurfaceOp) (json.RawMessage, error) {
	if err := op.Validate(); err != nil {
		return nil, err
	}
	if op.IsAppend() {
		return json.RawMessage(`"append"`), nil
	}
	return json.RawMessage(fmt.Sprintf(`{"op":"replace","startSeq":%d,"endSeq":%d}`, op.StartSeq, op.EndSeq)), nil
}

// UnmarshalEvent decodes one event envelope.
//
// Every legal envelope field is read into its own location and any other own field
// is refused, so a malformed or an aliased envelope fails at the read boundary
// instead of being half-interpreted. The payload stays raw.
func UnmarshalEvent(data []byte) (Event, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return Event{}, fmt.Errorf("%w: empty event envelope", ErrInvalidEvent)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return Event{}, fmt.Errorf("%w: envelope must be an object: %w", ErrInvalidEvent, err)
	}
	for name := range fields {
		if !isEnvelopeKey(name) {
			return Event{}, fmt.Errorf("%w: unknown envelope field %q", ErrInvalidEvent, name)
		}
	}
	var event Event
	raw, ok := fields["type"]
	if !ok {
		return Event{}, fmt.Errorf("%w: envelope requires a type", ErrInvalidEvent)
	}
	if err := json.Unmarshal(raw, &event.Type); err != nil {
		return Event{}, fmt.Errorf("%w: envelope type: %w", ErrInvalidEvent, err)
	}
	if event.Type == "" {
		return Event{}, fmt.Errorf("%w: envelope type is empty", ErrInvalidEvent)
	}
	if raw, ok := fields["seq"]; ok {
		var seq uint64
		if err := json.Unmarshal(raw, &seq); err != nil {
			return Event{}, fmt.Errorf("%w: envelope seq must be a non-negative integer", ErrInvalidEvent)
		}
		event.Seq = Seq(seq)
	} else {
		return Event{}, fmt.Errorf("%w: envelope requires a seq", ErrInvalidEvent)
	}
	if raw, ok := fields["time"]; ok {
		var when int64
		if err := json.Unmarshal(raw, &when); err != nil {
			return Event{}, fmt.Errorf("%w: envelope time must be a whole number of milliseconds", ErrInvalidEvent)
		}
		event.Time = TimeMS(when)
	} else {
		return Event{}, fmt.Errorf("%w: envelope requires a time", ErrInvalidEvent)
	}
	rawData, ok := fields["data"]
	if !ok {
		return Event{}, fmt.Errorf("%w: envelope requires data", ErrInvalidEvent)
	}
	if !json.Valid(rawData) {
		return Event{}, fmt.Errorf("%w: envelope data is not valid JSON", ErrInvalidEvent)
	}
	event.Data = append(json.RawMessage(nil), rawData...)
	if raw, ok := fields["ignorable"]; ok {
		var ignorable bool
		if err := json.Unmarshal(raw, &ignorable); err != nil {
			return Event{}, fmt.Errorf("%w: envelope ignorable must be a boolean", ErrInvalidEvent)
		}
		// The authoritative reference envelope check accepts the marker only as a
		// literal true. A written false is refused rather than read as "required",
		// so a producer cannot express the marker's absence in two ways that a
		// reader would have to reconcile.
		if !ignorable {
			return Event{}, fmt.Errorf("%w: envelope ignorable must be true when present", ErrInvalidEvent)
		}
		event.HasIgnorable = true
		event.Ignorable = true
	}
	if raw, ok := fields["surfaceOp"]; ok {
		op, err := unmarshalSurfaceOp(raw)
		if err != nil {
			return Event{}, err
		}
		event.SurfaceOp = &op
	}
	if raw, ok := fields["sourceEventSeqs"]; ok {
		var seqs []uint64
		if err := json.Unmarshal(raw, &seqs); err != nil {
			return Event{}, fmt.Errorf("%w: sourceEventSeqs must be an array of integers", ErrInvalidEvent)
		}
		event.SourceEventSeqs = make([]Seq, 0, len(seqs))
		for _, seq := range seqs {
			event.SourceEventSeqs = append(event.SourceEventSeqs, Seq(seq))
		}
	}
	return event, nil
}

// unmarshalSurfaceOp decodes one surface operation, requiring the exact key set of
// its variant. An aliased object with an extra or missing key is refused rather
// than interpreted leniently.
func unmarshalSurfaceOp(raw json.RawMessage) (SurfaceOp, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return SurfaceOp{}, fmt.Errorf("%w: surfaceOp: %w", ErrInvalidEvent, err)
		}
		if text != SurfaceAppend {
			return SurfaceOp{}, fmt.Errorf("%w: unknown surfaceOp %q", ErrInvalidEvent, text)
		}
		return SurfaceOp{Op: SurfaceAppend}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return SurfaceOp{}, fmt.Errorf("%w: surfaceOp must be a string or an object", ErrInvalidEvent)
	}
	if len(fields) != 3 {
		return SurfaceOp{}, fmt.Errorf("%w: replace surfaceOp needs exactly op, startSeq and endSeq", ErrInvalidEvent)
	}
	var op SurfaceOp
	for name, value := range fields {
		switch name {
		case "op":
			if err := json.Unmarshal(value, &op.Op); err != nil {
				return SurfaceOp{}, fmt.Errorf("%w: surfaceOp op: %w", ErrInvalidEvent, err)
			}
		case "startSeq":
			var seq uint64
			if err := json.Unmarshal(value, &seq); err != nil {
				return SurfaceOp{}, fmt.Errorf("%w: surfaceOp startSeq must be a non-negative integer", ErrInvalidEvent)
			}
			op.StartSeq = Seq(seq)
		case "endSeq":
			var seq uint64
			if err := json.Unmarshal(value, &seq); err != nil {
				return SurfaceOp{}, fmt.Errorf("%w: surfaceOp endSeq must be a non-negative integer", ErrInvalidEvent)
			}
			op.EndSeq = Seq(seq)
		default:
			return SurfaceOp{}, fmt.Errorf("%w: unknown surfaceOp field %q", ErrInvalidEvent, name)
		}
	}
	if op.Op != SurfaceReplace {
		return SurfaceOp{}, fmt.Errorf("%w: unknown surfaceOp %q", ErrInvalidEvent, op.Op)
	}
	return op, nil
}

// isEnvelopeKey reports whether name is one of the legal envelope fields.
func isEnvelopeKey(name string) bool {
	for _, legal := range envelopeKeys {
		if name == legal {
			return true
		}
	}
	return false
}

// MarshalLog encodes a whole log as a JSON array of envelopes, one per line-free
// element, in log order.
func MarshalLog(events []Event) ([]byte, error) {
	array := make([]json.RawMessage, 0, len(events))
	for index, event := range events {
		raw, err := MarshalEvent(event)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", index, err)
		}
		array = append(array, raw)
	}
	return json.Marshal(array)
}

// UnmarshalLog decodes a whole log, refusing a gap or an out-of-order sequence.
func UnmarshalLog(data []byte) ([]Event, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("%w: log must be an array of envelopes: %w", ErrInvalidEvent, err)
	}
	events := make([]Event, 0, len(raw))
	for index, item := range raw {
		event, err := UnmarshalEvent(item)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", index, err)
		}
		if event.Seq != Seq(index) {
			return nil, fmt.Errorf("%w: event at index %d has seq %d (expected %d); a log must be contiguous from 0",
				ErrSeqGap, index, event.Seq, index)
		}
		events = append(events, event)
	}
	return events, nil
}

// jsonLossless reports whether a payload survives a JSON round trip. The log is
// JSON, so a value that would change on the way out must be refused when it is
// written rather than discovered by a reader much later.
func jsonLossless(data []byte) bool {
	if len(bytes.TrimSpace(data)) == 0 {
		return true
	}
	var probe any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&probe); err != nil {
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return false
	}
	return true
}

// equalJSON compares two JSON values structurally, ignoring key order. It is used
// where a rule says "only content may change": the comparison must not be fooled
// by a reordering of the same fields.
func equalJSON(left, right []byte) bool {
	var leftValue, rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

// SortedSeqStrings is a diagnostic helper that renders sequence numbers in order.
func SortedSeqStrings(seqs []Seq) []string {
	sorted := append([]Seq(nil), seqs...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left] < sorted[right] })
	result := make([]string, 0, len(sorted))
	for _, seq := range sorted {
		result = append(result, fmt.Sprint(seq))
	}
	return result
}
