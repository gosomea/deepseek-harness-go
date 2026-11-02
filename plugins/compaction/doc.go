// Package compaction replaces a region of history with a summary, and never
// reports a replacement that did not happen.
//
// The package exists because a summary is a model call, and model calls fail. The
// dangerous shape is a partial one: the region is marked replaced while the
// summary never arrived, or the summary is written while the replacement failed.
// Both leave a history that disagrees with itself.
//
// The flow is therefore three observable phases — start, summary, end — and the
// middle one can fail without claiming success. A caller can always tell whether
// the replacement finished by looking at what it got back.
package compaction
