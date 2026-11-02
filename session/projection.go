package session

import (
	"encoding/json"
	"fmt"
)

// MessageProjection is a pure interpreter for one plugin-owned event type that
// changes the content of an *existing* model-visible message.
//
// A projection is not a fold and not a producer: it cannot add or remove surface
// nodes, it returns replacement messages for positions that are already on the
// surface, and the message identity it returns must be the identity already there.
// The reference states the same contract.
type MessageProjection interface {
	// Type is the event type this interpreter owns.
	Type() string
	// Project validates one event against the history before it and returns the
	// messages it changes, keyed by their original positions.
	//
	// It must validate the whole decision before returning anything: a partial map
	// would apply a change the interpreter itself rejected. It must not mutate the
	// context it is given.
	Project(event Event, context ProjectionContext) (map[Seq]json.RawMessage, error)
}

// ProjectionContext is the history an interpreter sees.
type ProjectionContext struct {
	// Nodes lists the current surface positions in model-visible order.
	Nodes []Seq
	// Events is the contiguous event window up to and including the candidate.
	Events []Event
	// BaseSeq is the absolute position of the window's first event.
	BaseSeq Seq
	// Log reads the log this projection runs against, for an interpreter that
	// needs an event outside the supplied window.
	Log *Log
}

// projectedMessages is the per-log cache of projection output, keyed by the
// position whose content was replaced.
type projectedMessages map[Seq]json.RawMessage

// requiresProjectionTypes is the set of event types that must have an interpreter
// registered before they may enter a log.
//
// It mirrors the reference's generated `MESSAGE_PROJECTION_EVENT_TYPES`. A type
// belongs here when its model-visible effect is not expressible by the surface op
// alone, so silently ignoring it would reconstruct history without the change.
var requiresProjectionTypes = map[string]bool{
	"image/offload": true,
}

// RequiresProjection reports whether an event type must have an interpreter
// registered.
func RequiresProjection(eventType string) bool { return requiresProjectionTypes[eventType] }

// RegisterProjection installs one message interpreter and returns the function
// that removes it.
//
// A second registration for the same event type is refused: two interpreters for
// one type would make history depend on registration order, which is exactly the
// nondeterminism the log exists to prevent.
func (l *Log) RegisterProjection(projection MessageProjection) (func(), error) {
	if projection == nil {
		return nil, fmt.Errorf("%w: projection is nil", ErrInvalidEvent)
	}
	eventType := projection.Type()
	if eventType == "" {
		return nil, fmt.Errorf("%w: projection needs an event type", ErrInvalidEvent)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, existing := range l.projections {
		if existing.Type() == eventType {
			return nil, fmt.Errorf("%w: message projection %q is already registered", ErrInvalidEvent, eventType)
		}
	}
	l.projections = append(l.projections, projection)
	index := len(l.projections) - 1
	removed := false
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if removed {
			return
		}
		removed = true
		if index < len(l.projections) && l.projections[index] == projection {
			l.projections = append(l.projections[:index], l.projections[index+1:]...)
		}
	}, nil
}
