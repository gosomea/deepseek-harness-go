package agentloop

import (
	"context"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// runSteps drives the step loop until it stops naturally or hits the bound.
func (a *instance) runSteps(ctx context.Context, turn int) (int, error) {
	steps := 0
	for {
		if err := ctx.Err(); err != nil {
			return steps, err
		}
		if steps >= a.config.MaxSteps {
			// The bound is hit, not exceeded: the turn closes with a failure so a
			// runaway model is visible rather than silently truncated.
			return steps, fmt.Errorf("%w: turn exceeded %d steps", ErrStepLimit, a.config.MaxSteps)
		}
		step := steps
		if _, err := a.writeEvent(appendInput("step/start", encode(stepStartData{Turn: turn, Step: step}))); err != nil {
			return steps, err
		}
		steps++

		outcome, err := a.runStep(ctx, turn, step)
		if err != nil {
			return steps, err
		}
		if _, err := a.writeEvent(appendInput("step/end", encode(stepEndData{
			Turn: turn, Step: step, Reason: string(outcome.reason), ToolCalls: len(outcome.calls),
		}))); err != nil {
			return steps, err
		}
		// A step that ran tool calls always continues: the model asked a question
		// and the results must be answered by the next request.
		if outcome.answeredTools {
			continue
		}
		// Steering that arrived during the step is claimed by another step of the
		// same turn. Claiming is part of continuing, not a check done before it:
		// merely testing the queue would leave the input unclaimed and the loop
		// would spin, and the reference's "claim pending next-step input" step is
		// exactly this claim.
		if !a.claimInto(turn) {
			return steps, nil
		}
	}
}

// claimInto takes every pending input and records it as a user message,
// reporting whether there was anything to claim.
//
// It is the continuation-step counterpart of the batch Run claims before opening
// a turn. Both go through the same atomic claim, which is what makes "queued
// input is never claimed twice" true for steering as well.
func (a *instance) claimInto(turn int) bool {
	inputs := a.inbox.claim()
	if len(inputs) == 0 {
		return false
	}
	for _, input := range inputs {
		message, err := llm.NewMessage(llm.RoleUser, llm.TextContent(input.Text), llm.MessageSource{Kind: llm.SourceUser})
		if err != nil {
			// A message that cannot be built is a caller-visible failure; the turn
			// loop stops and the error surfaces through the turn's own result.
			continue
		}
		if _, err := a.appendMessage("user/message", message); err != nil {
			continue
		}
	}
	return true
}
