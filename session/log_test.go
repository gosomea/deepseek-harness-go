package session

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixedClock returns a clock that advances one millisecond per call, so tests are
// deterministic without pretending the wall clock matters.
func fixedClock() func() time.Time {
	calls := 0
	return func() time.Time {
		calls++
		return time.UnixMilli(int64(1700000000000 + calls))
	}
}

// TestAppendAssignsDenseSequencesFromTheLogLength pins that a sequence number is a
// position, not a counter: the next sequence is always the current length.
func TestAppendAssignsDenseSequencesFromTheLogLength(t *testing.T) {
	log := NewLog(KnownTypes())
	for expected := 0; expected < 5; expected++ {
		if got := log.Seq(); got != Seq(expected) {
			t.Fatalf("Seq() = %d before append %d, want the log length", got, expected)
		}
		event, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{"turn":1}`)})
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if event.Seq != Seq(expected) {
			t.Fatalf("Append() seq = %d, want %d", event.Seq, expected)
		}
	}
	if log.Len() != 5 || log.Seq() != 5 {
		t.Fatalf("Len()=%d Seq()=%d, want 5 and 5", log.Len(), log.Seq())
	}
	// EventAt returns a detached copy.
	first, ok := log.EventAt(0)
	if !ok {
		t.Fatal("EventAt(0) = false, want the first event")
	}
	first.Data[2] = 'X'
	again, _ := log.EventAt(0)
	if string(again.Data) != `{"turn":1}` {
		t.Fatalf("EventAt data changed through the returned copy: %s", again.Data)
	}
	if _, ok := log.EventAt(99); ok {
		t.Fatal("EventAt(out of range) = true, want false")
	}
}

// TestRejectedAppendLeavesTheLogUnchanged pins the all-or-nothing rule: a failure
// happens before the event enters the slice, so the sequence does not advance and
// nothing is published.
func TestRejectedAppendLeavesTheLogUnchanged(t *testing.T) {
	log := NewLog(KnownTypes())
	published := 0
	if _, err := log.OnEvent(func(Event) { published++ }); err != nil {
		t.Fatalf("OnEvent() error = %v", err)
	}
	if _, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	before := log.Len()

	failures := []struct {
		name  string
		input AppendInput
		want  error
	}{
		{"unknown required type", AppendInput{Type: "future/event", Data: []byte(`{}`)}, ErrUnknownRequiredEvent},
		{"payload not JSON", AppendInput{Type: "turn/start", Data: []byte(`{`)}, ErrNonJSONData},
		{"surface event without a marker", AppendInput{Type: "user/message", Data: []byte(`{}`)}, ErrInvalidEvent},
		{"log-only event with a marker", AppendInput{Type: "turn/start", Data: []byte(`{}`), SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}, ErrInvalidEvent},
		{"empty type", AppendInput{Type: "", Data: []byte(`{}`)}, ErrInvalidEvent},
	}
	for _, failure := range failures {
		t.Run(failure.name, func(t *testing.T) {
			if _, err := log.Append(failure.input); !errors.Is(err, failure.want) {
				t.Fatalf("Append() = %v, want %v", err, failure.want)
			}
			if log.Len() != before || log.Seq() != Seq(before) {
				t.Fatalf("a rejected append changed the log: Len=%d", log.Len())
			}
			if published != before {
				t.Fatalf("a rejected append published: %d observers called, want %d", published, before)
			}
		})
	}
}

// TestUnknownEventPolicy pins the two-sided rule: an unknown type is refused unless
// it is marked ignorable, and a marked one is preserved verbatim.
func TestUnknownEventPolicy(t *testing.T) {
	log := NewLog(KnownTypes())
	if _, err := log.Append(AppendInput{Type: "future/note", Data: []byte(`{"x":1}`)}); !errors.Is(err, ErrUnknownRequiredEvent) {
		t.Fatalf("Append() = %v, want ErrUnknownRequiredEvent", err)
	}
	event, err := log.Append(AppendInput{Type: "future/note", Data: []byte(`{"x":1}`), Ignorable: true})
	if err != nil {
		t.Fatalf("Append(ignorable) error = %v", err)
	}
	if !event.Ignorable.IsSet() || !event.HasIgnorable {
		t.Fatalf("the ignorable marker was not recorded: %+v", event)
	}
	// The unknown payload survives a whole-log round trip.
	encoded, err := MarshalLog(log.Events())
	if err != nil {
		t.Fatalf("MarshalLog() error = %v", err)
	}
	decoded, err := UnmarshalLog(encoded)
	if err != nil {
		t.Fatalf("UnmarshalLog() error = %v", err)
	}
	if len(decoded) != 1 || decoded[0].Type != "future/note" || string(decoded[0].Data) != `{"x":1}` {
		t.Fatalf("decoded = %+v, want the unknown event preserved", decoded)
	}
	// A known log-only type does NOT get the opaque exemption.
	if _, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`), Ignorable: true, SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}); err == nil {
		t.Fatal("a known log-only event was allowed to carry surface metadata because it was marked ignorable")
	}
}

// TestAppendCopiesCallerOwnedInput pins that a committed fact cannot be rewritten
// through the slices the caller still holds.
func TestAppendCopiesCallerOwnedInput(t *testing.T) {
	log := NewLog(KnownTypes())
	if _, err := log.Append(AppendInput{Type: "user/message", Data: []byte(`{"a":1}`), SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	// append the first event's payload buffer, then mutate the original.
	data := []byte(`{"b":2}`)
	sources := []Seq{0}
	op := SurfaceOp{Op: SurfaceAppend}
	if _, err := log.Append(AppendInput{Type: "user/message", Data: data, SurfaceOp: &op, SourceEventSeqs: sources}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	data[3] = '9'
	sources[0] = 99
	op.Op = SurfaceReplace
	stored, _ := log.EventAt(1)
	if string(stored.Data) != `{"b":2}` {
		t.Fatalf("stored data changed after the caller mutated its buffer: %s", stored.Data)
	}
	if stored.SourceEventSeqs[0] != 0 {
		t.Fatal("stored citations changed after the caller mutated its slice")
	}
	if stored.SurfaceOp.Op != SurfaceAppend {
		t.Fatal("stored surface op changed after the caller mutated its value")
	}
}

// TestAppendIsNotReentrant pins that an observer appending during publication is
// refused rather than reordering the log.
func TestAppendIsNotReentrant(t *testing.T) {
	log := NewLog(KnownTypes())
	var reentrant error
	if _, err := log.OnEvent(func(Event) {
		_, reentrant = log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)})
	}); err != nil {
		t.Fatalf("OnEvent() error = %v", err)
	}
	if _, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if !errors.Is(reentrant, ErrReentrantAppend) {
		t.Fatalf("reentrant Append() = %v, want ErrReentrantAppend", reentrant)
	}
	if log.Len() != 1 {
		t.Fatalf("Len() = %d, want the reentrant append refused", log.Len())
	}
}

// TestObserverFailureIsContained pins that a broken observer cannot fail a
// committed append or hide the observers after it.
func TestObserverFailureIsContained(t *testing.T) {
	log := NewLog(KnownTypes())
	heard := 0
	if _, err := log.OnEvent(func(Event) { panic("observer exploded") }); err != nil {
		t.Fatalf("OnEvent(panicking) error = %v", err)
	}
	if _, err := log.OnEvent(func(Event) { heard++ }); err != nil {
		t.Fatalf("OnEvent() error = %v", err)
	}
	event, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Append() = %v, want the append committed despite the observer", err)
	}
	if event.Seq != 0 || log.Len() != 1 {
		t.Fatal("the append did not commit")
	}
	if heard != 1 {
		t.Fatalf("the second observer heard %d events, want 1", heard)
	}
	failures := log.ObserverErrors()
	if len(failures) != 1 || !strings.Contains(failures[0].Error(), "panicked") {
		t.Fatalf("ObserverErrors() = %v, want the panic recorded", failures)
	}
}

// TestValidatorRejectsBeforeCommit pins the seam a derived component uses: a
// validation failure leaves the log untouched and publishes nothing.
func TestValidatorRejectsBeforeCommit(t *testing.T) {
	log := NewLog(KnownTypes())
	published := 0
	if _, err := log.OnEvent(func(Event) { published++ }); err != nil {
		t.Fatalf("OnEvent() error = %v", err)
	}
	want := errors.New("derived state rejects this")
	remove, err := log.OnValidate(func(Event) error { return want })
	if err != nil {
		t.Fatalf("OnValidate() error = %v", err)
	}
	if _, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)}); !errors.Is(err, want) {
		t.Fatalf("Append() = %v, want the validator's error", err)
	}
	if log.Len() != 0 || published != 0 {
		t.Fatalf("a rejected candidate was committed: Len=%d published=%d", log.Len(), published)
	}
	// Removing the validator restores appends.
	remove()
	if _, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)}); err != nil {
		t.Fatalf("Append() after removing the validator = %v", err)
	}
}

// TestObserversAndValidatorsCanBeRemoved pins the removal semantics, including
// that a removal is idempotent and does not disturb the others.
func TestObserversAndValidatorsCanBeRemoved(t *testing.T) {
	log := NewLog(KnownTypes())
	first, second := 0, 0
	removeFirst, err := log.OnEvent(func(Event) { first++ })
	if err != nil {
		t.Fatalf("OnEvent() error = %v", err)
	}
	if _, err := log.OnEvent(func(Event) { second++ }); err != nil {
		t.Fatalf("OnEvent() error = %v", err)
	}
	removeFirst()
	removeFirst() // idempotent
	if _, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if first != 0 || second != 1 {
		t.Fatalf("first=%d second=%d, want 0 and 1", first, second)
	}
	if _, err := log.OnEvent(nil); err == nil {
		t.Fatal("OnEvent(nil) = nil, want an error")
	}
	if _, err := log.OnValidate(nil); err == nil {
		t.Fatal("OnValidate(nil) = nil, want an error")
	}
}

// TestSetClockMustPrecedeTheFirstAppend pins that one log uses one clock, so
// timestamps within a log are comparable.
func TestSetClockMustPrecedeTheFirstAppend(t *testing.T) {
	log := NewLog(KnownTypes())
	if err := log.SetClock(nil); err == nil {
		t.Fatal("SetClock(nil) = nil, want an error")
	}
	if err := log.SetClock(fixedClock()); err != nil {
		t.Fatalf("SetClock() error = %v", err)
	}
	first, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	second, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if second.Time <= first.Time {
		t.Fatalf("timestamps %d then %d, want the clock observed", first.Time, second.Time)
	}
	if err := log.SetClock(fixedClock()); err == nil {
		t.Fatal("SetClock() after the first append = nil, want an error")
	}
	// An explicit time overrides the clock.
	explicit := time.UnixMilli(42)
	forced, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`), Time: &explicit})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if forced.Time != 42 {
		t.Fatalf("Time = %d, want the explicit time", forced.Time)
	}
}

// TestLogRoundTripRefusesGapsAndDuplicates pins that a decoded log must be
// contiguous from zero. A gap or a duplicate is never renumbered.
func TestLogRoundTripRefusesGapsAndDuplicates(t *testing.T) {
	log := NewLog(KnownTypes())
	for index := 0; index < 3; index++ {
		if _, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)}); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	encoded, err := MarshalLog(log.Events())
	if err != nil {
		t.Fatalf("MarshalLog() error = %v", err)
	}
	decoded, err := UnmarshalLog(encoded)
	if err != nil {
		t.Fatalf("UnmarshalLog() error = %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded %d events, want 3", len(decoded))
	}
	gap := `[{"type":"turn/start","seq":0,"time":1,"data":{}},{"type":"turn/start","seq":2,"time":1,"data":{}}]`
	if _, err := UnmarshalLog([]byte(gap)); !errors.Is(err, ErrSeqGap) {
		t.Fatalf("UnmarshalLog(gap) = %v, want ErrSeqGap", err)
	}
	duplicate := `[{"type":"turn/start","seq":0,"time":1,"data":{}},{"type":"turn/start","seq":0,"time":1,"data":{}}]`
	if _, err := UnmarshalLog([]byte(duplicate)); !errors.Is(err, ErrSeqGap) {
		t.Fatalf("UnmarshalLog(duplicate) = %v, want ErrSeqGap", err)
	}
	if events, err := UnmarshalLog(nil); err != nil || len(events) != 0 {
		t.Fatalf("UnmarshalLog(nil) = %v, %d events, want an empty log", err, len(events))
	}
	if _, err := UnmarshalLog([]byte(`{}`)); err == nil {
		t.Fatal("UnmarshalLog(object) = nil, want an error")
	}
	// A malformed event inside the array is blamed by index.
	bad := `[{"type":"turn/start","seq":0,"time":1,"data":{}},{"type":"turn/start","seq":1,"time":1}]`
	if _, err := UnmarshalLog([]byte(bad)); err == nil || !strings.Contains(err.Error(), "event 1") {
		t.Fatalf("UnmarshalLog(bad) = %v, want the failing index blamed", err)
	}
}

// TestConcurrentAppendsAndReads pins the concurrency contract under the race
// detector: sequences stay dense and distinct while readers observe the log.
func TestConcurrentAppendsAndReads(t *testing.T) {
	log := NewLog(KnownTypes())
	if err := log.SetClock(fixedClock()); err != nil {
		t.Fatalf("SetClock() error = %v", err)
	}
	const writers, perWriter = 8, 25
	seen := make([][]Seq, writers)
	var wait sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			for count := 0; count < perWriter; count++ {
				event, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)})
				if err != nil {
					t.Errorf("Append() error = %v", err)
					return
				}
				seen[index] = append(seen[index], event.Seq)
			}
		}(writer)
	}
	// Concurrent readers must never observe a partially built log.
	wait.Add(1)
	go func() {
		defer wait.Done()
		for count := 0; count < 200; count++ {
			if events := log.Events(); len(events) != 0 {
				for index, event := range events {
					if event.Seq != Seq(index) {
						t.Errorf("observed event %d with seq %d: the log was not dense", index, event.Seq)
						return
					}
				}
			}
			_ = log.Seq()
			_ = log.Len()
		}
	}()
	wait.Wait()

	if log.Len() != writers*perWriter {
		t.Fatalf("Len() = %d, want %d", log.Len(), writers*perWriter)
	}
	assigned := map[Seq]bool{}
	for _, sequences := range seen {
		for _, seq := range sequences {
			if assigned[seq] {
				t.Fatalf("seq %d was assigned twice", seq)
			}
			assigned[seq] = true
		}
	}
	if len(assigned) != writers*perWriter {
		t.Fatalf("%d distinct sequences, want %d", len(assigned), writers*perWriter)
	}
}

// TestRegistryPolicy pins the type registry: what it knows, how it classifies,
// and that register is a deliberate act.
func TestRegistryPolicy(t *testing.T) {
	registry := NewRegistry("a", "b")
	if !registry.Knows("a") || registry.Knows("c") {
		t.Fatal("Knows() did not report membership correctly")
	}
	if got := registry.Types(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Types() = %v, want both names sorted", got)
	}
	if err := registry.Classify("c", true); err != nil {
		t.Fatalf("Classify(unknown, ignorable) = %v, want nil", err)
	}
	if err := registry.Classify("c", false); !errors.Is(err, ErrUnknownRequiredEvent) {
		t.Fatalf("Classify(unknown) = %v, want ErrUnknownRequiredEvent", err)
	}
	if err := registry.Classify("a", false); err != nil {
		t.Fatalf("Classify(known) = %v, want nil", err)
	}
	if err := registry.Register("a"); err == nil {
		t.Fatal("Register(duplicate) = nil, want an error")
	}
	if err := registry.Register(""); err == nil {
		t.Fatal("Register(empty) = nil, want an error")
	}
	if err := registry.Register("c"); err != nil {
		t.Fatalf("Register(c) = %v, want nil", err)
	}
	var nilRegistry *Registry
	if nilRegistry.Knows("a") || nilRegistry.Types() != nil {
		t.Fatal("a nil registry must know nothing and list nothing")
	}
	if len((&Registry{}).Types()) != 0 {
		t.Fatal("an empty registry must list nothing")
	}
	var typed *Registry
	if err := typed.Register("x"); err == nil {
		t.Fatal("Register on a nil registry = nil, want an error")
	}
}

// TestKnownTypesAndSurfaceEligibility pins the vocabulary this build interprets
// and the closed surface set.
func TestKnownTypesAndSurfaceEligibility(t *testing.T) {
	registry := KnownTypes()
	for _, eventType := range []string{"user/message", "assistant/message", "tool/result", "turn/start", "request/header"} {
		if !registry.Knows(eventType) {
			t.Fatalf("KnownTypes() does not know %q", eventType)
		}
	}
	if registry.Knows("web/deepseek-search-llm-request") {
		t.Fatal("KnownTypes() claims a type this build cannot interpret")
	}
	types := SurfaceEventTypes()
	if len(types) != 5 {
		t.Fatalf("SurfaceEventTypes() = %v, want the five message-producing types", types)
	}
	if !IsSurfaceEligibleType("tool/result") || IsSurfaceEligibleType("turn/start") {
		t.Fatal("IsSurfaceEligibleType did not classify correctly")
	}
}

// TestNewLogWithoutARegistryRefusesEverything pins the deliberate default: a log
// that knows no event types treats every event as unknown and required, so a
// caller who forgets to pass a registry finds out immediately instead of silently
// accepting a vocabulary the build cannot interpret.
func TestNewLogWithoutARegistryRefusesEverything(t *testing.T) {
	log := NewLog(nil)
	if _, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`)}); err == nil {
		t.Fatal("Append() = nil, want an unknown required event refused")
	}
	if log.Len() != 0 {
		t.Fatalf("Len() = %d, want the log untouched", log.Len())
	}
	// An ignorable record is still representable, which is the documented escape.
	if _, err := log.Append(AppendInput{Type: "turn/start", Data: []byte(`{}`), Ignorable: true}); err != nil {
		t.Fatalf("Append(ignorable) = %v, want an ignorable record accepted", err)
	}
}

// TestSortedSeqStrings pins the diagnostic helper.
func TestSortedSeqStrings(t *testing.T) {
	got := SortedSeqStrings([]Seq{5, 1, 3})
	if strings.Join(got, ",") != "1,3,5" {
		t.Fatalf("SortedSeqStrings() = %v, want sorted", got)
	}
}

// TestEqualJSONIgnoresKeyOrder pins the comparison a "only content may change"
// rule depends on: a reordered but equal object must compare equal.
func TestEqualJSONIgnoresKeyOrder(t *testing.T) {
	if !equalJSON([]byte(`{"a":1,"b":2}`), []byte(`{"b":2,"a":1}`)) {
		t.Fatal("equalJSON() = false for a reordered equal object")
	}
	if equalJSON([]byte(`{"a":1}`), []byte(`{"a":2}`)) {
		t.Fatal("equalJSON() = true for different values")
	}
	if equalJSON([]byte(`{`), []byte(`{}`)) {
		t.Fatal("equalJSON() = true for invalid JSON")
	}
}

// TestJSONLossless pins the payload admission rule.
func TestJSONLossless(t *testing.T) {
	if !jsonLossless(nil) || !jsonLossless([]byte(`{}`)) || !jsonLossless([]byte(`[1,2]`)) {
		t.Fatal("jsonLossless() rejected a representable payload")
	}
	if jsonLossless([]byte(`{`)) || jsonLossless([]byte(`{} {}`)) {
		t.Fatal("jsonLossless() accepted a malformed payload")
	}
}
