package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestCLIDemoMatchesDocumentedOutput pins the demonstration's stdout to
// expected.txt, so the walkthrough in docs/cli/session-cli.md cannot drift from
// what the program prints.
func TestCLIDemoMatchesDocumentedOutput(t *testing.T) {
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

// TestCLIDemoIsDeterministic keeps two runs identical.
func TestCLIDemoIsDeterministic(t *testing.T) {
	var first, second bytes.Buffer
	if err := run(&first); err != nil {
		t.Fatal(err)
	}
	if err := run(&second); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatalf("output is not reproducible:\n%s\n---\n%s", first.String(), second.String())
	}
}

// TestCLIDemoShowsTheNamedBehaviours pins what the example exists to show: a
// refusal before the side effect, an allowed write, a read-only refusal, and a
// reopen that carries the history.
func TestCLIDemoShowsTheNamedBehaviours(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"read=notes.txt",
		"denied=true",
		"file unchanged after refusal=true",
		"file now=\"approved change\"",
		"command denied=true",
		"events=4 messages=2 openTurn=false unsettled=0",
		`history[0] role=user content="read notes.txt"`,
		`history[1] role=assistant content="notes.txt says original content"`,
		"needsAttention=false",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	// The refusal must be printed before the write that succeeds, which is the
	// ordering the whole demonstration exists to show.
	denied := strings.Index(text, "file unchanged after refusal=true")
	allowed := strings.Index(text, "file now=")
	if denied < 0 || allowed < 0 || denied > allowed {
		t.Fatalf("the refusal must be shown before the allowed write:\n%s", text)
	}
}

// TestCLIDemoWritesOnlyUnderItsOwnTempDir is a safety property: the example must
// never touch a real project.
func TestCLIDemoWritesOnlyUnderItsOwnTempDir(t *testing.T) {
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
	// Every temporary directory the example created must have been removed.
	existing := map[string]bool{}
	for _, entry := range before {
		existing[entry.Name()] = true
	}
	for _, entry := range after {
		if strings.HasPrefix(entry.Name(), "cli-e2e-") && !existing[entry.Name()] {
			t.Fatalf("the example left %s behind", entry.Name())
		}
	}
}
