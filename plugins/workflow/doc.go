// Package workflow provides a deterministic fake workflow engine.
//
// The engine runs a validated script step by step in-process and records the
// outcome of each step, so a test or an example can observe orchestration without
// a production engine. It is the fake half of the workflow seam: the seam
// validates a script and its arguments, and this engine executes the steps so a
// caller can prove the orchestration shape before a real engine exists.
//
// The engine refuses a script whose capability it does not declare, so "the engine
// cannot run this" is decided from the declaration rather than discovered after a
// partial run.
package workflow
