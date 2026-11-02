package session

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrInvalidEvent reports an event that cannot enter the log: a missing or
// unknown type, a malformed envelope, surface metadata on the wrong kind of
// event, or a payload this build cannot classify.
var ErrInvalidEvent = errors.New("session: invalid event")

// ErrSeqGap reports a sequence number that is not the next position. A gap or a
// duplicate is never repaired or renumbered, because the sequence number names an
// event position the whole system addresses.
var ErrSeqGap = errors.New("session: event seq is not contiguous")

// ErrUnknownRequiredEvent reports an event whose type this build does not know
// and which is not marked ignorable.
//
// The default is deliberately "required": a reader that silently skipped an
// unrecognized event could reconstruct a wrong session, whereas refusing is an
// inconvenience the writer can fix with one marker. Only an informational record
// whose loss cannot affect reconstruction should be marked ignorable.
var ErrUnknownRequiredEvent = errors.New("session: unknown event type is not ignorable")

// ErrNonJSONData reports an event payload that cannot be written losslessly. The
// log is JSON, so a value that does not survive a round trip must fail at the
// append site rather than at a later flush.
var ErrNonJSONData = errors.New("session: event data is not losslessly JSON")

// ErrReentrantAppend reports an append attempted from inside another append's
// publication. Allowing it would reorder the log while a reader observes it.
var ErrReentrantAppend = errors.New("session: append cannot reenter while another append is being published")

// Seq is one event's position in a session log.
//
// It is a position, not an allocated counter: the next sequence of a log is
// always its current length. Using a distinct type from an offset keeps the two
// conventions visible, and neither admits a negative value.
type Seq uint64

// TimeMS is a Unix epoch timestamp in milliseconds.
//
// It is signed and never range-checked beyond "a whole number of milliseconds":
// the reference records wall-clock time and deliberately does not require
// monotonicity, because two events in the same millisecond are ordinary.
type TimeMS int64

// Ignorable reports whether a reader may skip an event it does not recognize.
//
// It is only ever meaningful when true. The zero value means "required", which is
// the safe default.
type Ignorable bool

// IsSet reports whether the marker means "a reader may skip this event". It exists
// so the boolean conversion is written once instead of at every call site.
func (i Ignorable) IsSet() bool { return bool(i) }

// SurfaceOp is how one message-producing event enters the ordered surface.
//
// It is a struct with a discriminator rather than one type per variant so that a
// decoder validates the exact key set: "append" carries no range, and a
// replacement must carry both endpoints and nothing else.
type SurfaceOp struct {
	// Op is either SurfaceAppend or SurfaceReplace.
	Op string
	// StartSeq is the first shadowed surface node of a replacement.
	StartSeq Seq
	// EndSeq is the last shadowed surface node of a replacement.
	EndSeq Seq
}

// The surface operation discriminator values.
const (
	// SurfaceAppend adds the event to the surface tail.
	SurfaceAppend = "append"
	// SurfaceReplace shadows an existing surface range with this event.
	SurfaceReplace = "replace"
)

// IsAppend reports whether the operation appends to the surface tail.
func (o SurfaceOp) IsAppend() bool { return o.Op == SurfaceAppend }

// IsReplace reports whether the operation replaces a surface range.
func (o SurfaceOp) IsReplace() bool { return o.Op == SurfaceReplace }

// Validate reports whether the operation is one of the two exact shapes.
func (o SurfaceOp) Validate() error {
	switch o.Op {
	case SurfaceAppend:
		if o.StartSeq != 0 || o.EndSeq != 0 {
			return fmt.Errorf("%w: append surfaceOp carries no range", ErrInvalidEvent)
		}
		return nil
	case SurfaceReplace:
		return nil
	case "":
		return fmt.Errorf("%w: surfaceOp requires an op", ErrInvalidEvent)
	default:
		return fmt.Errorf("%w: unknown surfaceOp %q", ErrInvalidEvent, o.Op)
	}
}

// Event is one immutable entry in the session log.
//
// The envelope is a fixed tagged record: a type, a dense sequence number, a
// time, a payload, an optional ignorable marker and optional surface metadata.
// There is no id, no turn or step at this level, and no parent link; those live
// inside a payload or on the session, and keeping them out means an unknown event
// can be preserved without this package having to understand it.
//
// Payload stays raw JSON so that a type this build does not know survives a decode
// and re-encode unchanged. A caller reads a known payload through DecodeData.
type Event struct {
	// Type is the event's discriminator.
	Type string
	// Seq is the event's position in the log.
	Seq Seq
	// Time is the recorded wall-clock milliseconds.
	Time TimeMS
	// Data is the event's payload as JSON.
	Data json.RawMessage
	// Ignorable marks an informational record a reader may skip when it does not
	// recognize the type. It is only ever true when present.
	Ignorable Ignorable
	// SurfaceOp is the surface placement of a message-producing event. It is nil
	// for a log-only event.
	SurfaceOp *SurfaceOp
	// SourceEventSeqs cites earlier events this one derives from.
	SourceEventSeqs []Seq
	// HasIgnorable distinguishes an absent marker from a written false, which is
	// how a lossless round trip keeps what a writer recorded.
	HasIgnorable bool
}

// HasSurface reports whether the event carries surface metadata.
func (e Event) HasSurface() bool { return e.SurfaceOp != nil }

// IsSurfaceEligible reports whether this event's type can join the model surface.
func (e Event) IsSurfaceEligible() bool { return IsSurfaceEligibleType(e.Type) }

// Clone returns a detached copy. The log stores and returns copies so a caller
// cannot mutate a committed fact through a slice it still holds.
func (e Event) Clone() Event {
	cloned := e
	if e.Data != nil {
		cloned.Data = make(json.RawMessage, len(e.Data))
		copy(cloned.Data, e.Data)
	}
	if e.SurfaceOp != nil {
		op := *e.SurfaceOp
		cloned.SurfaceOp = &op
	}
	if e.SourceEventSeqs != nil {
		cloned.SourceEventSeqs = make([]Seq, len(e.SourceEventSeqs))
		copy(cloned.SourceEventSeqs, e.SourceEventSeqs)
	}
	return cloned
}
