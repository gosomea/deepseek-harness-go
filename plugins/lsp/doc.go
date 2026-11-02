// Package lsp provides a minimal LSP (Language Server Protocol) stdio client.
//
// LSP is JSON-RPC over stdio with the same lifecycle problems as MCP: a language
// server is a child process that must be started, correlated and reaped. This
// package owns that lifecycle with the same discipline — explicit id correlation
// and a guaranteed teardown — so a caller cannot leave a server running or
// mispair a response.
//
// It is keyless and testable in-process: a test supplies a reader/writer pair
// rather than a real language server.
package lsp
