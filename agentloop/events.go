package agentloop

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// ErrEventRejected reports a turn event the log refused.
var ErrEventRejected = errors.New("agentloop: turn event rejected")

// turnStartData is the payload of a turn/start event.
//
// The driver writes the turn number it is about to run, so a reader can tell
// "turn 3 started" from "step 0 of turn 3 started" without inferring it from
// position. `input` records what began the turn, which is what makes the turn
// reconstructable from the log alone.
type turnStartData struct {
	// Turn is the turn number, counted from 0 within one Agent.
	Turn int `json:"turn"`
	// Input is the user text that began the turn.
	Input string `json:"input"`
}

// stepStartData is the payload of a step/start event.
type stepStartData struct {
	// Turn is the turn this step belongs to.
	Turn int `json:"turn"`
	// Step is the step number within the turn.
	Step int `json:"step"`
}

// stepEndData is the payload of a step/end event.
type stepEndData struct {
	// Turn is the turn this step belongs to.
	Turn int `json:"turn"`
	// Step is the step number within the turn.
	Step int `json:"step"`
	// Reason is why the step ended, mirroring the model finish kind.
	Reason string `json:"reason"`
	// ToolCalls counts the tool calls this step requested.
	ToolCalls int `json:"toolCalls,omitempty"`
}

// turnEndData is the payload of a turn/end event.
type turnEndData struct {
	// Turn is the turn number.
	Turn int `json:"turn"`
	// Steps counts the steps the turn ran. A turn closed before any step may
	// report zero, which is a legitimate outcome rather than an error.
	Steps int `json:"steps"`
	// Failed reports whether the turn ended because of a failure rather than a
	// natural stop.
	Failed bool `json:"failed,omitempty"`
	// Failure is the failure description, when there was one.
	Failure string `json:"failure,omitempty"`
	// Canceled reports whether the turn ended because the caller cancelled it.
	Canceled bool `json:"canceled,omitempty"`
}

// requestHeaderData is the payload of a request/header event.
//
// It records the request that was actually sent, so a reader can rebuild the
// model-visible input without re-deriving it: the provider, the model, the
// system prompt, and the tool names in the order they were offered.
type requestHeaderData struct {
	// Turn is the turn this request belongs to.
	Turn int `json:"turn"`
	// Step is the step this request belongs to.
	Step int `json:"step"`
	// Provider is the route the request was sent to.
	Provider string `json:"provider"`
	// Model is the model identifier.
	Model string `json:"model"`
	// SystemPrompt is the rendered system prompt, empty when there was none.
	SystemPrompt string `json:"systemPrompt,omitempty"`
	// Tools lists the tool names offered, in request order.
	Tools []string `json:"tools,omitempty"`
	// Messages counts the messages the request carried.
	Messages int `json:"messages"`
}

// attemptData is the payload of an assistant/attempt event.
type attemptData struct {
	// Turn is the turn this attempt belongs to.
	Turn int `json:"turn"`
	// Step is the step this attempt belongs to.
	Step int `json:"step"`
	// Attempt numbers the attempts within one step.
	Attempt int `json:"attempt"`
	// Kind is the finish kind, or "incomplete" when the stream ended without one.
	Kind string `json:"kind"`
	// Failure is the failure description for a failed attempt.
	Failure string `json:"failure,omitempty"`
	// Details counts the content blocks the attempt produced.
	Details int `json:"details,omitempty"`
}

// toolCallData is the payload of a tool/call event.
//
// The call is recorded *before* it executes, which is what lets a reader see that
// a call was requested even when the process died mid-execution, and what makes
// "claimed but never dispatched" distinguishable from "dispatched".
type toolCallData struct {
	// Turn is the turn this call belongs to.
	Turn int `json:"turn"`
	// Step is the step this call belongs to.
	Step int `json:"step"`
	// CallID correlates the call with its result.
	CallID string `json:"callId"`
	// Name is the requested tool name.
	Name string `json:"name"`
	// Arguments is the raw argument string as the model produced it.
	Arguments string `json:"arguments"`
}

// toolResultData is the payload of a tool/result event.
type toolResultData struct {
	// Turn is the turn this result belongs to.
	Turn int `json:"turn"`
	// Step is the step this result belongs to.
	Step int `json:"step"`
	// CallID answers the recorded call.
	CallID string `json:"callId"`
	// Name is the tool that was called.
	Name string `json:"name"`
	// Outcome is the normalized pipeline outcome.
	Outcome string `json:"outcome"`
	// IsError marks a failed or refused call.
	IsError bool `json:"isError,omitempty"`
	// Attempts records whether the tool body actually ran.
	Attempts int `json:"attempts"`
}

// writeEvent appends one event to the log, wrapping the error so a caller can
// tell a refused turn event from an unrelated failure.
func (a *instance) writeEvent(input session.AppendInput) (session.Event, error) {
	event, err := a.config.Log.Append(input)
	if err != nil {
		return session.Event{}, fmt.Errorf("%w: %s: %w", ErrEventRejected, input.Type, err)
	}
	return event, nil
}

// encode marshals an event payload, which cannot fail for these fixed structs.
func encode(payload any) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		// The payloads are compile-time structs of strings and ints, so this is
		// unreachable; panicking would hide a real bug behind an error path that
		// cannot be tested.
		panic(fmt.Sprintf("agentloop: cannot encode %T: %v", payload, err))
	}
	return data
}

// appendMessage records one message event on the surface.
//
// The payload shape differs by event type and the difference is not cosmetic:
// the surface reader decodes `user/message` with the message object at the top
// level, and the other four message-bearing types with it under "message". Using
// the wrong form produces a surface node that derives to nothing or an event the
// log refuses outright, so the shape is chosen here, once, from the one rule.
func (a *instance) appendMessage(eventType string, message llm.Message) (session.Event, error) {
	data, err := llm.MarshalMessage(message)
	if err != nil {
		return session.Event{}, fmt.Errorf("%w: %s: %w", ErrEventRejected, eventType, err)
	}
	payload := data
	if eventType != "user/message" {
		payload, err = encodeNested(struct{}{}, data)
		if err != nil {
			return session.Event{}, fmt.Errorf("%w: %s: %w", ErrEventRejected, eventType, err)
		}
	}
	return a.writeEvent(session.AppendInput{
		Type:      eventType,
		Data:      payload,
		SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	})
}

// appendToolCall records a requested call before it executes.
func (a *instance) appendToolCall(turn, step int, call tools.Call) error {
	_, err := a.writeEvent(session.AppendInput{
		Type: "tool/call",
		Data: encode(toolCallData{
			Turn: turn, Step: step,
			CallID: call.ID, Name: call.Name.String(), Arguments: call.Arguments,
		}),
	})
	return err
}

// appendToolResult records a settled result on the surface.
//
// The payload is the nested message form the surface reader expects: the harness
// fields (outcome, attempts) live alongside "message", and the message itself is
// what a fold turns into history. Writing the harness fields instead of the
// message would append a surface node that derives to nothing, silently losing
// the result from history.
func (a *instance) appendToolResult(turn, step int, result tools.Result, message llm.Message) error {
	inner, err := llm.MarshalMessage(message)
	if err != nil {
		return fmt.Errorf("%w: tool/result: %w", ErrEventRejected, err)
	}
	nested, err := encodeNested(toolResultData{
		Turn: turn, Step: step,
		CallID: result.CallID, Name: result.Name.String(),
		Outcome: string(result.Outcome), IsError: result.IsError, Attempts: result.Attempts,
	}, inner)
	if err != nil {
		return err
	}
	_, err = a.writeEvent(session.AppendInput{
		Type:      "tool/result",
		Data:      nested,
		SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	})
	return err
}

// encodeNested renders an event payload that carries a model message under
// "message", which is the shape the surface reader decodes for the four nested
// event types.
func encodeNested(payload any, message []byte) ([]byte, error) {
	fields, err := marshalToMap(payload)
	if err != nil {
		return nil, err
	}
	fields["message"] = json.RawMessage(message)
	return json.Marshal(fields)
}

// marshalToMap renders a payload to its field map so a nested key can be added
// without hand-writing a second struct that could drift from the first.
func marshalToMap(payload any) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// EncodePayloadForTest exposes the event encoder so a test can assert that an
// unencodable payload fails loudly. The helper accepts any value, so this is a
// reachable input rather than a hypothetical one.
func EncodePayloadForTest(payload any) []byte { return encode(payload) }

// EncodeNestedForTest exposes the nested-payload encoder for the same reason.
func EncodeNestedForTest(payload any, message []byte) ([]byte, error) {
	return encodeNested(payload, message)
}
