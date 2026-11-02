package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestReplayMatchesDocumentedOutput pins the demonstration's stdout to
// expected.txt, so the walkthrough in docs/persistence/recovery.md cannot drift
// from what the program prints.
func TestReplayMatchesDocumentedOutput(t *testing.T) {
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

// TestReplayIsDeterministic keeps two runs identical.
func TestReplayIsDeterministic(t *testing.T) {
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

// TestReplayShowsTheNamedBehaviours pins what the example exists to demonstrate:
// a refused second writer, a reopened history, an unsettled call, and the two
// recovery verdicts.
func TestReplayShowsTheNamedBehaviours(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"appended=4",
		"second writer refused=true",
		"events=4 version=1 writerHeld=false",
		"openTurn=true unsettled=[call-1] needsAttention=true",
		`history[0] role=user content="read a.txt"`,
		"torn tail: complete=4 repairable=true",
		"middle damage: complete=1 repairable=false",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

// TestReplayWritesOnlyUnderItsOwnTempDir is a safety property worth asserting:
// the demonstration must never touch a caller's session data.
func TestReplayWritesOnlyUnderItsOwnTempDir(t *testing.T) {
	before, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	// The example removes its own directory, so the temporary directory must not
	// accumulate session-replay-* entries.
	for _, entry := range after {
		if strings.HasPrefix(entry.Name(), "session-replay-") {
			t.Fatalf("the example left %s behind", entry.Name())
		}
	}
	_ = before
}
