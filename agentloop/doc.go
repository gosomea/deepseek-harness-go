// Package agentloop is the default Agent driver: it turns a spec into a running
// Agent, executes turns, and owns everything it created.
//
// It is the only package that implements the agent.Factory seam for the built-in
// path, and it is deliberately free of provider-specific code. The model route
// arrives as an llm.Provider, tool execution arrives as a tools.Pipeline, and
// the log arrives as a session.Log: the loop orchestrates them and hardcodes
// none of them. That is what lets the same loop run against a fake provider in
// tests and a real endpoint in a later stage without a branch.
package agentloop
