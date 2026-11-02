// Package desktophost provides the Go side of a Desktop carrier startup bridge.
//
// The package exists because a desktop app is a private profile with its own
// lifecycle: it is launched with an injected credential, it must report readiness
// or a fatal startup error, and it must release its host and listeners on close.
// This package models that handshake on the Go side — readiness, fatal failure,
// and a guaranteed shutdown — so the rules are testable without a real desktop
// shell or code signing.
//
// The actual Electron/native shell packaging is a recorded gap: this package is
// the host half, not the desktop bundle.
package desktophost
