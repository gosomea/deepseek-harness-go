// Package ssh provides a remote execution Provider seam.
//
// The package exists because "run these tools on another machine" is where the
// local execution world stops being enough: paths, working directory, credentials
// and connection state all change, while the tool contract must not. This package
// names a remote Provider interface with the same shape as the local one, so a
// caller swaps the execution world without changing the tools.
//
// It is keyless and testable in-process: an in-memory Provider records the remote
// world's behaviour (path resolution, connection state) so a test proves the seam
// without a real SSH host. A real SSH client is recorded as an unverified gap
// until a host is available.
package ssh
