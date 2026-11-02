package agentloop

import (
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/agent"
)

// inbox holds input waiting to be claimed by a step.
//
// It is a FIFO queue with one property that matters for correctness: claiming is
// atomic, so two steps cannot both take the same input. The reference calls this
// out as "queued input is not claimed twice", and the only way to make that true
// is for the claim itself to remove the entries it returns.
type inbox struct {
	mu sync.Mutex
	// pending holds unclaimed input in arrival order.
	pending []agent.Input
	// closed refuses further pushes. It is set by disposal.
	closed bool
	// claimed counts inputs taken by a step. It exists so a test can assert that
	// steering was consumed exactly once.
	claimed int
}

// newInbox returns an empty inbox.
func newInbox() *inbox { return &inbox{} }

// push appends one input.
func (b *inbox) push(input agent.Input) error {
	if err := input.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fmt.Errorf("%w: inbox is closed", agent.ErrInputRejected)
	}
	b.pending = append(b.pending, input)
	return nil
}

// claim takes every pending input, leaving the inbox empty.
//
// It returns a copy so a caller that keeps the slice cannot mutate the queue, and
// it clears the queue inside the same critical section that produced the copy,
// which is what makes double-claiming impossible. A claim with nothing pending
// returns nil rather than an error: "no input" is a normal state, not a failure.
func (b *inbox) claim() []agent.Input {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) == 0 {
		return nil
	}
	claimed := append([]agent.Input(nil), b.pending...)
	b.pending = b.pending[:0]
	b.claimed += len(claimed)
	return claimed
}

// len reports how many inputs are pending.
func (b *inbox) len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

// claimedCount reports how many inputs have been claimed so far.
func (b *inbox) claimedCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.claimed
}

// close refuses further input. It is idempotent and preserves what is pending so
// a caller can decide whether to drain it.
func (b *inbox) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}
