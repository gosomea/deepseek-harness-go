package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/session"
)

// ErrNoCheckpoint reports a resume attempted on a session with no checkpoint.
var ErrNoCheckpoint = errors.New("persistence: session has no checkpoint")

// ResumeState is what a reopened session knows about work that was in flight.
//
// It exists because "the log was restored" and "the work was resumed" are
// different questions. A crash can leave a tool call requested but never
// settled, and the honest answer is to surface that rather than to guess whether
// the side effect happened.
type ResumeState struct {
	// ID is the session that was reopened.
	ID SessionID
	// Events is the restored log.
	Events []session.Event
	// UnsettledCalls lists tool call ids that were requested without a result.
	//
	// They are reported rather than re-executed: the process may have died
	// before, during, or after the tool body ran, and re-running an unknown
	// side effect is exactly the mistake this field exists to prevent.
	UnsettledCalls []string
	// OpenTurn reports whether the last turn never closed.
	OpenTurn bool
	// PendingInputs holds user messages that were recorded but never claimed by
	// a step, reconstructed from the log's own boundaries.
	PendingInputs int
}

// NeedsAttention reports whether the resumed session has work a caller must
// decide about before continuing.
func (s ResumeState) NeedsAttention() bool {
	return len(s.UnsettledCalls) > 0 || s.OpenTurn
}

// Resume reopens a session and reconstructs what was in flight when it stopped.
//
// It reads the committed prefix only: a torn tail is not silently included, and
// the caller repairs first if it wants those bytes considered. Nothing is
// re-executed here — the returned state describes, it does not act.
func Resume(ctx context.Context, handle Handle) (ResumeState, error) {
	if handle == nil {
		return ResumeState{}, fmt.Errorf("%w: resume requires a handle", ErrInvalidValue)
	}
	events, err := handle.Read(ctx, 0)
	if err != nil {
		return ResumeState{}, err
	}
	state := ResumeState{ID: handle.ID(), Events: events}
	state.UnsettledCalls, state.OpenTurn, state.PendingInputs = analyze(events)
	return state, nil
}

// analyze walks the restored log and classifies what was in flight.
//
// It reads only the event envelope and its type, not the payload grammar: the
// question "was this call settled" is answerable from the type sequence, and
// depending on payload fields here would make resume break whenever a producer
// added one.
func analyze(events []session.Event) (unsettled []string, openTurn bool, pending int) {
	requested := map[string]bool{}
	settled := map[string]bool{}
	turnDepth := 0
	claimed := map[string]bool{}

	for _, event := range events {
		switch event.Type {
		case "turn/start":
			turnDepth++
		case "turn/end":
			if turnDepth > 0 {
				turnDepth--
			}
		case "tool/call":
			if id := stringField(event.Data, "callId"); id != "" {
				requested[id] = true
			}
		case "tool/result":
			if id := stringField(event.Data, "callId"); id != "" {
				settled[id] = true
			}
		case "user/message":
			if id := stringField(event.Data, "id"); id != "" {
				claimed[id] = false
			}
		case "step/start":
			// A step claims every user message recorded before it, which is the
			// shape the driver writes: the batch is recorded as user messages and
			// then a step consumes them.
			for id := range claimed {
				claimed[id] = true
			}
		}
	}
	for id, wasRequested := range requested {
		if wasRequested && !settled[id] {
			unsettled = append(unsettled, id)
		}
	}
	sortStrings(unsettled)
	for _, wasClaimed := range claimed {
		if !wasClaimed {
			pending++
		}
	}
	return unsettled, turnDepth > 0, pending
}

// stringField reads one string field from an event payload.
//
// It is a tolerant reader rather than a decoder: resume must not fail because a
// producer wrote a field this build does not use, and a payload it cannot read
// simply contributes nothing to the analysis.
func stringField(data []byte, field string) string {
	needle := `"` + field + `":"`
	start := indexOf(data, needle)
	if start < 0 {
		return ""
	}
	rest := data[start+len(needle):]
	end := indexOf(rest, `"`)
	if end < 0 {
		return ""
	}
	return string(rest[:end])
}

// indexOf is a small substring search so this file needs no extra import.
func indexOf(haystack []byte, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if string(haystack[index:index+len(needle)]) == needle {
			return index
		}
	}
	return -1
}

// sortStrings sorts in place; a tiny insertion sort keeps the dependency list
// empty for a handful of call ids.
func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for scan := index; scan > 0 && values[scan] < values[scan-1]; scan-- {
			values[scan], values[scan-1] = values[scan-1], values[scan]
		}
	}
}

// Checkpoint is the durable marker a resumed session starts from.
type Checkpoint struct {
	// ID is the session the checkpoint belongs to.
	ID SessionID
	// Seq is the next offset a writer should append at.
	Seq session.Seq
	// Complete reports whether the checkpoint covers the whole committed log.
	Complete bool
}

// CheckpointEventType is the boundary event a checkpoint is recorded as.
//
// It reuses the vocabulary's existing seed-boundary type rather than inventing a
// new one: the log refuses an unknown required event by design, so a checkpoint
// written under a fresh name would be rejected by every reader that has not been
// taught it. The payload carries the offset the checkpoint covers.
const CheckpointEventType = "session/end-seed"

// WriteCheckpoint records how far a session has been made durable.
//
// It appends a boundary event rather than writing a side file: the fact belongs
// to the session's own history, and a side file could be lost or become
// inconsistent with the log it describes. Flush follows the append, because a
// checkpoint that is not durable is not a checkpoint.
func WriteCheckpoint(ctx context.Context, handle Handle, at session.Seq) (Checkpoint, error) {
	if handle == nil {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint requires a handle", ErrInvalidValue)
	}
	if !handle.Access().CanWrite() {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint requires a writing handle", ErrReadOnly)
	}
	event, err := handle.Append(ctx, session.AppendInput{
		Type: CheckpointEventType,
		Data: []byte(fmt.Sprintf(`{"seq":%d}`, at)),
	})
	if err != nil {
		return Checkpoint{}, err
	}
	if err := handle.Flush(ctx); err != nil {
		return Checkpoint{}, err
	}
	return Checkpoint{ID: handle.ID(), Seq: event.Seq + 1, Complete: true}, nil
}

// ReadCheckpoint returns the most recent checkpoint in the log.
func ReadCheckpoint(ctx context.Context, handle Handle) (Checkpoint, error) {
	if handle == nil {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint requires a handle", ErrInvalidValue)
	}
	events, err := handle.Read(ctx, 0)
	if err != nil {
		return Checkpoint{}, err
	}
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type != CheckpointEventType {
			continue
		}
		return Checkpoint{ID: handle.ID(), Seq: events[index].Seq + 1, Complete: true}, nil
	}
	return Checkpoint{}, fmt.Errorf("%w: %s", ErrNoCheckpoint, handle.ID())
}

// NestMessage renders a message payload in the form the log expects for the four
// nested message-bearing event types (`system`/`developer`/`assistant`/`tool
// result`): the message object sits under "message", with room for the producer's
// own fields beside it.
//
// It is exported because a caller that writes those events through a handle must
// use this shape: the surface reader decodes it that way, and a payload written
// flat would be refused or would derive to nothing.
func NestMessage(message json.RawMessage) ([]byte, error) {
	if len(message) == 0 {
		return nil, fmt.Errorf("%w: nesting requires a message", ErrInvalidValue)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(message, &decoded); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	return json.Marshal(map[string]json.RawMessage{"message": message})
}
