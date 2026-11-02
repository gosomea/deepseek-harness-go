package main

import (
	"os"
	"testing"
)

// TestDemoMatchesDocumentedOutput pins the example to its recorded output.
func TestDemoMatchesDocumentedOutput(t *testing.T) {
	want, err := os.ReadFile("expected.txt")
	if err != nil {
		t.Fatalf("read expected.txt: %v", err)
	}
	file, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer file.Close()
	if err := run(file); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("output differs\nwant:\n%s\ngot:\n%s", want, got)
	}
}
