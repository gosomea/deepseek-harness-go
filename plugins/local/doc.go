// Package local provides the capability implementations for the machine the Agent
// runs on: a workspace-confined filesystem, a process runner with bounded output
// and process-tree cancellation, and the tools that reach them.
//
// It is a Provider, not a policy. Every side effect it performs is preceded by the
// caller's policy check, and this package refuses to run when the platform cannot
// promise what the configuration requires. That separation is what lets the same
// tools run against a different world without the tools knowing.
package local
