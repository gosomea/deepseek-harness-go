// Package llmtrpc adapts the DSH-Go model contract to the tRPC-Agent-Go model
// layer.
//
// This package is the project's only import boundary for the framework: core
// packages define their own request, stream and message types, and this
// adapter converts between them. That keeps the framework's Request, Response
// and Session types out of the durable formats and out of every other package,
// so replacing the model library cannot ripple through the harness.
//
// The adapter owns translation, capability declaration and lifecycle. It does
// not own retry policy: the harness records attempts, and enabling the SDK's
// own retries on top would produce unobservable duplicate requests.
package llmtrpc
