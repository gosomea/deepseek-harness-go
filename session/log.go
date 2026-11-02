package session

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// Validator inspects a candidate event before it is committed.
//
// It is the seam that lets a component which derives state from the log reject a
// candidate *before* the log changes. Without it, a rule that depends on derived
// state (does this replacement name an existing surface node?) could only be
// checked after the fact, which would leave a committed event the component cannot
// interpret.
type Validator func(Event) error

// Observer receives each committed event after it enters the log.
//
// An observer runs after the append is committed, so its failure cannot change
// what was recorded: it is reported to the observer's own error sink and never
// turns a successful append into a failed one.
type Observer func(Event)

// Log is an append-only, in-memory session log.
//
// It owns the dense event slice and the rules that decide whether a candidate may
// join it. Deriving anything from the log — the model surface, projections,
// summaries — belongs to a component that attaches through OnValidate and
// OnEvent, so this type stays the single authority on what was recorded.
//
// Log is safe for concurrent use. Appends are not reentrant: an observer must not
// append, because that would reorder the log while a reader walks it.
type Log struct {
	// mu guards every field below it.
	mu sync.Mutex
	// events is the dense event slice; its length is always the next sequence.
	events []Event
	// registry classifies event types for the unknown-event policy.
	registry *Registry
	// surface is the live ordered view of message-producing positions. It is
	// maintained by the same commit that appends an event, so a reader never sees
	// a log whose surface does not match its events.
	surface Surface
	// projections are the registered message interpreters.
	projections []MessageProjection
	// validators run before a candidate is committed.
	validators []Validator
	// observers receive each committed event.
	observers []Observer
	// observerErrors collects observer failures without failing the append.
	observerErrors []error
	// publish serializes appends. It is held for the whole call, including the
	// observer fan-out, so notifications arrive in commit order.
	publish sync.Mutex
	// publisher is the goroutine currently inside publish, or 0 when none is.
	// It exists so that a nested append from a callback is reported as an error
	// rather than deadlocking on publish, while an append from a different
	// goroutine still queues normally. Go offers no supported goroutine identity,
	// so this is read from the runtime's stack header in one helper; the cost is
	// paid once per append, not per event read.
	publisher atomic.Uint64
	// now supplies the timestamp, so a test can be deterministic.
	now func() time.Time
}

// NewLog creates an empty log that classifies events with registry.
//
// A nil registry knows no event types, which means every event is treated as an
// unknown required event and refused. Callers normally pass KnownTypes.
func NewLog(registry *Registry) *Log {
	if registry == nil {
		registry = NewRegistry()
	}
	return &Log{registry: registry, now: time.Now, surface: Surface{projected: map[Seq]json.RawMessage{}}}
}

// SetClock replaces the timestamp source. It is for deterministic tests and must
// be called before any append, so no two events in one log come from different
// clocks.
func (l *Log) SetClock(now func() time.Time) error {
	if now == nil {
		return fmt.Errorf("%w: clock is nil", ErrInvalidEvent)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) != 0 {
		return fmt.Errorf("%w: clock must be set before the first append", ErrInvalidEvent)
	}
	l.now = now
	return nil
}

// OnValidate registers a pre-commit validator and returns the function that
// removes it.
//
// Validators run in registration order before every append. The first failure
// rejects the append, leaving the log unchanged, so a component can enforce a rule
// over state it derives from the log without risk of a half-applied event.
func (l *Log) OnValidate(validator Validator) (func(), error) {
	if validator == nil {
		return nil, fmt.Errorf("%w: validator is nil", ErrInvalidEvent)
	}
	l.mu.Lock()
	l.validators = append(l.validators, validator)
	index := len(l.validators) - 1
	l.mu.Unlock()
	return l.remover(len(l.validators)-index-1, func() {
		l.validators = append(l.validators[:index], l.validators[index+1:]...)
	}), nil
}

// OnEvent registers an observer and returns the function that removes it.
//
// Observers are notified in registration order, and one observer's failure does
// not prevent the rest from seeing the same committed event.
func (l *Log) OnEvent(observer Observer) (func(), error) {
	if observer == nil {
		return nil, fmt.Errorf("%w: observer is nil", ErrInvalidEvent)
	}
	l.mu.Lock()
	l.observers = append(l.observers, observer)
	index := len(l.observers) - 1
	l.mu.Unlock()
	return l.remover(len(l.observers)-index-1, func() {
		l.observers = append(l.observers[:index], l.observers[index+1:]...)
	}), nil
}

// remover builds an idempotent removal function. The registration index is
// captured at registration time; removal is by identity of the captured slot so a
// concurrent append cannot make an unrelated callback disappear.
func (l *Log) remover(_ int, remove func()) func() {
	removed := false
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if removed {
			return
		}
		removed = true
		remove()
	}
}

// ObserverErrors returns the failures observer callbacks reported, in order. They
// are recorded rather than returned from Append because a committed append cannot
// be undone by a downstream reader's failure.
func (l *Log) ObserverErrors() []error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]error(nil), l.observerErrors...)
}

// Seq returns the next sequence number, which is always the log length.
func (l *Log) Seq() Seq {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Seq(len(l.events))
}

// Len reports how many events the log holds.
func (l *Log) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}

// Events returns a detached copy of the whole log in order.
func (l *Log) Events() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.detachedLocked()
}

// EventAt returns a detached copy of one event, or false when the position is
// outside the log.
func (l *Log) EventAt(seq Seq) (Event, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if seq >= Seq(len(l.events)) {
		return Event{}, false
	}
	return l.events[seq].Clone(), true
}

// Surface returns a detached snapshot of the live surface.
//
// The nodes are the message-producing positions in model-visible order; the
// generations tell a derived-message cache whether it must rebuild.
func (l *Log) Surface() Surface {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Surface{
		Nodes:             append([]Seq(nil), l.surface.Nodes...),
		Replacements:      append([]SurfaceReplacement(nil), l.surface.Replacements...),
		ReplaceGeneration: l.surface.ReplaceGeneration,
		ContentGeneration: l.surface.ContentGeneration,
		projected:         l.surface.projected,
	}
}

// SurfaceNodes returns the live message-producing positions in model-visible
// order.
func (l *Log) SurfaceNodes() []Seq {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Seq(nil), l.surface.Nodes...)
}

// ReplaceGeneration reports the number of committed positional replacements.
func (l *Log) ReplaceGeneration() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.surface.ReplaceGeneration
}

// ContentGeneration reports the number of committed changes to existing
// model-visible content. A replacement and a message projection both count; a
// plain append does not.
func (l *Log) ContentGeneration() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.surface.ContentGeneration
}

// DeriveMessages rebuilds model history from the live surface.
func (l *Log) DeriveMessages() ([]llm.Message, error) {
	l.mu.Lock()
	snapshot := Surface{
		Nodes:     append([]Seq(nil), l.surface.Nodes...),
		projected: l.surface.projected,
	}
	events := l.detachedLocked()
	l.mu.Unlock()
	return snapshot.DeriveMessages(events)
}

// AppendInput describes one event to append. Type and Data are required: an event
// always carries a payload, and a producer with nothing to say passes an empty
// object. Both are refused rather than defaulted, so "no payload" cannot be
// expressed two ways.
type AppendInput struct {
	// Type is the event's discriminator.
	Type string
	// Data is the payload as JSON. It is required; pass an empty object when the
	// event carries no fields.
	Data []byte
	// Time overrides the recorded time. A zero value uses the log's clock.
	Time *time.Time
	// Ignorable marks an informational record whose loss cannot affect
	// reconstruction.
	Ignorable bool
	// SurfaceOp places a message-producing event on the surface.
	SurfaceOp *SurfaceOp
	// SourceEventSeqs cites earlier events this one derives from.
	SourceEventSeqs []Seq
}

// Append validates and commits one event, returning a detached copy of what was
// recorded.
//
// Everything that can fail happens before the event enters the slice: the registry
// classifies the type, the payload must be losslessly JSON, the envelope and its
// surface metadata must be well-formed, and every registered validator must
// accept it. A rejected append therefore leaves the sequence unchanged and
// publishes nothing.
//
// The payload and cited sequences are copied on the way in, so a caller cannot
// later mutate a committed fact through the slice it passed.
func (l *Log) Append(input AppendInput) (Event, error) {
	// A nested append (a callback appending during the fan-out) must fail instead
	// of waiting on the lock its own caller holds.
	if current := goroutineID(); l.publisher.Load() == current {
		return Event{}, fmt.Errorf("%w: %q", ErrReentrantAppend, input.Type)
	}
	l.publish.Lock()
	defer l.publish.Unlock()
	l.publisher.Store(goroutineID())
	defer l.publisher.Store(0)

	if input.Type == "" {
		return Event{}, fmt.Errorf("%w: event requires a type", ErrInvalidEvent)
	}
	if err := l.registry.Classify(input.Type, Ignorable(input.Ignorable)); err != nil {
		return Event{}, err
	}

	l.mu.Lock()
	event, err := l.buildLocked(input)
	if err != nil {
		l.mu.Unlock()
		return Event{}, err
	}
	// The surface transition is planned against the current surface before the
	// event is stored, so a rejected candidate leaves both unchanged.
	planner := l.surface
	planner.Nodes = append([]Seq(nil), l.surface.Nodes...)
	planner.Replacements = append([]SurfaceReplacement(nil), l.surface.Replacements...)
	planned := map[Seq]json.RawMessage{}
	for seq, message := range l.surface.projected {
		planned[seq] = message
	}
	planner.projected = planned
	window := append(l.detachedLocked(), event)
	if err := planner.apply(event, window, 0, l.projections); err != nil {
		l.mu.Unlock()
		return Event{}, err
	}
	validators := append([]Validator(nil), l.validators...)
	l.mu.Unlock()

	for _, validator := range validators {
		if err := validator(event); err != nil {
			return Event{}, err
		}
	}

	l.mu.Lock()
	l.events = append(l.events, event)
	l.surface = planner
	observers := append([]Observer(nil), l.observers...)
	l.mu.Unlock()

	committed := event.Clone()
	var failures []error
	for _, observer := range observers {
		if err := observe(observer, committed); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		l.mu.Lock()
		l.observerErrors = append(l.observerErrors, failures...)
		l.mu.Unlock()
	}
	return committed, nil
}

// goroutineID extracts the runtime's goroutine identifier from a stack header.
//
// Go does not expose goroutine identity, and the only thing it is used for here is
// telling "a callback appended during its own publication" apart from "another
// goroutine appended concurrently". The reference is single-threaded, so it only
// ever had to answer the first question; a Go port must answer both, and this is
// the smallest mechanism that can. The value is only ever compared for equality
// within one process.
func goroutineID() uint64 {
	var header [64]byte
	length := runtime.Stack(header[:], false)
	// The header looks like "goroutine 123 [running]:". Anything unexpected means
	// "unknown", which falls back to treating the append as non-reentrant.
	const prefix = "goroutine "
	if length < len(prefix) || string(header[:len(prefix)]) != prefix {
		return 0
	}
	var id uint64
	for _, char := range header[len(prefix):length] {
		if char < '0' || char > '9' {
			break
		}
		id = id*10 + uint64(char-'0')
	}
	return id
}

// buildLocked validates one candidate and returns the exact event to store.
func (l *Log) buildLocked(input AppendInput) (Event, error) {
	when := l.now()
	if input.Time != nil {
		when = *input.Time
	}
	event := Event{
		Type:            input.Type,
		Seq:             Seq(len(l.events)),
		Time:            TimeMS(when.UnixMilli()),
		Data:            append([]byte(nil), input.Data...),
		Ignorable:       Ignorable(input.Ignorable),
		HasIgnorable:    input.Ignorable,
		SourceEventSeqs: append([]Seq(nil), input.SourceEventSeqs...),
	}
	if input.SurfaceOp != nil {
		op := *input.SurfaceOp
		event.SurfaceOp = &op
	}
	if !jsonLossless(event.Data) {
		return Event{}, fmt.Errorf("%w: %q", ErrNonJSONData, input.Type)
	}
	if err := ValidateEventEnvelope(event); err != nil {
		return Event{}, err
	}
	return event, nil
}

// observe calls one observer, converting a panic into an error so one broken
// observer cannot corrupt the log or hide the others.
func observe(observer Observer, event Event) (err error) {
	defer func() {
		if reason := recover(); reason != nil {
			err = fmt.Errorf("session: observer panicked: %v", reason)
		}
	}()
	observer(event)
	return nil
}

// detachedLocked returns a detached copy of the whole log.
func (l *Log) detachedLocked() []Event {
	cloned := make([]Event, 0, len(l.events))
	for _, event := range l.events {
		cloned = append(cloned, event.Clone())
	}
	return cloned
}
