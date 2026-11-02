// Command session replays one agent conversation from its authoritative event log.
//
// It writes a small log by hand — a turn boundary, a user prompt, a failed model
// attempt, a successful assistant reply, a tool call and its result — then shows
// two things a reader cannot see from a messages array: which events actually
// produce model history, and how a failed attempt stays in the log without
// becoming a message. Finally it rewrites one message's content through a
// projection, which changes history without changing the log's shape.
//
// The same sequence is explained step by step in docs/session/projection.md.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
)

// placeholderProjection stands in for a real content-rewriting plugin: it replaces
// the content of one existing message without changing its identity or its place.
type placeholderProjection struct {
	// text is the replacement content.
	text string
}

// Type reports the event type this interpreter owns.
func (placeholderProjection) Type() string { return "image/offload" }

// Project rewrites the target message's content.
//
// It reports the target through the event payload, reads the target from the
// supplied window, and returns a replacement keyed by the target's original
// position, which is what preserves the message's identity.
func (p placeholderProjection) Project(event session.Event, context session.ProjectionContext) (map[session.Seq]json.RawMessage, error) {
	var payload struct {
		// Target is the surface position whose message is rewritten.
		Target session.Seq `json:"target"`
	}
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, err
	}
	var target session.Event
	for _, candidate := range context.Events {
		if candidate.Seq == payload.Target {
			target = candidate
		}
	}
	// A user message carries the message directly; the other four carry it under
	// "message". A projection must handle the shape of the event it owns.
	raw := target.Data
	if target.Type != "user/message" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(target.Data, &fields); err != nil {
			return nil, err
		}
		raw = fields["message"]
	}
	message, err := llm.UnmarshalMessage(raw)
	if err != nil {
		return nil, err
	}
	// A projection rewrites content and must preserve everything else, including the
	// message's identity and its producer attribution. Rebuilding the value keeps
	// both, which is why this does not construct a fresh message.
	rewritten := message
	rewritten.Content = []llm.ContentBlock{llm.TextBlock{Text: p.text}}
	encoded, err := llm.MarshalMessage(rewritten)
	if err != nil {
		return nil, err
	}
	return map[session.Seq]json.RawMessage{payload.Target: encoded}, nil
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run builds the log, prints one line per event and the derived history, then shows
// the same log folded without an interpreter to make the projection's effect
// visible.
func run(out io.Writer) error {
	log, err := buildLog()
	if err != nil {
		return err
	}

	// One line per event: whether it can join the surface, and the text it derives
	// to. A log-only event has no derived message.
	fmt.Fprintln(out, "--- log ---")
	surface := log.Surface()
	for _, event := range log.Events() {
		line := fmt.Sprintf("seq=%d %-19s surface=%-5t", event.Seq, event.Type, event.IsSurfaceEligible())
		message, err := surface.DeriveEventMessage(event)
		if err != nil {
			return err
		}
		if message == nil {
			line += " derives=none"
		} else {
			line += fmt.Sprintf(" derives=%q", llm.ContentText(message.Content))
		}
		fmt.Fprintln(out, line)
	}

	// The surface is the model-visible position list. It is shorter than the log
	// because boundary markers, a tool call and a failed attempt never enter it,
	// and it is not shorter by exactly one because a projection adds no node.
	fmt.Fprintf(out, "log=%d surface=%d\n", log.Len(), len(log.SurfaceNodes()))

	fmt.Fprintln(out, "--- model history ---")
	messages, err := log.DeriveMessages()
	if err != nil {
		return err
	}
	for index, message := range messages {
		fmt.Fprintf(out, "[%d] %-9s id=%-8s %q\n", index, message.Role, message.ID, llm.ContentText(message.Content))
	}
	fmt.Fprintf(out, "history=%d contentGeneration=%d replaceGeneration=%d\n",
		len(messages), log.ContentGeneration(), log.ReplaceGeneration())

	// Folding the same log without the interpreter is refused, which is how a
	// reader finds out that a plugin owns part of this history.
	fmt.Fprintln(out, "--- without the interpreter ---")
	_, err = session.FoldSurface(log.Events())
	fmt.Fprintf(out, "refused=%t\n", err != nil)

	// A rejected append leaves the sequence, the surface and the history untouched.
	fmt.Fprintln(out, "--- rejection ---")
	before := log.Len()
	_, err = log.Append(session.AppendInput{Type: "future/note", Data: []byte(`{"x":1}`)})
	fmt.Fprintf(out, "unknown required refused=%t logUnchanged=%t\n", err != nil, log.Len() == before)
	_, err = log.Append(session.AppendInput{Type: "user/message", Data: []byte(`{}`)})
	fmt.Fprintf(out, "missing surface marker refused=%t logUnchanged=%t\n", err != nil, log.Len() == before)
	_, err = log.Append(session.AppendInput{Type: "user/message", Data: []byte(`{}`),
		SurfaceOp: &session.SurfaceOp{Op: session.SurfaceReplace, StartSeq: 5, EndSeq: 9}})
	fmt.Fprintf(out, "bad replacement refused=%t logUnchanged=%t\n", err != nil, log.Len() == before)
	return nil
}

// buildLog writes the example conversation into a fresh log.
//
// The clock is fixed so two runs print identical bytes.
func buildLog() (*session.Log, error) {
	log := session.NewLog(session.KnownTypes())
	moment := time.UnixMilli(1700000000000)
	if err := log.SetClock(func() time.Time { return moment }); err != nil {
		return nil, err
	}
	// Registering the interpreter is what makes the log accept an offload event;
	// without it the append is refused rather than silently ignored.
	if _, err := log.RegisterProjection(placeholderProjection{text: "[image omitted]"}); err != nil {
		return nil, err
	}
	appendSurface := &session.SurfaceOp{Op: session.SurfaceAppend}
	inputs := []session.AppendInput{
		// A turn opens. It is log-only: the model never sees a boundary.
		{Type: "turn/start", Data: []byte(`{"turn":1}`)},
		// The human prompt is the first surface node.
		{Type: "user/message", Data: userData("u1", "read a.txt"), SurfaceOp: appendSurface},
		// A model attempt that produced nothing usable. It stays in the log so the
		// failure is diagnosable, and it never becomes a message.
		{Type: "assistant/attempt", Data: []byte(`{"turn":1,"step":1}`)},
		// The retry that succeeded.
		{Type: "assistant/message", Data: assistantData("a1", "let me read it", "call-1"), SurfaceOp: appendSurface},
		// The tool call itself is log-only; its result is what the model sees.
		{Type: "tool/call", Data: []byte(`{"turn":1,"step":1,"callId":"call-1","name":"read","arguments":"{\"path\":\"a.txt\"}"}`)},
		{Type: "tool/result", Data: toolResultData("t1", "call-1", "file contents"), SurfaceOp: appendSurface},
		// A second prompt, which a projection will later rewrite.
		{Type: "user/message", Data: userData("u2", "what about the screenshot?"), SurfaceOp: appendSurface},
		// The offload decision. It requires an interpreter to be registered on
		// this log, because its model-visible effect is not expressible by the
		// surface op alone.
		{Type: "image/offload", Data: []byte(`{"target":6}`)},
		// The turn closes.
		{Type: "turn/end", Data: []byte(`{"turn":1,"reason":"stop"}`)},
	}
	for _, input := range inputs {
		if _, err := log.Append(input); err != nil {
			return nil, fmt.Errorf("append %s: %w", input.Type, err)
		}
	}
	return log, nil
}

// userData builds a user message payload.
func userData(id, text string) []byte {
	return marshal(map[string]any{
		"id": id, "role": "user", "content": []any{map[string]any{"type": "text", "text": text}},
		"source": map[string]any{"kind": "user"},
	})
}

// assistantData builds an assistant message payload carrying one tool call.
func assistantData(id, text, callID string) []byte {
	return marshal(map[string]any{
		"turn": 1, "step": 1,
		"message": map[string]any{
			"id": id, "role": "assistant",
			"content": []any{
				map[string]any{"type": "text", "text": text},
				map[string]any{"type": "tool-call", "id": callID, "name": "read", "arguments": `{"path":"a.txt"}`},
			},
			"source": map[string]any{"kind": "model", "provider": "fake", "model": "fake-1"},
		},
	})
}

// toolResultData builds a tool result payload answering one call.
func toolResultData(id, callID, text string) []byte {
	return marshal(map[string]any{
		"turn": 1, "step": 1,
		"message": map[string]any{
			"id": id, "role": "tool",
			"content":    []any{map[string]any{"type": "text", "text": text}},
			"source":     map[string]any{"kind": "tool", "callId": callID},
			"toolCallId": callID,
		},
	})
}

// marshal encodes a payload, panicking only on a programming error in this file.
func marshal(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
