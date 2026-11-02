package agentloop

import (
	"context"

	"github.com/gosomea/deepseek-harness-go/agent"
)

// Cancel requests cancellation of the active turn without disposing the Agent.
//
// The distinction matters and is observable: after Cancel the Agent is still
// alive, its inbox still holds whatever was queued, and a later turn can run.
// The boolean reports whether there was active work to cancel.
func (a *instance) Cancel(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	a.mu.Lock()
	cancel := a.cancelled
	active := a.active
	disposed := a.disposed
	a.mu.Unlock()
	if disposed {
		return false, agent.ErrClosed
	}
	if !active || cancel == nil {
		return false, nil
	}
	cancel()
	return true, nil
}

// TurnCount reports how many turns this instance has started.
func (a *instance) TurnCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.turnSeq
}

// Compile-time assertions: the instance is deliberately usable as three
// different capability sets, and the narrower ones are what the factory hands
// out.
var (
	_ agent.Handle   = (*instance)(nil)
	_ agent.Owner    = (*instance)(nil)
	_ agent.Canceler = (*instance)(nil)
)
