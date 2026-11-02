// Package mcp provides a minimal MCP (Model Context Protocol) stdio client.
//
// The package exists because an MCP server is an external process with a
// lifecycle: it is started, spoken to over stdio with length-delimited JSON
// messages, and must be reaped. That lifecycle — start, request/response
// correlation, and a guaranteed teardown — is what this package owns, so a caller
// does not accidentally leak a child process or mispair a response.
//
// It is a keyless, in-process-testable seam: a test drives it with an
// io.Reader/io.Writer pair instead of a real server, so correlation and teardown
// are proven without a socket or a network.
package mcp
