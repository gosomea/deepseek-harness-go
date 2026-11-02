package agentloop

import "context"

// Runner drives an Agent's turns.
//
// It is separate from agent.Handle on purpose: a consumer that only reads and
// sends does not need the ability to pump the driver, and naming it separately
// keeps the permission explicit. The harness example and the tests use it; a
// production caller normally lets the factory schedule runs.
type Runner interface {
	// RunTurn executes one turn: claim pending input, open the turn, run steps
	// until it stops, and close the turn in every exit path.
	RunTurn(ctx context.Context) error
}

// RunTurn implements Runner.
func (a *instance) RunTurn(ctx context.Context) error { return a.Run(ctx) }

// RunForTest is the entry the tests and examples use; it is RunTurn under a name
// that states its audience, so a reader of a failing test knows who called it.
func (a *instance) RunForTest(ctx context.Context) error { return a.Run(ctx) }

// AsRunner returns the runner capability of a handle, reporting whether it has
// one. A handle produced by another driver does not, which is why this is a
// checked conversion rather than an interface assertion at the call site.
func AsRunner(handle any) (Runner, bool) {
	runner, ok := handle.(Runner)
	return runner, ok
}

var _ Runner = (*instance)(nil)
