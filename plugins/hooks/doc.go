// Package hooks provides a hook bridge for lifecycle injection.
//
// The package exists because an external extension often needs to observe or modify
// a lifecycle event (a turn starting, a tool executing) without being compiled
// into the host. A hook bridge routes named events to registered handlers and
// enforces that a handler's failure is observable rather than silently swallowed.
//
// It is a small, in-process seam: real external plugin channels are a later
// concern, but the routing and error-visibility rules are proven here keyless.
package hooks
