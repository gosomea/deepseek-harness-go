// Package hostapi provides an in-process Host API gateway.
//
// The gateway dispatches versioned envelope requests to registered method
// handlers, enforces authentication on protected methods, and delivers
// notifications to subscribers. It exists as the host half of the protocol: the
// wire DTOs live in protocol, and this package is the dispatcher that owns
// correlation (by request id), authorization, and subscription fan-out.
//
// The gateway is transport-agnostic on purpose: a test drives it with an envelope
// in and a response out, with no socket. A networked transport wraps the same
// dispatch, so the authorization and correlation rules are proven once, keyless.
package hostapi
