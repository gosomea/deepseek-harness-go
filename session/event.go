package session

import (
	"encoding/json"
	"fmt"
)

// This file owns the envelope-level rules: what makes an event a well-formed
// entry, and how its surface metadata and cited sources are constrained. The JSON
// encoding of those rules lives in codec.go.

// ValidateEventEnvelope checks the envelope-level rules that do not depend on a
// log position or a registry.
func ValidateEventEnvelope(event Event) error {
	if event.Type == "" {
		return fmt.Errorf("%w: event requires a type", ErrInvalidEvent)
	}
	if len(event.Data) == 0 {
		return fmt.Errorf("%w: event %q requires data", ErrInvalidEvent, event.Type)
	}
	var probe any
	if err := json.Unmarshal(event.Data, &probe); err != nil {
		return fmt.Errorf("%w: event %q data is not valid JSON: %w", ErrInvalidEvent, event.Type, err)
	}
	if event.HasIgnorable && !bool(event.Ignorable) {
		// A written false is representable but means the same as absent; it is kept
		// only so a round trip is faithful.
		_ = event
	}
	if err := validateSurfaceMetadata(event); err != nil {
		return err
	}
	return nil
}

// validateSurfaceMetadata enforces surface eligibility and citation rules.
func validateSurfaceMetadata(event Event) error {
	if !IsSurfaceEligibleType(event.Type) {
		// An unknown ignorable record keeps opaque metadata without affecting
		// history. A known log-only event must not carry surface metadata at all.
		if !KnownTypes().Knows(event.Type) && bool(event.Ignorable) {
			return nil
		}
		if event.SurfaceOp != nil {
			return fmt.Errorf("%w: %q is not surface-eligible and cannot carry surfaceOp", ErrInvalidEvent, event.Type)
		}
		if event.SourceEventSeqs != nil {
			return fmt.Errorf("%w: %q is not surface-eligible and cannot carry sourceEventSeqs", ErrInvalidEvent, event.Type)
		}
		return nil
	}
	if event.SurfaceOp == nil {
		return fmt.Errorf("%w: %q is surface-eligible and requires a surfaceOp", ErrInvalidEvent, event.Type)
	}
	if err := event.SurfaceOp.Validate(); err != nil {
		return err
	}
	if event.SurfaceOp.IsReplace() {
		if event.SurfaceOp.StartSeq >= event.Seq || event.SurfaceOp.EndSeq >= event.Seq {
			return fmt.Errorf("%w: surface replace at seq %d must reference earlier events", ErrInvalidEvent, event.Seq)
		}
	}
	return validateSources(event, nil)
}

// validateSources checks the cited source-event sequences against the event's own
// position and, when a range is known, against the nodes it must cover.
func validateSources(event Event, shadowed []Seq) error {
	if event.Type == "assistant/message" && event.SourceEventSeqs != nil {
		return fmt.Errorf("%w: assistant/message embeds its stream and cannot carry sourceEventSeqs", ErrInvalidEvent)
	}
	sources := map[Seq]bool{}
	if event.SourceEventSeqs != nil {
		if len(event.SourceEventSeqs) == 0 {
			return fmt.Errorf("%w: sourceEventSeqs must not be empty", ErrInvalidEvent)
		}
		for _, source := range event.SourceEventSeqs {
			if sources[source] {
				return fmt.Errorf("%w: sourceEventSeqs must not contain duplicates", ErrInvalidEvent)
			}
			sources[source] = true
			if source >= event.Seq {
				return fmt.Errorf("%w: sourceEventSeqs must reference earlier events: %d >= %d",
					ErrInvalidEvent, source, event.Seq)
			}
		}
	}
	var missing []Seq
	for _, seq := range shadowed {
		if !sources[seq] {
			missing = append(missing, seq)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: sourceEventSeqs must include every shadowed surface node; missing %v", ErrInvalidEvent, missing)
	}
	return nil
}
