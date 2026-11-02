package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestDemoMatchesDocumentedOutput pins the example's stdout to expected.txt, so
// the walkthrough in docs/agentloop/turns.md cannot drift from what it prints.
func TestDemoMatchesDocumentedOutput(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("expected.txt")
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("demo output changed:\n%s", out.String())
	}
}

// TestDemoIsDeterministic keeps two runs identical: the scripted provider and the
// fixed clock make the whole turn reproducible.
func TestDemoIsDeterministic(t *testing.T) {
	var first, second bytes.Buffer
	if err := run(&first); err != nil {
		t.Fatal(err)
	}
	if err := run(&second); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatalf("output is not reproducible:\n%s\n%s", first.String(), second.String())
	}
}

// TestDemoShowsFullTurnWithToolCall pins the behaviour the example exists to
// demonstrate: two model requests, one tool execution, the recorded event order,
// and a second request that carries the tool result.
func TestDemoShowsFullTurnWithToolCall(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"seq=0 turn/start",
		"seq=1 user/message",
		"seq=2 step/start",
		"seq=3 request/header",
		"seq=4 assistant/attempt",
		"seq=5 assistant/message",
		"seq=6 tool/call",
		"seq=7 tool/result",
		"seq=8 step/end",
		"requests=2 toolBodyRuns=1",
		"message[1] role=assistant content=\"\" blocks=tool-call:read_file",
		"history=4",
		"second request carries the tool result=true",
		"state=disposed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	// The tool call must be recorded before its result, which is what lets a
	// reader distinguish a requested call from a settled one.
	callIndex := strings.Index(text, "seq=6 tool/call")
	resultIndex := strings.Index(text, "seq=7 tool/result")
	if callIndex < 0 || resultIndex < 0 || callIndex > resultIndex {
		t.Fatalf("tool/call must precede tool/result:\n%s", text)
	}
}
