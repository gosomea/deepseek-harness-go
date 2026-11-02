package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Version is the wire protocol version this package speaks.
//
// It is a single frozen integer rather than a range: a client and a host either
// agree on the version or one of them refuses, which is what makes "which
// messages can I send" answerable before a request is made.
const Version = 1

// Errors this package returns.
var (
	// ErrVersionMismatch reports a peer that speaks another protocol version.
	ErrVersionMismatch = errors.New("protocol: version mismatch")
	// ErrUnknownMethod reports a method this host does not implement.
	ErrUnknownMethod = errors.New("protocol: unknown method")
	// ErrInvalidEnvelope reports a malformed message envelope.
	ErrInvalidEnvelope = errors.New("protocol: invalid envelope")
	// ErrUnauthorized reports a request without a valid credential.
	ErrUnauthorized = errors.New("protocol: unauthorized")
)

// Envelope is the outer frame of every wire message.
//
// It carries the protocol version, a request id for correlating a response and a
// method name. The request id is mandatory because responses are matched to
// requests by it, not by arrival order: a slow request must not receive another
// request's answer.
type Envelope struct {
	// Version is the sender's protocol version. Required.
	Version int `json:"version"`
	// ID correlates a response with its request. Required.
	ID string `json:"id"`
	// Method is the request method, or "response" for an answer. Required.
	Method string `json:"method"`
	// Auth is a bearer token for an authenticated call. Empty for an
	// unauthenticated or pre-auth exchange.
	Auth string `json:"auth,omitempty"`
	// Payload is the method-specific body.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Validate reports whether an envelope is well formed for this version.
func (e Envelope) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("%w: got %d, want %d", ErrVersionMismatch, e.Version, Version)
	}
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("%w: an id is required", ErrInvalidEnvelope)
	}
	if strings.TrimSpace(e.Method) == "" {
		return fmt.Errorf("%w: a method is required", ErrInvalidEnvelope)
	}
	return nil
}

// ErrorCode is a stable machine-readable error identifier.
type ErrorCode string

// The error codes this protocol defines.
const (
	// CodeInvalid is a malformed request.
	CodeInvalid ErrorCode = "invalid"
	// CodeNotFound is a missing resource.
	CodeNotFound ErrorCode = "not-found"
	// CodeUnauthorized is a missing or bad credential.
	CodeUnauthorized ErrorCode = "unauthorized"
	// CodeUnsupported is a method the host does not implement.
	CodeUnsupported ErrorCode = "unsupported"
	// CodeInternal is an unexpected host failure.
	CodeInternal ErrorCode = "internal"
)

// WireError is the error body of a failed response.
//
// It carries a stable code separate from a human message, so a client can branch
// on the code while a person reads the message. Collapsing them into one string
// would make the code unparseable.
type WireError struct {
	// Code is the stable identifier.
	Code ErrorCode `json:"code"`
	// Message is a human explanation.
	Message string `json:"message"`
}

// Error implements the error interface.
func (e WireError) Error() string { return string(e.Code) + ": " + e.Message }

// Response is the answer to a request.
type Response struct {
	// ID echoes the request id.
	ID string `json:"id"`
	// Result is the success body.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is the failure body, when the request failed.
	Error *WireError `json:"error,omitempty"`
}

// Validate reports whether a response is well formed.
func (r Response) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("%w: a response id is required", ErrInvalidEnvelope)
	}
	if r.Error == nil && len(r.Result) == 0 {
		return fmt.Errorf("%w: a response needs a result or an error", ErrInvalidEnvelope)
	}
	if r.Error != nil && len(r.Result) != 0 {
		return fmt.Errorf("%w: a response cannot have both a result and an error", ErrInvalidEnvelope)
	}
	return nil
}

// DecodeEnvelope parses and validates an envelope from bytes.
//
// DisallowUnknownFields is deliberately not used for the envelope: a peer may
// send fields a newer minor build added, and refusing the whole message would be
// a version trap. Unknown *methods* are refused instead (see ErrUnknownMethod),
// because a silently ignored method call is worse than an error.
func DecodeEnvelope(data []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	if err := e.Validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// Encode marshals an envelope.
func (e Envelope) Encode() ([]byte, error) { return json.Marshal(e) }
