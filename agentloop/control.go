package agentloop

import (
	"context"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/agent"
)

// Send delivers one input to the Agent's inbox.
//
// Input is accepted while the Agent is idle or running, which is what makes
// steering possible: a caller can add input during a turn and have it claimed by
// the next step rather than starting a competing turn. A disposed Agent refuses
// the input instead of silently dropping it.
func (a *instance) Send(ctx context.Context, input agent.Input) error {
	if err := input.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	disposed := a.disposed
	a.mu.Unlock()
	if disposed {
		return fmt.Errorf("%w: %s", agent.ErrInputRejected, agent.ErrClosed)
	}
	return a.inbox.push(input)
}

// Pending reports how many inputs are waiting to be claimed.
func (a *instance) Pending() int { return a.inbox.len() }

// Claimed reports how many inputs have been claimed by steps.
func (a *instance) Claimed() int { return a.inbox.claimedCount() }

// Dispose stops the Agent and releases its resources.
//
// It satisfies agent.Owner. Disposal closes the inbox first, so input that
// arrives during shutdown is refused rather than queued for a driver that will
// never run again.
func (a *instance) Dispose(ctx context.Context) error {
	a.inbox.close()
	return a.dispose(ctx)
}
