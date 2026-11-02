package fs_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/fs"
)

// The access mode decides whether a write is even attempted, so its two answers
// must not drift apart.
func TestModeAnswers(t *testing.T) {
	if !fs.ModeReadOnly.Valid() || !fs.ModeWorkspaceWrite.Valid() {
		t.Fatal("the defined modes must validate")
	}
	if fs.Mode("invented").Valid() {
		t.Fatal("an unknown mode must not validate")
	}
	if fs.ModeReadOnly.CanWrite() {
		t.Fatal("read-only must not permit a write")
	}
	if !fs.ModeWorkspaceWrite.CanWrite() {
		t.Fatal("workspace-write must permit a write")
	}
}

// A write request is checked before anything touches the disk.
func TestWriteRequestValidation(t *testing.T) {
	if err := (fs.WriteRequest{Path: "a.txt"}).Validate(); err != nil {
		t.Fatalf("a plain write must validate: %v", err)
	}
	at := time.UnixMilli(1_700_000_000_000)
	conditional := fs.WriteRequest{Path: "a.txt", ExpectedModified: &at}
	if err := conditional.Validate(); err != nil {
		t.Fatalf("a conditional write must validate: %v", err)
	}
	if err := (fs.WriteRequest{}).Validate(); !errors.Is(err, fs.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a path, got %v", err)
	}
	if err := (fs.WriteRequest{Path: "   "}).Validate(); !errors.Is(err, fs.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank path, got %v", err)
	}
	// A create that also expects a modification time is contradictory: the file
	// must not exist, so it cannot have a recorded time to match.
	contradictory := fs.WriteRequest{Path: "a.txt", Create: true, ExpectedModified: &at}
	if err := contradictory.Validate(); !errors.Is(err, fs.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for create plus expectation, got %v", err)
	}
}

// A read is bounded so one large file cannot exhaust memory.
func TestReadSizeBound(t *testing.T) {
	if err := fs.CheckReadSize(1024); err != nil {
		t.Fatalf("a small read must be allowed: %v", err)
	}
	if err := fs.CheckReadSize(fs.MaxReadBytes); err != nil {
		t.Fatalf("a read exactly at the limit must be allowed: %v", err)
	}
	if err := fs.CheckReadSize(fs.MaxReadBytes + 1); !errors.Is(err, fs.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge above the limit, got %v", err)
	}
	if fs.MaxReadBytes <= 0 {
		t.Fatal("the limit must be positive")
	}
}

// The conflict, missing and size errors are distinct conditions, because a caller
// responds to each differently.
func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{fs.ErrInvalidValue, fs.ErrNotFound, fs.ErrConflict, fs.ErrTooLarge}
	for i, first := range sentinels {
		for j, second := range sentinels {
			if i != j && errors.Is(first, second) {
				t.Fatalf("%v and %v must be distinct", first, second)
			}
		}
	}
}
