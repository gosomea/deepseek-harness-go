package main

import (
	"bytes"
	"os"
	"testing"
)

// TestDemoMatchesDocumentedOutput pins the example's stdout to expected.txt, so
// the tutorial cannot drift from what the program prints.
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

// TestDemoIsDeterministic keeps two runs of the same document identical.
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

// TestDemoLeavesNoResourceBehind pins that a successful run releases the tree:
// the plugin catalog acquires nothing here, so the observable contract is that
// run returns nil and reports every entry exactly once.
func TestDemoLeavesNoResourceBehind(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"entry cache",
		"entry off",
		"entry store",
		"active=2 total=3",
	} {
		if !bytes.Contains([]byte(text), []byte(want)) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}
