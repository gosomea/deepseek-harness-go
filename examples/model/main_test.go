package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestDemoMatchesDocumentedOutput pins the example's stdout to expected.txt, so
// the walkthrough in docs/llm/streaming.md cannot drift from what it prints.
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

// TestDemoIsDeterministic keeps two runs identical: the scripted provider, the
// assembler and the adapter translation are all pure functions of their inputs.
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

// TestDemoShowsSplitArgumentsSurvivingAsOneCall pins the behaviour the example
// exists to demonstrate: three fragments of one argument string assemble into
// one tool call with the original bytes intact, and the adapter refuses what it
// cannot express instead of dropping it.
func TestDemoShowsSplitArgumentsSurvivingAsOneCall(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		`chunk=2 type=tool-call-delta index=0 text="" arguments="{\"pa"`,
		`chunk=3 type=tool-call-delta index=0 text="" arguments="th\":\"a.txt\"}"`,
		`block=0 kind=tool-call id=call-1 name=read_file arguments="{\"path\":\"a.txt\"}"`,
		"finish=tool-calls ok=true",
		"usage=input:5 output:4 reported=true",
		"unsupported=true",
		"names the block=true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}
