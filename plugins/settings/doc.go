// Package settings resolves configuration for one agent without letting values
// leak between scopes.
//
// The package exists because "what value did this agent actually use" has three
// possible answers that are easy to confuse: the schema default, the value the
// session overrode, and the value inherited from a parent scope. A resolver that
// merges them in an implicit order makes a wrong answer indistinguishable from a
// right one.
//
// Resolution is therefore explicit and layered: a default, then scope overrides
// from the root down to the target scope, then the session override. Each layer
// is recorded with where it came from, so a caller can ask not only what the
// value is but which layer supplied it.
package settings
