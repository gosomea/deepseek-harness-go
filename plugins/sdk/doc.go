// Package sdk provides a Go client for the Host API gateway.
//
// The client turns method calls into versioned protocol envelopes and matches the
// response back by request id. It exists so a remote client and an in-process
// caller share one calling convention: the client is transport-agnostic and is
// driven by a "send" function, which in a test is the gateway itself and in
// production is a socket.
//
// Cancellation is expressed through the context: a cancelled call returns without
// claiming a result, so "the answer did not arrive" and "the answer was empty"
// stay distinct.
package sdk
