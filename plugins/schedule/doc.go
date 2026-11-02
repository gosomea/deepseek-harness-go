// Package schedule drives durable jobs on a controlled clock.
//
// The package exists because "run this later" is where a program's internal
// goroutine and a durable task diverge. A scheduler here ticks on an injected
// clock rather than wall time, so a test can advance time deterministically and
// observe exactly which jobs became due. It claims due jobs through the jobs
// store, so scheduling and running stay separable: the clock decides *when*, the
// runner decides *what*.
package schedule
