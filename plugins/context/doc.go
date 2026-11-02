// Package context records what entered a model request, so the request can be
// rebuilt from the session log instead of guessed at.
//
// The package exists because "the model saw X" has to be a fact, not a
// side effect of when a plugin happened to run. Every contribution is written to
// the log before it is rendered; rendering is a pure function of the recorded
// contributions. The same log plus the same code therefore produces the same
// request, and a test can compare two renders byte for byte.
//
// Three things are kept apart on purpose:
//
//   - the log fact: an owned contribution appended to the session log;
//   - the render: deterministic text and message order derived from those facts;
//   - the request-time access path: a short-lived handle (for example a file
//     reference) that resolves at render time and is never itself the fact.
//
// A contribution that never reached the log cannot reach the request.
package context
