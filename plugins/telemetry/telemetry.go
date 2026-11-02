package telemetry

import (
	"errors"
	"fmt"
	"sync"
)

// Errors this package returns.
var (
	// ErrForbiddenField reports a field not on the telemetry allow-list.
	ErrForbiddenField = errors.New("telemetry: field is not allowed")
)

// Event is one telemetry record: a name and a set of allowed scalar fields.
type Event struct {
	// Name is the event name. Required.
	Name string
	// Fields are the scalar fields; only allow-listed keys are accepted.
	Fields map[string]string
}

// allowedFields is the fixed set of field names telemetry may carry.
//
// Content, prompts, credentials and free text are deliberately absent: they must
// not leave the host. Keeping the list here, and refusing anything else, is what
// makes the privacy boundary enforceable.
var allowedFields = map[string]bool{
	"status": true, "count": true, "durationMs": true, "model": true, "provider": true, "errorCode": true,
}

// Sink receives telemetry events.
type Sink interface {
	// Record delivers one event.
	Record(Event) error
}

// Recorder validates events against the allow-list and forwards them to a sink.
type Recorder struct {
	mu   sync.Mutex
	sink Sink
	sent []Event
}

// New returns a recorder over a sink.
func New(sink Sink) (*Recorder, error) {
	if sink == nil {
		return nil, fmt.Errorf("telemetry: a sink is required")
	}
	return &Recorder{sink: sink}, nil
}

// Emit validates and records one event.
//
// It refuses an event with an off-list field before forwarding, so a privacy
// violation is a hard error rather than a leak that reaches the sink.
func (r *Recorder) Emit(event Event) error {
	if event.Name == "" {
		return fmt.Errorf("telemetry: event name is required")
	}
	for key := range event.Fields {
		if !allowedFields[key] {
			return fmt.Errorf("%w: %q", ErrForbiddenField, key)
		}
	}
	if err := r.sink.Record(event); err != nil {
		return err
	}
	r.mu.Lock()
	r.sent = append(r.sent, event)
	r.mu.Unlock()
	return nil
}

// Sent returns the events emitted, for tests to observe.
func (r *Recorder) Sent() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.sent))
	copy(out, r.sent)
	return out
}
