package llm

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// ErrInvalidValue reports a value that cannot enter the conversation
// vocabulary: a missing required field, an unknown discriminator where a known
// one is required, or a payload that is not losslessly JSON.
var ErrInvalidValue = errors.New("llm: invalid value")

// MessageID identifies one message across every representation boundary. It is
// an opaque string: the reference brands identity at compile time and performs
// no format validation, so this package preserves whatever id a producer
// supplies instead of rejecting historical or foreign ids.
type MessageID string

// String returns the raw identifier.
func (id MessageID) String() string { return string(id) }

// IsZero reports whether the identifier is unset.
func (id MessageID) IsZero() bool { return id == "" }

// ToolCallID correlates a tool call with the result that answers it. The pairing
// is by identifier, never by position: a result that arrives out of order still
// names the call it belongs to.
type ToolCallID string

// String returns the raw identifier.
func (id ToolCallID) String() string { return string(id) }

// IsZero reports whether the identifier is unset.
func (id ToolCallID) IsZero() bool { return id == "" }

// NewMessageID mints a random version-4 UUID message identifier.
//
// The format matches the reference's identifier shape so that logs written by
// either implementation are legible to a reader. Nothing depends on the format:
// MessageID accepts any non-empty string, so a caller may supply an id from an
// existing log.
func NewMessageID() (MessageID, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40 // version 4
	raw[8] = (raw[8] & 0x3f) | 0x80 // RFC 4122 variant
	encoded := hex.EncodeToString(raw)
	return MessageID(encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]), nil
}

// MustMessageID is NewMessageID for callers that cannot continue without an
// identifier, such as a package-level fixture. It panics only when the
// operating system's random source fails.
func MustMessageID() MessageID {
	id, err := NewMessageID()
	if err != nil {
		panic(err)
	}
	return id
}
