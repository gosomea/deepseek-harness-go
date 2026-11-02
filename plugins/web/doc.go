// Package web provides web fetch and search capabilities behind a seam.
//
// The package exists because web content is external, untrusted input: it may be
// huge, slow, or hostile. A fetcher here returns bounded, structured results and
// treats the body as data, never as instructions. Search is a separate small
// interface so a host that only fetches does not depend on a search provider.
//
// The provider is a seam, not a bundled client: a real network client and a
// keyless mock satisfy the same interface, so a test drives redirection, size
// caps and timeouts without a socket, and a live smoke is recorded separately.
package web
