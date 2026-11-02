// Package workflow defines the seam for a scripted multi-step orchestration.
//
// The package exists because "run this workflow" hides the difference between an
// orchestration description (a script with validated meta and args) and the thing
// that executes it (a provider, in-process for tests, external in production).
// Keeping the description and the executor apart is what lets a caller validate a
// script before handing it to an engine, and lets a fake engine stand in for a
// real one without the orchestration changing.
//
// A script is data, not code that the seam interprets: the seam validates the
// meta shape and the arguments and then asks a Provider to run it. An engine that
// does not know a script's capability refuses it here rather than mid-run.
package workflow
