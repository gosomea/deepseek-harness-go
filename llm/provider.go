package llm

import (
	"context"
	"errors"
	"fmt"
)

// ErrProviderNotFound reports a route no registered provider owns.
var ErrProviderNotFound = errors.New("llm: provider not found")

// ErrProviderClosed reports use of a provider registry after it was closed.
var ErrProviderClosed = errors.New("llm: provider registry closed")

// Capability names one provider feature a caller may depend on.
//
// Capabilities are declared rather than probed because a caller must be able to
// decide before a request whether the route can satisfy it. A provider that
// cannot do something says so here; the alternative, silently dropping a field,
// is what the reference forbids.
type Capability string

// The capabilities this package understands.
const (
	// CapabilityStreaming means the provider can deliver chunks incrementally.
	CapabilityStreaming Capability = "streaming"
	// CapabilityReasoning means the provider can emit reasoning content.
	CapabilityReasoning Capability = "reasoning"
	// CapabilityToolCalls means the provider can request tool calls.
	CapabilityToolCalls Capability = "tool-calls"
	// CapabilityUsage means the provider reports token accounting.
	CapabilityUsage Capability = "usage"
)

// UnsupportedCapabilityError reports a request that needs a capability the
// selected route does not declare.
//
// It is a distinct type so a caller can tell "this route cannot do that" from
// "the request failed": the first is a routing decision, the second is an
// attempt failure.
type UnsupportedCapabilityError struct {
	// Provider is the route key that was selected.
	Provider string
	// Capability is the missing feature.
	Capability Capability
}

// Error implements the error interface.
func (e *UnsupportedCapabilityError) Error() string {
	return fmt.Sprintf("llm: provider %q does not support %s", e.Provider, e.Capability)
}

// ProviderInfo describes one registered route.
type ProviderInfo struct {
	// ID is the route key callers select with Request.Provider.
	ID string
	// Name is the human-readable provider name for diagnostics.
	Name string
	// Capabilities is the declared feature set, in unspecified order.
	Capabilities []Capability
}

// Supports reports whether the route declares one capability.
func (i ProviderInfo) Supports(capability Capability) bool {
	for _, declared := range i.Capabilities {
		if declared == capability {
			return true
		}
	}
	return false
}

// Provider converts one request into one stream.
//
// The interface is deliberately two methods wide: a provider that also needs
// discovery, credentials or batch execution adds those through separate small
// interfaces, so a consumer that only generates does not depend on them.
type Provider interface {
	// Generate starts one attempt. A non-nil error means the attempt could not
	// be started at all, which is distinct from a failure reported inside the
	// stream by a terminal error finish.
	Generate(ctx context.Context, request Request) (Stream, error)
	// Info describes the route this provider serves.
	Info() ProviderInfo
}

// CapabilityProvider is implemented by providers that declare capabilities.
// Every provider in this package does; the interface exists so a consumer can
// depend on the narrower declaration check alone.
type CapabilityProvider interface {
	// Info describes the route and its declared capabilities.
	Info() ProviderInfo
}
