package testkit_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// provenance records how the reference traces were captured. It must stay
// accurate: a trace whose hash does not match is either hand-edited or stale,
// and both would make the parity suite prove less than it claims.
type provenance struct {
	SourceCommit       string            `json:"source_commit"`
	ReferenceEntry     string            `json:"reference_entry"`
	ReferenceEntrySHA  string            `json:"reference_entry_sha256"`
	ReferenceTreeClean bool              `json:"reference_tree_clean"`
	Traces             map[string]string `json:"traces"`
	LiveVerification   string            `json:"live_verification"`
	DeterminismCheck   string            `json:"determinism_check"`
	SourceRepository   string            `json:"source_repository"`
	ReferenceVersion   string            `json:"reference_version"`
	RecordedOn         string            `json:"recorded_on"`
}

func TestReferenceTracesMatchProvenance(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "testdata", "parity", "cordis")
	body, err := os.ReadFile(filepath.Join(dir, "provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded provenance
	if err := json.Unmarshal(body, &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded.SourceCommit != "00102833dfaee1da9f48a3a8eae9d34005a75218" {
		t.Fatalf("unexpected reference commit %q", recorded.SourceCommit)
	}
	if recorded.ReferenceEntry == "" || recorded.ReferenceEntrySHA == "" {
		t.Fatal("provenance must name the reference entry and its hash")
	}
	if recorded.ReferenceTreeClean != true {
		t.Error("traces must be recorded from a clean reference tree")
	}
	if recorded.LiveVerification == "" || recorded.DeterminismCheck == "" {
		t.Error("provenance must state how determinism and live agreement were checked")
	}

	// Every recorded trace must exist, and no trace may exist without a
	// recorded hash: an unlisted file would be reference evidence with no
	// provenance.
	expected := filepath.Join(dir, "expected")
	entries, err := os.ReadDir(expected)
	if err != nil {
		t.Fatal(err)
	}
	onDisk := map[string]bool{}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".txt" {
			continue
		}
		onDisk[entry.Name()] = true
		sum, err := os.ReadFile(filepath.Join(expected, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		want, ok := recorded.Traces[entry.Name()]
		if !ok {
			t.Errorf("%s has no provenance hash", entry.Name())
			continue
		}
		got := sha256.Sum256(sum)
		if hex.EncodeToString(got[:]) != want {
			t.Errorf("%s hash mismatch: trace was edited or the recording changed without updating provenance", entry.Name())
		}
	}
	for name := range recorded.Traces {
		if !onDisk[name] {
			t.Errorf("provenance lists %s but the file is missing", name)
		}
	}
}
