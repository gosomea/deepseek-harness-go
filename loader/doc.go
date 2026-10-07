// Package loader assembles a plugin tree from a JSON configuration document.
//
// The loader owns the difference between a configuration entry and the running
// plugin it names. An Entry is one configured node with a stable identity inside
// its tree; the catalog maps an entry's plugin name to a concrete factory, so the
// document refers to plugins by name without importing them itself. M2.1 covers
// entry identity, the factory catalog and the JSON codec; mounting entries into a
// parent/child tree and applying them belongs to the later M2 slices.
//
// Read docs/loader/go-primer.md before this package for the Go-to-DSH mapping.
package loader
