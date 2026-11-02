// Package protocol defines the wire DTOs, version negotiation and error
// vocabulary a Go Host exposes to external clients.
//
// The package exists because an internal Go interface is not a stable protocol:
// a Go method can change signature with the compiler catching callers, while a
// wire message that changes shape breaks a client compiled months earlier. So
// this package names its own request/response values, freezes a protocol version,
// and treats an unknown field, method or version as an explicit, checkable
// condition rather than silently ignoring it.
//
// It is deliberately separate from session, llm and tools: those own the runtime
// types. A DTO here is the projection a remote client sees, and it never exposes
// an internal type by reference.
package protocol
