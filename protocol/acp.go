package protocol

import (
	"encoding/json"
	"fmt"
)

// ACPMessageKind is the role of an ACP bridge message.
type ACPMessageKind string

// The ACP message kinds this protocol bridges.
const (
	// ACPRequest is a client request carried over ACP.
	ACPRequest ACPMessageKind = "request"
	// ACPResponse is a host response carried over ACP.
	ACPResponse ACPMessageKind = "response"
	// ACPNotification is a server-pushed event.
	ACPNotification ACPMessageKind = "notification"
)

// ACPMessage is one ACP bridge envelope.
//
// ACP carries the same request/response/notification triad as the native
// protocol, so this value maps a native Envelope onto an ACP frame without
// inventing semantics. It is a DTO: it names only wire fields, never an internal
// session or model type.
type ACPMessage struct {
	// Kind is request, response or notification.
	Kind ACPMessageKind `json:"kind"`
	// ID correlates a response with its request.
	ID string `json:"id"`
	// Method is the request method for a request.
	Method string `json:"method,omitempty"`
	// Body is the kind-specific payload.
	Body json.RawMessage `json:"body,omitempty"`
}

// Validate reports whether an ACP message is well formed.
func (m ACPMessage) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("%w: acp message requires an id", ErrInvalidEnvelope)
	}
	switch m.Kind {
	case ACPRequest:
		if m.Method == "" {
			return fmt.Errorf("%w: acp request requires a method", ErrInvalidEnvelope)
		}
	case ACPResponse, ACPNotification:
		// method is not required for these kinds.
	default:
		return fmt.Errorf("%w: unknown acp kind %q", ErrInvalidEnvelope, m.Kind)
	}
	return nil
}

// ToACP maps a native envelope onto an ACP request frame.
//
// A response envelope (method "response") maps to an ACP response; anything else
// maps to an ACP request. The mapping is total and lossless for the fields ACP
// names, so a native client and an ACP client observe the same correlation id.
func ToACP(env Envelope) (ACPMessage, error) {
	if err := env.Validate(); err != nil {
		return ACPMessage{}, err
	}
	kind := ACPRequest
	if env.Method == "response" {
		kind = ACPResponse
	}
	return ACPMessage{Kind: kind, ID: env.ID, Method: env.Method, Body: env.Payload}, nil
}

// FromACP maps an ACP request/response frame back onto a native envelope.
func FromACP(msg ACPMessage) (Envelope, error) {
	if err := msg.Validate(); err != nil {
		return Envelope{}, err
	}
	method := msg.Method
	if msg.Kind == ACPResponse && method == "" {
		method = "response"
	}
	return Envelope{Version: Version, ID: msg.ID, Method: method, Payload: msg.Body}, nil
}
