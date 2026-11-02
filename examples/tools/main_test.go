package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestDemoMatchesDocumentedOutput pins the example's stdout to expected.txt, so
// the walkthrough in docs/tools/execution-pipeline.md cannot drift from what it
// prints.
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

// TestDemoIsDeterministic keeps two runs identical: every stage and result is a
// pure function of the call and the definition.
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

// TestDemoShowsRefusalsNeverReachTheBody pins the claim the example exists to
// demonstrate: three refusals of different kinds, one body execution, and no
// execute stage for any refused call.
func TestDemoShowsRefusalsNeverReachTheBody(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"outcome=success attempts=1 isError=false",
		"outcome=input-invalid attempts=0 isError=true",
		"outcome=failed attempts=0 isError=true",
		"denied outcome=denied attempts=0 error=true",
		"bodyRuns=1",
		`schema carries the enforced constraint=true`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	// Only the admitted call may emit an execute stage.
	if got := strings.Count(text, "stage=execute"); got != 1 {
		t.Fatalf("want exactly one execute stage, got %d", got)
	}
}
