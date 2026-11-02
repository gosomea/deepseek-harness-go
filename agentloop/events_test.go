package agentloop_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// The event payloads must take the shapes the surface reader decodes, because a
// payload written in the wrong shape is either refused outright or appended as a
// surface node that derives to nothing.
func TestRecordedPayloadShapesMatchTheSurfaceRules(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"answer"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, event := range log.Events() {
		text := string(event.Data)
		switch event.Type {
		case "user/message":
			// The message object is at the top level: it carries an id directly.
			if !contains(text, `"id"`) || !contains(text, `"role":"user"`) {
				t.Fatalf("user/message must carry the message at the top level: %s", text)
			}
			if contains(text, `"message":{`) {
				t.Fatalf("user/message must not use the nested form: %s", text)
			}
		case "assistant/message", "tool/result":
			// These are nested: the harness fields live beside "message".
			if !contains(text, `"message":{`) {
				t.Fatalf("%s must carry the message under \"message\": %s", event.Type, text)
			}
		case "turn/start":
			if !contains(text, `"turn":0`) || !contains(text, `"input":"go"`) {
				t.Fatalf("turn/start must record the turn number and the input: %s", text)
			}
		case "step/start":
			if !contains(text, `"turn":0`) || !contains(text, `"step":0`) {
				t.Fatalf("step/start must cite its turn and step: %s", text)
			}
		case "request/header":
			for _, want := range []string{`"turn":0`, `"step":0`, `"provider":"fake"`, `"model":"fake-1"`, `"messages":1`} {
				if !contains(text, want) {
					t.Fatalf("request/header missing %s: %s", want, text)
				}
			}
		case "assistant/attempt":
			if !contains(text, `"attempt":1`) || !contains(text, `"kind":"stop"`) {
				t.Fatalf("assistant/attempt must number the try and name the outcome: %s", text)
			}
		}
	}
}

// A tool call and its result must cite the same call id, so a reader can pair
// them without relying on order.
func TestToolCallAndResultShareTheCallID(t *testing.T) {
	log := newLog(t)
	var bodyRuns int
	provider := testkit.NewFakeProvider(
		testkit.ScriptToolCallAttempt("call-42", "read_file", []string{`{"path":"a.txt"}`}),
		testkit.ScriptTextAttempt([]string{"done"}),
	)
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log, Pipeline: readFileTool(t, &bodyRuns)})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "read"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	var callID, resultID string
	for _, event := range log.Events() {
		switch event.Type {
		case "tool/call":
			callID = extractField(string(event.Data), "callId")
		case "tool/result":
			resultID = extractField(string(event.Data), "callId")
		}
	}
	if callID != "call-42" || resultID != "call-42" {
		t.Fatalf("the call and its result must share the id: %q vs %q", callID, resultID)
	}
}

// extractField pulls a JSON string field out of an event payload. It is a tiny
// reader rather than a decoder because the payloads under test are flat and the
// assertion is about which field carries which fact.
func extractField(payload, field string) string {
	needle := `"` + field + `":"`
	start := strings.Index(payload, needle)
	if start < 0 {
		return ""
	}
	rest := payload[start+len(needle):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
