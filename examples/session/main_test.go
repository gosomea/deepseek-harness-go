package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestDemoMatchesDocumentedOutput pins the example's stdout to expected.txt, so the
// walkthrough in docs/session/projection.md cannot drift from what the program
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

// TestDemoIsDeterministic keeps two runs identical, which the fixed clock and the
// byte-stable codecs are responsible for.
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

// TestDemoShowsLogGrowthWithoutHistoryGrowth pins the behaviour the example exists
// to demonstrate: nine events, four surface nodes, and a failed attempt that stays
// in the log without becoming a message.
func TestDemoShowsLogGrowthWithoutHistoryGrowth(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"seq=2 assistant/attempt   surface=false derives=none",
		"log=9 surface=4",
		"history=4 contentGeneration=1 replaceGeneration=0",
		"refused=true",
		"unknown required refused=true logUnchanged=true",
		"missing surface marker refused=true logUnchanged=true",
		"bad replacement refused=true logUnchanged=true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}
