// Package scope gives one agent its own registration world.
//
// A scope pairs an opaque identity with a Cordis child context whose
// registrations are both scope-visible and scope-lifetime. Child scopes inherit
// ancestor contributions with the nearest definition winning, and an ancestor
// can observe a descendant's events; neither relation works in reverse.
//
// This is deliberately not the same thing as Cordis service isolation. Isolate
// partitions one service's resolution namespace; a scope tags registrations,
// relates identities in a chain, routes events and owns disposal. Handing out a
// scoped context hands out the minting plugin's injected services, so a scope
// neither widens nor narrows service visibility.
//
// A scope routes trusted in-process plugins. It is not a sandbox and not an
// authority boundary.
package scope
