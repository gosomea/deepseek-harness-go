// Package capability holds the seams through which an Agent reaches the world
// outside its own process: a workspace to anchor paths, a filesystem to read and
// write, a subprocess runner, a sandbox to confine, and an approval channel to
// ask about side effects.
//
// Every seam is a consumer-defined interface: the tool layer states what it
// needs, and a Provider satisfies it. That direction matters — it is what lets
// the same tools run against a local machine, a remote host, or a test double
// without the tools learning which one they have.
//
// One rule runs through all of them: nothing reaches the world before policy has
// had its say. A capability that cannot say whether an action is permitted must
// refuse the action, never assume it is fine.
package capability
