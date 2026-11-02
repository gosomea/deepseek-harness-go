// Package subagent defines the seam through which a parent Agent creates, drives
// and releases a child Agent.
//
// The package exists because "run another Agent" hides three different questions.
// The first is who owns the child: a child is not a tool call that ends, it is an
// instance with its own lifetime, so someone must be able to dispose it and know
// when that finished. The second is what the child can do: not every provider can
// fork an existing conversation, continue one across a boundary, or inherit a
// parent's tool set, so a provider must declare its features instead of silently
// behaving like the ones it lacks. The third is where the child's facts live: a
// fork copies a prefix as inheritance, it does not re-create those facts as the
// child's own.
//
// A single Provider interface is therefore kept small, and capability is declared
// rather than probed, so a caller can decide before spawning whether a provider
// can satisfy what it needs. A provider that cannot continue or fork says so here;
// the alternative — accepting the request and failing later — is what this seam
// prevents.
package subagent
