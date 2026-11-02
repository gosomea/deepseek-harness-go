package agentloop

import (
	"context"
	"fmt"
	"time"

	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// intField reads one integer field from an event payload.
//
// It is a tolerant reader: a payload this build cannot parse contributes nothing,
// which is the right answer for a turn-number scan and avoids making resume fail
// because some unrelated producer added a field.
func intField(data []byte, field string) int {
	needle := `"` + field + `":`
	index := 0
	for {
		found := indexOfFrom(data, needle, index)
		if found < 0 {
			return -1
		}
		start := found + len(needle)
		end := start
		for end < len(data) && data[end] >= '0' && data[end] <= '9' {
			end++
		}
		if end > start {
			value := 0
			for _, digit := range data[start:end] {
				value = value*10 + int(digit-'0')
			}
			return value
		}
		index = start
	}
}

// indexOfFrom finds needle in haystack at or after from.
func indexOfFrom(haystack []byte, needle string, from int) int {
	for index := from; index+len(needle) <= len(haystack); index++ {
		if string(haystack[index:index+len(needle)]) == needle {
			return index
		}
	}
	return -1
}

// resumedTurn is the turn number a session continues from.
//
// The turn number is derived from the restored log rather than from a counter
// that a crash would have reset: two runs of the same session must not both
// call their first turn "turn 0", because the recorded events would then be
// ambiguous about which run they belong to.
func resumedTurn(events []session.Event) int {
	turn := -1
	for _, event := range events {
		if event.Type != "turn/start" {
			continue
		}
		if value := intField(event.Data, "turn"); value > turn {
			turn = value
		}
	}
	return turn + 1
}

// Resume reconstructs the driver state for a session that was already recorded.
//
// It answers the question an application must answer before continuing: what was
// in flight when the process stopped? The answer is a description, not an action
// — nothing here executes a tool or replays a request, because a crash may have
// happened after a side effect and re-running it would duplicate the effect.
func Resume(ctx context.Context, store persistence.Store, id persistence.SessionID) (persistence.ResumeState, error) {
	if store == nil {
		return persistence.ResumeState{}, fmt.Errorf("%w: resume requires a store", ErrInvalidConfig)
	}
	handle, err := store.Open(ctx, persistence.OpenOptions{ID: id, Access: persistence.AccessRead})
	if err != nil {
		return persistence.ResumeState{}, err
	}
	defer func() { _ = handle.Close() }()
	state, err := persistence.Resume(ctx, handle)
	if err != nil {
		return persistence.ResumeState{}, err
	}
	// The caller is told explicitly which offsets it is resuming from, so it can
	// check the assumption against its own view instead of trusting a bare
	// success.
	if err := ctx.Err(); err != nil {
		return persistence.ResumeState{}, err
	}
	return state, nil
}

// ResumeInto restores a session's state into a live Factory.
//
// It is the step that turns "the log was restored" into "an Agent is ready":
// the Factory gets the replayed events, so its log holds the same history the
// store does, and the caller receives the state describing what is still owed.
// It deliberately does not start a turn.
func (f *Factory) ResumeInto(ctx context.Context, store persistence.Store, id persistence.SessionID) (*ResumedSession, error) {
	if f == nil {
		return nil, fmt.Errorf("%w: resume requires a factory", ErrInvalidConfig)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: resume requires a store", ErrInvalidConfig)
	}
	handle, err := store.Open(ctx, persistence.OpenOptions{ID: id, Access: persistence.AccessRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = handle.Close() }()
	state, err := persistence.Resume(ctx, handle)
	if err != nil {
		return nil, err
	}
	// The restored events are replayed into the factory's own log so that a
	// request built after the resume is derived from the same history the store
	// holds. Replaying is safe because it appends records, not side effects.
	restored, err := f.replay(ctx, state.Events)
	if err != nil {
		return nil, err
	}
	return &ResumedSession{
		ID:       id,
		State:    state,
		Restored: restored,
		Turn:     resumedTurn(state.Events),
	}, nil
}

// ResumedSession is what a resume produced.
type ResumedSession struct {
	// ID is the resumed session.
	ID persistence.SessionID
	// State describes what was in flight.
	State persistence.ResumeState
	// Restored counts the events replayed into the factory's log.
	Restored int
	// Turn is the turn number the next turn continues from.
	Turn int
}

// NeedsAttention reports whether the resumed session has work a caller must
// decide about before running another turn.
func (s ResumedSession) NeedsAttention() bool { return s.State.NeedsAttention() }

// replay copies restored events into the factory's log.
//
// Each event's payload is re-appended through the log's own Append, so the log's
// validation runs on it: a restored record the current build cannot represent is
// reported here instead of surfacing later as a malformed request.
func (f *Factory) replay(ctx context.Context, events []session.Event) (int, error) {
	target := f.Config().Log
	if target == nil {
		return 0, fmt.Errorf("%w: factory has no log to resume into", ErrInvalidConfig)
	}
	replayed := 0
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return replayed, err
		}
		// The log stores the payload, not the event envelope: re-marshalling the
		// whole event would nest an envelope inside a payload, and the fold would
		// then fail with a message error that points at the wrong thing.
		if _, err := target.Append(session.AppendInput{
			Type:            event.Type,
			Data:            append([]byte(nil), event.Data...),
			Time:            eventTime(event),
			Ignorable:       event.HasIgnorable,
			SourceEventSeqs: event.SourceEventSeqs,
			SurfaceOp:       event.SurfaceOp,
		}); err != nil {
			return replayed, err
		}
		replayed++
	}
	return replayed, nil
}

// eventTime returns the event's timestamp in the form Append expects.
//
// A zero timestamp is passed through as "unset" rather than being turned into
// the zero instant: the log distinguishes the two, and inventing a time would
// make a replayed event look like it happened in 1970.
func eventTime(event session.Event) *time.Time {
	if event.Time == 0 {
		return nil
	}
	// TimeMS is a Unix millisecond count, so the instant is rebuilt from it
	// rather than stored beside it: one representation, no drift.
	value := time.UnixMilli(int64(event.Time))
	return &value
}

// PendingInputs returns the user-message events a restored log recorded but never
// claimed, so an application can decide whether to re-deliver them.
//
// It reads the log instead of trusting a counter: the set must be derivable from
// the record, or a crash between recording an input and counting it would lose
// the input. It returns the events rather than agent.Input values because the
// caller has to decide how a restored message becomes a new input — re-sending
// the original text may not be the right choice for a resumed session.
func PendingInputs(events []session.Event) []session.Event {
	claimed := map[session.Seq]bool{}
	inputs := map[session.Seq]session.Event{}
	for _, event := range events {
		switch event.Type {
		case "user/message":
			inputs[event.Seq] = event
		case "step/start":
			// A step claims every input recorded before it.
			for seq := range inputs {
				claimed[seq] = true
			}
		}
	}
	pending := make([]session.Event, 0, len(inputs))
	for seq, event := range inputs {
		if claimed[seq] {
			continue
		}
		pending = append(pending, event)
	}
	// A stable order, so two runs of the same log deliver the same sequence.
	sortEventsBySeq(pending)
	return pending
}

// sortEventsBySeq orders events by their log position.
//
// A map iteration is deliberately randomized in Go, so without this the pending
// set would come back in a different order on every call — the kind of
// nondeterminism that turns a reproducible resume into an intermittent bug.
func sortEventsBySeq(events []session.Event) {
	for index := 1; index < len(events); index++ {
		for scan := index; scan > 0 && events[scan].Seq < events[scan-1].Seq; scan-- {
			events[scan], events[scan-1] = events[scan-1], events[scan]
		}
	}
}

// ResumedTurnForTest reports the turn a restored log continues from.
//
// It exists so a test can assert the continuation point without duplicating the
// scan, which would let the test and the implementation drift apart.
func ResumedTurnForTest(events []session.Event) int { return resumedTurn(events) }

// PendingInputsForTest exposes the pending-input derivation to tests.
func PendingInputsForTest(events []session.Event) []session.Event { return PendingInputs(events) }
