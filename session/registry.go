package session

import (
	"fmt"
	"sort"
)

// surfaceEventTypes is the closed set of event types that produce model messages
// and may therefore carry surface metadata. The reference declares the same five
// as a closed union.
var surfaceEventTypes = map[string]bool{
	"system/message":    true,
	"developer/message": true,
	"user/message":      true,
	"assistant/message": true,
	"tool/result":       true,
}

// IsSurfaceEligibleType reports whether an event type can join the model surface.
func IsSurfaceEligibleType(eventType string) bool { return surfaceEventTypes[eventType] }

// SurfaceEventTypes returns the message-producing types in a stable order. It
// exists so a caller can enumerate the vocabulary instead of copying the list.
func SurfaceEventTypes() []string {
	types := make([]string, 0, len(surfaceEventTypes))
	for eventType := range surfaceEventTypes {
		types = append(types, eventType)
	}
	sort.Strings(types)
	return types
}

// Registry classifies event types for one log.
//
// Classification is what makes the unknown-event policy decidable: a type the
// registry knows is understood and must therefore be processed; a type it does not
// know is either an ignorable informational record, which is preserved and skipped,
// or a required event, which makes the whole log unreadable rather than silently
// shorter.
//
// A zero Registry knows no types, which is only useful for tests; production code
// starts from KnownTypes.
type Registry struct {
	// known is the set of types this build can interpret.
	known map[string]bool
}

// NewRegistry builds a registry from an explicit list of known types.
func NewRegistry(types ...string) *Registry {
	known := make(map[string]bool, len(types))
	for _, eventType := range types {
		if eventType != "" {
			known[eventType] = true
		}
	}
	return &Registry{known: known}
}

// Knows reports whether this build can interpret the type.
func (r *Registry) Knows(eventType string) bool {
	if r == nil {
		return false
	}
	return r.known[eventType]
}

// Types returns the known types in sorted order.
func (r *Registry) Types() []string {
	if r == nil {
		return nil
	}
	types := make([]string, 0, len(r.known))
	for eventType := range r.known {
		types = append(types, eventType)
	}
	sort.Strings(types)
	return types
}

// Register adds one type this build can interpret. It reports an error for an
// empty or already-known type, because a duplicate registration is a build
// mistake rather than a runtime condition.
func (r *Registry) Register(eventType string) error {
	if eventType == "" {
		return fmt.Errorf("%w: registry needs a type name", ErrInvalidEvent)
	}
	if r == nil {
		return fmt.Errorf("%w: registry is nil", ErrInvalidEvent)
	}
	if r.known == nil {
		r.known = map[string]bool{}
	}
	if r.known[eventType] {
		return fmt.Errorf("%w: event type %q is already registered", ErrInvalidEvent, eventType)
	}
	r.known[eventType] = true
	return nil
}

// Classify reports how a reader must treat one event type.
func (r *Registry) Classify(eventType string, ignorable Ignorable) error {
	if r.Knows(eventType) {
		return nil
	}
	if ignorable {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrUnknownRequiredEvent, eventType)
}

// knownCoreTypes is the event vocabulary this build interprets.
//
// It mirrors the reference's generated catalog for the events the stages up to M3
// produce. A later stage that adds a producer registers its type here; the list is
// the single place that decides whether an unfamiliar log is readable.
var knownCoreTypes = []string{
	"assistant/attempt",
	"assistant/message",
	"developer/message",
	"image/offload",
	"request/context",
	"request/header",
	"session/end-seed",
	"step/end",
	"step/start",
	"system/message",
	"tool/call",
	"tool/result",
	"turn/end",
	"turn/start",
	"user/message",
}

// KnownTypes returns the registry of every core event type this build interprets.
//
// The list includes `image/offload`, which this package classifies but does not
// itself interpret: its model-visible effect is owned by the plugin that produces
// it, so a log containing one is refused unless that plugin registered its
// interpreter. Classifying a type and interpreting it are deliberately separate.
//
// A caller that adds its own producers composes a registry from this one rather
// than extending the package's list, so two plugins cannot fight over one
// vocabulary.
func KnownTypes() *Registry {
	return NewRegistry(knownCoreTypes...)
}
