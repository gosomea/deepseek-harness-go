package session

import (
	"encoding/json"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// SurfaceReplacement records one positional replacement observed while folding a log.
type SurfaceReplacement struct {
	// Seq is the position of the event that replaced the range.
	Seq Seq
	// Start is the declared inclusive start of the replaced range.
	Start Seq
	// End is the declared inclusive end of the replaced range.
	End Seq
	// Shadowed lists the surface positions the operation actually removed, in
	// surface order. It can differ from the declared range when the range's
	// interior contains positions that were themselves replaced earlier.
	Shadowed []Seq
}

// Surface is the result of folding a log's surface operations.
type Surface struct {
	// Nodes lists the current message-producing positions in model-visible order.
	Nodes []Seq
	// Replacements lists the positional replacements in event order.
	Replacements []SurfaceReplacement
	// ReplaceGeneration counts committed positional replacements.
	ReplaceGeneration uint64
	// ContentGeneration counts replacements plus plugin-owned content changes. It
	// is the sole invalidation signal a derived-message cache has.
	ContentGeneration uint64
	// projected holds each interpreter's replacement messages by position.
	projected map[Seq]json.RawMessage
}

// FoldSurface replays a whole log through the canonical surface fold.
//
// This is the offline path: it derives the surface from a complete event slice, so
// a reader that has a log can reconstruct model history without any live state.
// The incremental path used by a live log shares every rule with this one.
//
// Rules the fold enforces, all of them coming from the reference:
//   - positions must be dense from the base, so a gap is refused;
//   - only the five message-producing types may carry surface metadata, and they
//     must carry it, while every other type must not;
//   - a replacement's endpoints must be *current* surface nodes, not merely
//     existing ones, and must be strictly earlier than the replacing event;
//   - a replacement must cite every node it shadows;
//   - a tool-result replacement may change only message content;
//   - surface node 0 is protected while it holds the system prompt;
//   - an event type that requires an interpreter is refused without one.
func FoldSurface(events []Event, projections ...MessageProjection) (*Surface, error) {
	surface := &Surface{projected: map[Seq]json.RawMessage{}}
	for index, event := range events {
		if event.Seq != Seq(index) {
			return nil, fmt.Errorf("%w: event at index %d has seq %d (expected %d)",
				ErrSeqGap, index, event.Seq, index)
		}
		if err := surface.apply(event, events, 0, projections); err != nil {
			return nil, err
		}
	}
	return surface, nil
}

// apply folds one event into the surface.
func (s *Surface) apply(event Event, window []Event, baseSeq Seq, projections []MessageProjection) error {
	if err := ValidateEventEnvelope(event); err != nil {
		return err
	}
	// An unknown record marked ignorable is kept for the audit trail and never
	// changes history, even when it carries opaque metadata that would otherwise
	// look like a surface placement. This is the only path where metadata on an
	// unrecognized type is tolerated, and tolerating it here is what keeps it from
	// affecting the surface.
	if !KnownTypes().Knows(event.Type) && event.Ignorable.IsSet() && !IsSurfaceEligibleType(event.Type) {
		return nil
	}
	projection := findProjection(projections, event.Type)
	if projection != nil {
		messages, err := projection.Project(event, ProjectionContext{
			Nodes:   append([]Seq(nil), s.Nodes...),
			Events:  window,
			BaseSeq: baseSeq,
		})
		if err != nil {
			return err
		}
		if err := validateProjectionOutput(messages); err != nil {
			return err
		}
		for seq, message := range messages {
			s.projected[seq] = message
		}
		s.ContentGeneration++
		return nil
	}
	if RequiresProjection(event.Type) {
		return fmt.Errorf("%w: %q requires a message projection; register its interpreter",
			ErrInvalidEvent, event.Type)
	}
	if event.SurfaceOp == nil {
		// A log-only event lengthens the log without touching the surface. This is
		// the only place the two are deliberately allowed to diverge.
		return nil
	}
	if event.SurfaceOp.IsAppend() {
		s.Nodes = append(s.Nodes, event.Seq)
		return nil
	}
	op := *event.SurfaceOp
	startIdx := indexOfSeq(s.Nodes, op.StartSeq)
	if startIdx < 0 {
		return fmt.Errorf("%w: surface replace: start seq %d not found in surface", ErrInvalidEvent, op.StartSeq)
	}
	endIdx := indexOfSeq(s.Nodes, op.EndSeq)
	if endIdx < 0 {
		return fmt.Errorf("%w: surface replace: end seq %d not found in surface", ErrInvalidEvent, op.EndSeq)
	}
	if startIdx > endIdx {
		return fmt.Errorf("%w: surface replace: start seq %d (index %d) is after end seq %d (index %d)",
			ErrInvalidEvent, op.StartSeq, startIdx, op.EndSeq, endIdx)
	}
	shadowed := append([]Seq(nil), s.Nodes[startIdx:endIdx+1]...)
	if err := validateSources(event, shadowed); err != nil {
		return err
	}
	if err := checkToolResultRewrite(event, shadowed, window, baseSeq); err != nil {
		return err
	}
	if err := checkSystemHeadRewrite(event, s.Nodes, startIdx, window, baseSeq); err != nil {
		return err
	}
	replacement := make([]Seq, 0, len(s.Nodes)-(endIdx-startIdx+1)+1)
	replacement = append(replacement, s.Nodes[:startIdx]...)
	replacement = append(replacement, event.Seq)
	replacement = append(replacement, s.Nodes[endIdx+1:]...)
	s.Nodes = replacement
	s.ReplaceGeneration++
	s.ContentGeneration++
	s.Replacements = append(s.Replacements, SurfaceReplacement{
		Seq: event.Seq, Start: op.StartSeq, End: op.EndSeq, Shadowed: shadowed,
	})
	return nil
}

// findProjection returns the interpreter registered for eventType, or nil.
func findProjection(projections []MessageProjection, eventType string) MessageProjection {
	for _, projection := range projections {
		if projection.Type() == eventType {
			return projection
		}
	}
	return nil
}

// validateProjectionOutput checks that an interpreter returned messages for
// positions that already exist on the surface.
//
// A projection rewrites content; it cannot invent a position. Allowing it to would
// let an event add model history without declaring a surface placement, which is
// the one guarantee the surface exists to provide.
func validateProjectionOutput(messages map[Seq]json.RawMessage) error {
	for seq, message := range messages {
		if len(message) == 0 {
			return fmt.Errorf("%w: projection returned an empty message for seq %d", ErrInvalidEvent, seq)
		}
		if _, err := llm.UnmarshalMessage(message); err != nil {
			return fmt.Errorf("%w: projection message for seq %d is not a message: %w", ErrInvalidEvent, seq, err)
		}
	}
	return nil
}

// checkToolResultRewrite restricts a tool-result replacement to the content of
// exactly one current result: it may change what a result says, never which call it
// answers or whether it failed.
func checkToolResultRewrite(event Event, shadowed []Seq, window []Event, baseSeq Seq) error {
	if event.Type != "tool/result" {
		return nil
	}
	if len(shadowed) != 1 {
		return fmt.Errorf("%w: tool/result surface replacement must rewrite exactly one current node", ErrInvalidEvent)
	}
	original, ok := eventInWindow(window, baseSeq, shadowed[0])
	if !ok || original.Type != "tool/result" {
		return fmt.Errorf("%w: tool/result surface replacement must target a current tool/result", ErrInvalidEvent)
	}
	originalRest, err := withoutMessageContent(original.Data)
	if err != nil {
		return err
	}
	replacementRest, err := withoutMessageContent(event.Data)
	if err != nil {
		return err
	}
	if !equalJSON(originalRest, replacementRest) {
		return fmt.Errorf("%w: tool/result surface replacement may change only content", ErrInvalidEvent)
	}
	return nil
}

// checkSystemHeadRewrite protects the system prompt at surface node 0: a
// replacement covering it must itself be a system message over exactly that node,
// so a compaction range cannot silently erase the instructions.
func checkSystemHeadRewrite(event Event, nodes []Seq, startIdx int, window []Event, baseSeq Seq) error {
	if startIdx != 0 || len(nodes) == 0 {
		return nil
	}
	head, ok := eventInWindow(window, baseSeq, nodes[0])
	if !ok || head.Type != "system/message" {
		return nil
	}
	if event.Type != "system/message" || !event.SurfaceOp.IsReplace() {
		return fmt.Errorf("%w: surface replace: node 0 holds the system prompt and may be rewritten only by a system/message over exactly that node",
			ErrInvalidEvent)
	}
	if event.SurfaceOp.StartSeq != nodes[0] || event.SurfaceOp.EndSeq != nodes[0] {
		return fmt.Errorf("%w: surface replace: node 0 holds the system prompt and may be rewritten only by a system/message over exactly that node",
			ErrInvalidEvent)
	}
	return nil
}

// indexOfSeq locates one sequence in a surface node list.
func indexOfSeq(nodes []Seq, seq Seq) int {
	for index, node := range nodes {
		if node == seq {
			return index
		}
	}
	return -1
}

// eventInWindow resolves one position against a window that starts at baseSeq.
func eventInWindow(window []Event, baseSeq Seq, seq Seq) (Event, bool) {
	if seq < baseSeq {
		return Event{}, false
	}
	index := seq - baseSeq
	if index >= Seq(len(window)) {
		return Event{}, false
	}
	return window[index], true
}

// withoutMessageContent returns an event payload with the nested message's content
// removed, so two payloads can be compared "ignoring content".
func withoutMessageContent(data json.RawMessage) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("%w: event data is not an object: %w", ErrInvalidEvent, err)
	}
	if inner, ok := fields["message"]; ok {
		var message map[string]json.RawMessage
		if err := json.Unmarshal(inner, &message); err != nil {
			return nil, fmt.Errorf("%w: message is not an object: %w", ErrInvalidEvent, err)
		}
		message["content"] = nil
		delete(message, "content")
		encoded, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		fields["message"] = encoded
	}
	return json.Marshal(fields)
}

// DeriveMessages rebuilds model history from the surface.
//
// Only surface nodes produce messages, and a node whose content is empty produces
// none: an empty system, developer or assistant message keeps its place in the log
// and derives to nothing, which is how "no system prompt" and a max-tokens step are
// recorded without injecting an empty turn into the provider transcript.
//
// Replacement messages from interpreters win over the event's own content, so a
// projection's decision is what the model sees.
func (s *Surface) DeriveMessages(events []Event) ([]llm.Message, error) {
	messages := make([]llm.Message, 0, len(s.Nodes))
	for _, seq := range s.Nodes {
		event, ok := eventInWindow(events, 0, seq)
		if !ok {
			return nil, fmt.Errorf("%w: surface node %d is outside the supplied log", ErrInvalidEvent, seq)
		}
		message, err := s.DeriveEventMessage(event)
		if err != nil {
			return nil, err
		}
		if message == nil {
			continue
		}
		messages = append(messages, *message)
	}
	return messages, nil
}

// DeriveEventMessage projects one event to the model message it derives to, or nil
// when it produces none.
//
// A log-only event — a boundary marker, a failed attempt, an audit record —
// derives to nothing. That is the mechanism behind "the log grows while model
// history does not".
func (s *Surface) DeriveEventMessage(event Event) (*llm.Message, error) {
	if projected, ok := s.projected[event.Seq]; ok {
		message, err := llm.UnmarshalMessage(projected)
		if err != nil {
			return nil, fmt.Errorf("%w: projected message at seq %d: %w", ErrInvalidEvent, event.Seq, err)
		}
		return &message, nil
	}
	switch event.Type {
	case "user/message":
		return messageFromData(event.Data, false)
	case "system/message", "developer/message":
		return messageFromData(event.Data, true)
	case "assistant/message":
		return messageFromData(event.Data, true)
	case "tool/result":
		return messageFromData(event.Data, true)
	default:
		// Intentionally not exhaustive: turn and step boundaries, attempts and
		// errors are trace data, not history.
		return nil, nil
	}
}

// messageFromData reads the message out of an event payload.
//
// A user message carries the message object directly; the other four carry it
// under "message". When nested is true an empty message derives to no message, so
// an empty system prompt never injects an empty turn.
func messageFromData(data json.RawMessage, nested bool) (*llm.Message, error) {
	raw := data
	if nested {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, fmt.Errorf("%w: event data is not an object: %w", ErrInvalidEvent, err)
		}
		inner, ok := fields["message"]
		if !ok {
			return nil, fmt.Errorf("%w: event data has no message", ErrInvalidEvent)
		}
		raw = inner
	}
	message, err := llm.UnmarshalMessage(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: event message: %w", ErrInvalidEvent, err)
	}
	if len(message.Content) == 0 {
		return nil, nil
	}
	return &message, nil
}

// SurfaceNodes returns the model-visible positions of a log after folding it. It
// is the convenience entry a reader with a complete log uses.
func SurfaceNodes(events []Event, projections ...MessageProjection) ([]Seq, error) {
	surface, err := FoldSurface(events, projections...)
	if err != nil {
		return nil, err
	}
	return surface.Nodes, nil
}

// DeriveMessages folds a log and returns its model history in one step.
func DeriveMessages(events []Event, projections ...MessageProjection) ([]llm.Message, error) {
	surface, err := FoldSurface(events, projections...)
	if err != nil {
		return nil, err
	}
	return surface.DeriveMessages(events)
}
