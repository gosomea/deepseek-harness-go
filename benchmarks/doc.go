// Package benchmarks holds deterministic performance micro-benchmarks for the
// core request/session paths.
//
// The package exists so a performance claim has a fixed, reproducible input
// rather than an anecdote: each benchmark uses a synthetic, seeded workload with
// no model network, so a regression is visible without external latency
// polluting the numbers. Results are environment-bound and recorded separately
// from correctness gates.
package benchmarks
