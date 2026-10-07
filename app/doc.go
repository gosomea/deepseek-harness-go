// Package app composes a plugin tree from named bundles and a profile.
//
// A bundle is a named, ordered list of patches. A profile lists bundles in
// application order together with the entries that must run. Composing applies
// every layer in order over an empty entry list, so a later layer can override a
// row an earlier layer contributed, and the result is an ordinary entry
// declaration that loader mounts unchanged.
//
// The package also decides readiness: an entry the profile marks required must
// reach an active instance, while a sibling that is not required only has to be
// reported. That split is what lets a host tell a fatal profile apart from a
// partial one without treating every failed plugin as fatal.
//
// Bundles here are Go values registered by the host, not packages fetched at
// runtime: this package supports explicit compile-time registration only.
package app
