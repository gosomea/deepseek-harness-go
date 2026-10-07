package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFormatGateCoversDiscoveredPackages pins both directions of the format gate.
//
// The gate previously listed directories, so a new package was silently outside
// it. These cases fail if the scope ever narrows back to a fixed list: the
// unformatted file sits in a directory the old list never named.
func TestFormatGateCoversDiscoveredPackages(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		body string
		bad  bool
	}{
		{"clean-root", "cordis/a.go", "package cordis\n\nfunc Good() {}\n", false},
		{"unformatted-new-package", "loader/a.go", "package loader\n\nfunc  Bad( ) {}\n", true},
		{"unformatted-second-new-package", "app/a.go", "package app\n\nfunc  Bad( ) {}\n", true},
		{"unformatted-command", "cmd/dsh-go/a.go", "package main\n\nfunc  Bad( ) {}\n", true},
		{"unformatted-internal", "internal/a.go", "package internal\n\nfunc  Bad( ) {}\n", true},
		{"formatted-nested", "internal/deep/a.go", "package deep\n\nfunc Good() {}\n", false},
		{"unparseable", "loader/bad.go", "package loader\n\nfunc {\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, tc.path, tc.body)
			err := checkFormatting(root)
			if (err != nil) != tc.bad {
				t.Fatalf("checkFormatting = %v, bad = %v", err, tc.bad)
			}
		})
	}
}

// TestFormatGateSkipsEvidenceDirectories keeps historical evidence out of scope,
// matching every other documentation check.
func TestFormatGateSkipsEvidenceDirectories(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "cordis/a.go", "package cordis\n\nfunc Good() {}\n")
	writeFixture(t, root, "validation/run/a.go", "package run\n\nfunc  Bad( ) {}\n")
	writeFixture(t, root, "bin/a.go", "package main\n\nfunc  Bad( ) {}\n")
	if err := checkFormatting(root); err != nil {
		t.Fatalf("skipped directories must not fail the gate: %v", err)
	}
}

// TestFormatGateReportsEveryOffender keeps the diagnostic complete and stable, so
// one run is enough to fix every file.
func TestFormatGateReportsEveryOffender(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "loader/b.go", "package loader\n\nfunc  Bad( ) {}\n")
	writeFixture(t, root, "app/a.go", "package app\n\nfunc  Bad( ) {}\n")
	err := checkFormatting(root)
	if err == nil {
		t.Fatal("expected the gate to fail")
	}
	for _, want := range []string{"app/a.go", "loader/b.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("report %q should name %q", err.Error(), want)
		}
	}
	// The report is sorted, so a repeat run lists the same order.
	if strings.Index(err.Error(), "app/a.go") > strings.Index(err.Error(), "loader/b.go") {
		t.Fatalf("report should be sorted: %q", err.Error())
	}
}

// TestFormatGateAcceptsTheRepository keeps the gate honest about the real tree: it
// must pass on the checked-out sources.
func TestFormatGateAcceptsTheRepository(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// The package directory is scripts/doccheck; the repository root is two up.
	repository := filepath.Dir(filepath.Dir(root))
	if err := checkFormatting(repository); err != nil {
		t.Fatalf("the checked-out repository is not gofmt-clean: %v", err)
	}
}
