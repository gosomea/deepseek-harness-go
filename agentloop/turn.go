package agentloop

import (
	"context"
	"errors"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// Run executes one complete turn: claim input, start the turn, then run steps
// until the model stops or a step requests tools that are executed and answered.
//
// It is the sequence the reference calls "turn → step → request → settlement →
// tool batch → next step". The turn is closed in every exit path, including
// failure and cancellation, so a reader never sees a turn that started and never
// ended.
func (a *instance) Run(ctx context.Context) error {
	inputs := a.inbox.claim()
	if len(inputs) == 0 {
		// No input means no turn. The Agent stays idle rather than opening a turn
		// that would spend no step, which is the difference the reference draws
		// between "a turn with zero steps" and "no turn at all".
		return nil
	}

	runCtx, cancel, err := a.begin(ctx)
	if err != nil {
		return err
	}
	turn := a.currentTurn()
	defer func() { cancel() }()

	begin := inputs[0]
	if _, err := a.writeEvent(appendInput("turn/start", encode(turnStartData{
		Turn: turn, Input: begin.Text,
	}))); err != nil {
		a.settle(err)
		return err
	}
	// Every claimed input becomes a user message, so the model sees all of it
	// and the log records all of it. The first input is the one named by
	// turn/start; the rest are steering that arrived before the turn began.
	for _, input := range inputs {
		message, err := llm.NewMessage(llm.RoleUser, llm.TextContent(input.Text), llm.MessageSource{Kind: llm.SourceUser})
		if err != nil {
			a.closeTurn(turn, 0, err)
			a.settle(err)
			return err
		}
		if _, err := a.appendMessage("user/message", message); err != nil {
			a.closeTurn(turn, 0, err)
			a.settle(err)
			return err
		}
	}

	steps, runErr := a.runSteps(runCtx, turn)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		a.closeTurn(turn, steps, runErr)
		a.settle(runErr)
		return runErr
	}
	if errors.Is(runErr, context.Canceled) {
		a.closeTurnCanceled(turn, steps)
		if err := context.Cause(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			a.settle(err)
			return err
		}
		a.settle(context.Canceled)
		return context.Canceled
	}
	a.closeTurn(turn, steps, nil)
	a.settle(nil)
	return nil
}

// stepOutcome is what one step decided.
type stepOutcome struct {
	// reason is the finish kind the step ended with.
	reason llm.FinishKind
	// calls are the tool calls the step requested.
	calls []tools.Call
	// answeredTools reports that this step ran tool calls and recorded their
	// results, so the next step must ask the model what to do with them. It is
	// the only reason a step forces another one; queued steering is decided by
	// the caller, which can see the inbox.
	answeredTools bool
}

// runStep executes one step: build and record the request, stream the response,
// settle it, then run any requested tool calls and record their results.
func (a *instance) runStep(ctx context.Context, turn, step int) (stepOutcome, error) {
	request, header, err := a.buildRequest(turn, step)
	if err != nil {
		return stepOutcome{}, err
	}
	// The request is recorded before it is sent, so a crash mid-stream still
	// leaves the exact model-visible input in the log.
	if _, err := a.writeEvent(appendInput("request/header", encode(header))); err != nil {
		return stepOutcome{}, err
	}

	stream, err := a.config.Provider.Generate(ctx, request)
	if err != nil {
		// A provider that cannot start is an attempt failure, and the attempt is
		// recorded so the log shows the try rather than only its absence.
		attempt := attemptData{Turn: turn, Step: step, Attempt: 1, Kind: string(llm.FinishError), Failure: err.Error()}
		if _, writeErr := a.writeEvent(appendInput("assistant/attempt", encode(attempt))); writeErr != nil {
			return stepOutcome{}, writeErr
		}
		return stepOutcome{}, err
	}

	assembler := llm.NewAssembler()
	blocks, assembleErr := assembler.Assemble(stream)
	// A cancelled context is the cause, and a cut stream is only its symptom:
	// reporting the symptom would hide the cancellation from the caller and make
	// "this turn was cancelled" indistinguishable from "the provider failed".
	if ctxErr := ctx.Err(); ctxErr != nil {
		attempt := attemptData{
			Turn: turn, Step: step, Attempt: 1, Kind: "aborted",
			Failure: ctxErr.Error(), Details: len(blocks),
		}
		if _, err := a.writeEvent(appendInput("assistant/attempt", encode(attempt))); err != nil {
			return stepOutcome{}, err
		}
		return stepOutcome{}, ctxErr
	}
	reason, ok := assembler.FinishReason()
	attempt := attemptData{Turn: turn, Step: step, Attempt: 1, Details: len(blocks)}
	switch {
	case !ok:
		// The stream ended without a terminal chunk: an incomplete attempt is
		// recorded as such and never presented as a successful message.
		attempt.Kind = "incomplete"
		attempt.Failure = llm.ErrIncompleteStream.Error()
		if _, err := a.writeEvent(appendInput("assistant/attempt", encode(attempt))); err != nil {
			return stepOutcome{}, err
		}
		return stepOutcome{}, llm.ErrIncompleteStream
	case reason.Kind == llm.FinishError || reason.Kind == llm.FinishAborted:
		attempt.Kind = string(reason.Kind)
		attempt.Failure = reason.Failure.Message
		if _, err := a.writeEvent(appendInput("assistant/attempt", encode(attempt))); err != nil {
			return stepOutcome{}, err
		}
		return stepOutcome{}, &llm.FailureError{Failure: reason.Failure}
	default:
		attempt.Kind = string(reason.Kind)
		if _, err := a.writeEvent(appendInput("assistant/attempt", encode(attempt))); err != nil {
			return stepOutcome{}, err
		}
	}
	if assembleErr != nil {
		return stepOutcome{}, assembleErr
	}

	message, err := assembler.Message(header.Provider, header.Model)
	if err != nil {
		return stepOutcome{}, err
	}
	if _, err := a.appendMessage("assistant/message", message); err != nil {
		return stepOutcome{}, err
	}

	calls := collectToolCalls(blocks)
	if len(calls) == 0 {
		return stepOutcome{reason: reason.Kind}, nil
	}
	if err := a.executeCalls(ctx, turn, step, calls); err != nil {
		return stepOutcome{reason: reason.Kind, calls: calls}, err
	}
	return stepOutcome{reason: reason.Kind, calls: calls, answeredTools: true}, nil
}

// buildRequest derives the model request from the log.
//
// It deliberately rebuilds from history rather than accumulating a request in
// memory: the log is the authority, and a request derived from it is by
// construction reconstructable from it.
func (a *instance) buildRequest(turn, step int) (llm.Request, requestHeaderData, error) {
	history, err := a.config.Log.DeriveMessages()
	if err != nil {
		return llm.Request{}, requestHeaderData{}, err
	}
	// The route comes from the spec, which the caller set; the driver does not
	// substitute a default here. A missing route would be a caller bug that the
	// provider reports, and inventing one would hide it behind a route nobody
	// configured.
	provider := a.info.Provider
	model := a.info.Model
	prompt, err := a.config.SystemFragments.prompt(a.info.Scope.String())
	if err != nil {
		return llm.Request{}, requestHeaderData{}, err
	}
	messages := history
	if prompt != "" {
		system, err := llm.NewSystemMessage(prompt)
		if err != nil {
			return llm.Request{}, requestHeaderData{}, err
		}
		messages = append([]llm.Message{system}, messages...)
	}
	if len(messages) == 0 {
		return llm.Request{}, requestHeaderData{}, fmt.Errorf("%w: request would carry no message", ErrInvalidConfig)
	}
	definitions := a.config.SystemFragments.toolsFor(a.info.Scope.String())
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name.String())
	}
	request := llm.Request{Provider: provider, Model: model, Messages: messages}
	header := requestHeaderData{
		Turn: turn, Step: step, Provider: provider, Model: model,
		SystemPrompt: prompt, Tools: names, Messages: len(messages),
	}
	return request, header, nil
}

// collectToolCalls extracts the tool calls from assembled blocks, preserving
// order so results are recorded in the order the model asked for them.
func collectToolCalls(blocks []llm.ContentBlock) []tools.Call {
	var calls []tools.Call
	for _, block := range blocks {
		call, ok := block.(llm.ToolCallBlock)
		if !ok {
			continue
		}
		calls = append(calls, tools.Call{ID: call.ID.String(), Name: tools.ToolName(call.Name), Arguments: call.Arguments})
	}
	return calls
}

// executeCalls records each requested call, runs it through the pipeline, and
// records the settled result.
//
// A call is written to the log before it runs: that ordering is what makes "the
// model requested this" true even if the process dies mid-execution, and what
// lets a reader tell a dispatched call from one that was claimed but never sent.
func (a *instance) executeCalls(ctx context.Context, turn, step int, calls []tools.Call) error {
	for _, call := range calls {
		if err := a.appendToolCall(turn, step, call); err != nil {
			return err
		}
		result, execErr := a.executeCall(ctx, call)
		message, err := ToolResultMessage(result)
		if err != nil {
			return err
		}
		if err := a.appendToolResult(turn, step, result, message); err != nil {
			return err
		}
		if execErr != nil && ctx.Err() != nil {
			// Cancellation stops the batch: a result was recorded for the call in
			// flight, and no further call starts.
			return ctx.Err()
		}
	}
	return nil
}

// executeCall runs one call through the pipeline, or produces a normalized
// refusal when no pipeline is configured.
func (a *instance) executeCall(ctx context.Context, call tools.Call) (tools.Result, error) {
	if a.config.Pipeline == nil {
		result := tools.Result{
			CallID: call.ID, Name: call.Name, Outcome: tools.OutcomeFailed, IsError: true,
			Content: "no tool pipeline is configured",
		}
		return result, fmt.Errorf("%w: no tool pipeline", ErrInvalidConfig)
	}
	return a.config.Pipeline.Execute(ctx, call)
}

// ToolResultMessage builds the model-visible tool message for one result.
//
// It is exported because the harness example prints what the model sees: a
// test-only spelling would force the example to reconstruct the message and risk
// explaining something the driver does not actually send.
//
// The message always carries text, including for failures: a model that asked for
// a tool must learn what happened, and an empty result would leave it retrying
// blind.
func ToolResultMessage(result tools.Result) (llm.Message, error) {
	content := llm.TextContent(result.Content)
	return llm.NewToolResultMessage(llm.ToolCallID(result.CallID), content, result.IsError)
}

// currentTurn reports the turn number this instance is about to run.
func (a *instance) currentTurn() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.turnSeq - 1
}

// closeTurn writes the turn/end event for a completed or failed turn.
func (a *instance) closeTurn(turn, steps int, cause error) {
	payload := turnEndData{Turn: turn, Steps: steps}
	if cause != nil {
		payload.Failed = true
		payload.Failure = cause.Error()
	}
	_, _ = a.writeEvent(appendInput("turn/end", encode(payload)))
}

// closeTurnCanceled writes the turn/end event for a cancelled turn.
//
// Cancellation is recorded as its own fact rather than as a failure: a caller
// that cancelled must be able to tell that from a provider error.
func (a *instance) closeTurnCanceled(turn, steps int) {
	_, _ = a.writeEvent(appendInput("turn/end", encode(turnEndData{
		Turn: turn, Steps: steps, Canceled: true,
	})))
}

// appendInput builds a log input for a turn event.
//
// It exists so the session.AppendInput construction is written once: every turn
// event has exactly the same shape, and a caller that passed a different one
// would be recording something the reader cannot classify.
func appendInput(eventType string, data []byte) session.AppendInput {
	return session.AppendInput{Type: eventType, Data: data}
}
