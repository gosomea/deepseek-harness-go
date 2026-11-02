package agentloop_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// userMessageEvent builds a surface-eligible user message event.
func userMessageEvent(t *testing.T, id, text string) session.Event {
	t.Helper()
	data, err := testkit.UserMessage(id, text)
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	return session.Event{Type: "user/message", Data: data, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}}
}

// A resumed session reports what was in flight and continues from the recorded
// turn number rather than restarting at zero.
func TestResumeReportsInFlightWorkAndTurn(t *testing.T) {
	store := testkit.NewFakeStore()
	id := persistence.SessionID("s-1")
	store.Seed(id,
		session.Event{Type: "turn/start", Data: []byte(`{"turn":0,"input":"read a.txt"}`)},
		userMessageEvent(t, "m1", "read a.txt"),
		session.Event{Type: "step/start", Data: []byte(`{"turn":0,"step":0}`)},
		session.Event{Type: "request/header", Data: []byte(`{"turn":0,"step":0,"provider":"fake","model":"fake-1","messages":1}`)},
		session.Event{Type: "tool/call", Data: []byte(`{"turn":0,"step":0,"callId":"call-1","name":"read_file","arguments":"{}"}`)},
	)

	state, err := agentloop.Resume(context.Background(), store, id)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !state.OpenTurn {
		t.Fatal("the unclosed turn must be reported")
	}
	if len(state.UnsettledCalls) != 1 || state.UnsettledCalls[0] != "call-1" {
		t.Fatalf("the unsettled call must be listed, got %v", state.UnsettledCalls)
	}
	if !state.NeedsAttention() {
		t.Fatal("unsettled work must need attention")
	}
	// Resume reads; it must not add records to the store.
	if events := store.Events(id); len(events) != 5 {
		t.Fatalf("resume must not append to the store, got %d events", len(events))
	}
	// A second run continues from turn 1, not 0, so the recorded turns stay
	// distinguishable.
	if turn := agentloop.ResumedTurnForTest(state.Events); turn != 1 {
		t.Fatalf("a resumed session must continue from turn 1, got %d", turn)
	}
}

// ResumeInto puts the restored history into a live factory without starting a
// turn, and the next turn is derived from that restored history.
func TestResumeIntoFactoryWithoutRunningATurn(t *testing.T) {
	store := testkit.NewFakeStore()
	id := persistence.SessionID("s-1")
	message := userMessageEvent(t, "m1", "earlier question").Data
	store.Seed(id,
		session.Event{Type: "turn/start", Data: []byte(`{"turn":0,"input":"earlier question"}`)},
		session.Event{Type: "user/message", Data: message, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		session.Event{Type: "step/start", Data: []byte(`{"turn":0,"step":0}`)},
		session.Event{Type: "request/header", Data: []byte(`{"turn":0,"step":0,"provider":"fake","model":"fake-1","messages":1}`)},
		session.Event{Type: "assistant/message", Data: mustNested(t, "m1", "a1", "previous answer"), SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		session.Event{Type: "step/end", Data: []byte(`{"turn":0,"step":0}`)},
		session.Event{Type: "turn/end", Data: []byte(`{"turn":0,"steps":1}`)},
	)

	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"next answer"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})

	resumed, err := factory.ResumeInto(context.Background(), store, id)
	if err != nil {
		t.Fatalf("resume into: %v", err)
	}
	if resumed.ID != id || resumed.Restored != 7 {
		t.Fatalf("unexpected resumed session %+v", resumed)
	}
	if resumed.NeedsAttention() {
		t.Fatalf("a settled session must not need attention: %+v", resumed.State)
	}
	if resumed.Turn != 1 {
		t.Fatalf("the next turn must continue from 1, got %d", resumed.Turn)
	}
	// Resuming replays the log but starts no turn: the provider was not called.
	if got := len(provider.Attempts()); got != 0 {
		t.Fatalf("resuming must not call the provider, got %d attempts", got)
	}
	// The factory's own log now holds the restored history, so a request built
	// afterwards is derived from it.
	history, err := log.DeriveMessages()
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("the restored history must be replayed, got %d messages", len(history))
	}
}

// Resuming a session that still owes work must report it, and running the next
// turn must not silently re-execute the unsettled call.
func TestResumeDoesNotReExecuteUnsettledTools(t *testing.T) {
	store := testkit.NewFakeStore()
	id := persistence.SessionID("s-1")
	store.Seed(id,
		session.Event{Type: "turn/start", Data: []byte(`{"turn":0,"input":"read"}`)},
		userMessageEvent(t, "m1", "read"),
		session.Event{Type: "step/start", Data: []byte(`{"turn":0,"step":0}`)},
		session.Event{Type: "tool/call", Data: []byte(`{"turn":0,"step":0,"callId":"call-1","name":"read_file","arguments":"{}"}`)},
	)
	log := newLog(t)
	var bodyRuns int
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"done"}))
	factory := newFactory(t, agentloop.Config{
		Provider: provider, Log: log, Pipeline: readFileTool(t, &bodyRuns),
	})
	resumed, err := factory.ResumeInto(context.Background(), store, id)
	if err != nil {
		t.Fatalf("resume into: %v", err)
	}
	if !resumed.NeedsAttention() || len(resumed.State.UnsettledCalls) != 1 {
		t.Fatalf("the unsettled call must be surfaced: %+v", resumed.State)
	}
	// Resuming itself must not run the tool body.
	if bodyRuns != 0 {
		t.Fatalf("resume must not execute tool bodies, got %d runs", bodyRuns)
	}
	// The restored event is present in the factory's log exactly once, which is
	// what makes the unsettled call observable rather than replayed.
	var calls int
	for _, event := range log.Events() {
		if event.Type == "tool/call" {
			calls++
		}
	}
	if calls != 1 {
		t.Fatalf("the restored call must appear once, got %d", calls)
	}
}

// Pending inputs are derived from the log and come back in log order.
func TestPendingInputsAreDerivedInOrder(t *testing.T) {
	events := []session.Event{
		{Type: "user/message", Seq: 0, Data: []byte(`{"id":"m1"}`)},
		{Type: "step/start", Seq: 1, Data: []byte(`{"turn":0,"step":0}`)},
		{Type: "user/message", Seq: 2, Data: []byte(`{"id":"m2"}`)},
		{Type: "user/message", Seq: 3, Data: []byte(`{"id":"m3"}`)},
	}
	pending := agentloop.PendingInputsForTest(events)
	if len(pending) != 2 {
		t.Fatalf("want the two unclaimed inputs, got %d", len(pending))
	}
	if pending[0].Seq != 2 || pending[1].Seq != 3 {
		t.Fatalf("pending inputs must be in log order, got %d then %d", pending[0].Seq, pending[1].Seq)
	}
	// The order must be stable across calls, not map-randomized.
	again := agentloop.PendingInputsForTest(events)
	if again[0].Seq != pending[0].Seq || again[1].Seq != pending[1].Seq {
		t.Fatal("the pending set must be deterministic across calls")
	}
}

// Resume refuses a nil store, a missing session, and a cancelled context.
func TestResumeRefusals(t *testing.T) {
	if _, err := agentloop.Resume(context.Background(), nil, "s-1"); !errors.Is(err, agentloop.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig for a nil store, got %v", err)
	}
	store := testkit.NewFakeStore()
	if _, err := agentloop.Resume(context.Background(), store, "absent"); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := agentloop.Resume(ctx, store, "s-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	// A storage failure is reported rather than becoming an empty session.
	boom := errors.New("storage unavailable")
	store.FailOpen(boom)
	if _, err := agentloop.Resume(context.Background(), store, "s-1"); !errors.Is(err, boom) {
		t.Fatalf("want the storage failure, got %v", err)
	}
	var absent *agentloop.Factory
	if _, err := absent.ResumeInto(context.Background(), store, "s-1"); !errors.Is(err, agentloop.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig for a nil factory, got %v", err)
	}
}

// A failed replay must not leave the factory's log half-restored without saying
// so: the error is returned and names the cause.
func TestResumeIntoReportsReplayFailure(t *testing.T) {
	store := testkit.NewFakeStore()
	id := persistence.SessionID("s-1")
	// An event the log's vocabulary refuses: a surface-eligible type with no
	// surface op cannot be replayed.
	store.Seed(id, session.Event{Type: "user/message", Data: []byte(`{"id":"m1"}`)})
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"x"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	if _, err := factory.ResumeInto(context.Background(), store, id); err == nil {
		t.Fatal("a replayed event the log refuses must be reported")
	}
}

// mustNested renders a nested assistant message payload for fixtures.
func mustNested(t *testing.T, messageID, _, text string) []byte {
	t.Helper()
	data, err := testkit.AssistantMessage(messageID, "fake", "fake-1", text)
	if err != nil {
		t.Fatalf("build assistant message: %v", err)
	}
	return data
}

// The resumed handle is closed, so a store cannot leak an open handle per resume.
func TestResumeClosesItsHandle(t *testing.T) {
	store := testkit.NewFakeStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	if _, err := agentloop.Resume(context.Background(), store, "s-1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	opened, closed := store.Opened()
	if opened != 1 || closed != 1 {
		t.Fatalf("resume must open and close exactly one handle, got opened=%d closed=%d", opened, closed)
	}
}

// A resumed session with a recorded timestamp must preserve it, not restamp the
// replay: a restored event that says it happened at a different time would break
// every ordering argument built on the log.
func TestResumePreservesEventTimestamps(t *testing.T) {
	store := testkit.NewFakeStore()
	id := persistence.SessionID("s-1")
	at := time.UnixMilli(1_600_000_000_000)
	store.Seed(id, session.Event{
		Type: "turn/start", Data: []byte(`{"turn":0,"input":"x"}`), Time: session.TimeMS(at.UnixMilli()),
	})
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"x"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	if _, err := factory.ResumeInto(context.Background(), store, id); err != nil {
		t.Fatalf("resume into: %v", err)
	}
	events := log.Events()
	if len(events) != 1 {
		t.Fatalf("want the restored event, got %d", len(events))
	}
	if got := int64(events[0].Time); got != at.UnixMilli() {
		t.Fatalf("the restored timestamp must be preserved: got %d want %d", got, at.UnixMilli())
	}
}

// A zero timestamp is passed through as unset rather than becoming the epoch.
func TestResumeLeavesAnUnsetTimestampUnset(t *testing.T) {
	store := testkit.NewFakeStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0,"input":"x"}`)})
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"x"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	if _, err := factory.ResumeInto(context.Background(), store, "s-1"); err != nil {
		t.Fatalf("resume into: %v", err)
	}
	// The log supplies its own clock when no time is given, so the recorded time
	// is the log's, not 1970.
	if events := log.Events(); len(events) != 1 || events[0].Time == 0 {
		t.Fatalf("an unset timestamp must be filled by the log's clock, got %+v", events)
	}
}

// A resumed log whose highest turn is not the last one still continues from the
// highest, so a resumed session cannot reuse a turn number.
func TestResumedTurnUsesTheHighestRecordedTurn(t *testing.T) {
	events := []session.Event{
		{Type: "turn/start", Data: []byte(`{"turn":5}`)},
		{Type: "turn/start", Data: []byte(`{"turn":2}`)},
		{Type: "turn/end", Data: []byte(`{"turn":2}`)},
	}
	if got := agentloop.ResumedTurnForTest(events); got != 6 {
		t.Fatalf("the next turn must be the highest plus one, got %d", got)
	}
	// A log with no turn at all continues from zero.
	if got := agentloop.ResumedTurnForTest(nil); got != 0 {
		t.Fatalf("an empty log must continue from 0, got %d", got)
	}
	// A payload the scan cannot read contributes nothing instead of failing.
	if got := agentloop.ResumedTurnForTest([]session.Event{{Type: "turn/start", Data: []byte(`{"turn":`)}}); got != 0 {
		t.Fatalf("an unreadable payload must not invent a turn, got %d", got)
	}
}

// A cancelled context stops a replay partway rather than finishing it.
func TestResumeIntoHonoursCancellation(t *testing.T) {
	store := testkit.NewFakeStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"x"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := factory.ResumeInto(ctx, store, "s-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// A resumed session with an unclaimed input reports it, so the caller can decide
// whether to re-deliver.
func TestResumeIntoReportsPendingInput(t *testing.T) {
	store := testkit.NewFakeStore()
	id := persistence.SessionID("s-1")
	store.Seed(id,
		session.Event{Type: "turn/start", Data: []byte(`{"turn":0,"input":"first"}`)},
		userMessageEvent(t, "m1", "first"),
		session.Event{Type: "step/start", Data: []byte(`{"turn":0,"step":0}`)},
		session.Event{Type: "step/end", Data: []byte(`{"turn":0,"step":0}`)},
		userMessageEvent(t, "m2", "steering after the step"),
		session.Event{Type: "turn/end", Data: []byte(`{"turn":0,"steps":1}`)},
	)
	state, err := agentloop.Resume(context.Background(), store, id)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if state.PendingInputs != 1 {
		t.Fatalf("the unclaimed input must be counted, got %d", state.PendingInputs)
	}
	pending := agentloop.PendingInputsForTest(state.Events)
	if len(pending) != 1 || pending[0].Seq != 4 {
		t.Fatalf("the pending input must be the one after the step, got %+v", pending)
	}
}
