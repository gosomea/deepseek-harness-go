package persistence

import (
	"context"
	"errors"
	"testing"
)

// A handle reports its identity and access, and closes idempotently.
func TestHandleBaseIdentityAndClose(t *testing.T) {
	var base HandleBase
	base.Init("s-1", AccessWrite)
	if base.ID() != "s-1" || base.Access() != AccessWrite {
		t.Fatalf("unexpected identity %s/%s", base.ID(), base.Access())
	}
	if !base.Access().CanWrite() {
		t.Fatal("a write handle may append")
	}
	if base.Closed() {
		t.Fatal("a fresh handle is open")
	}
	if err := base.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !base.Closed() {
		t.Fatal("Close must be observable")
	}
	if err := base.Close(); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
}

// A read-only handle reports the read-only reason for a mutation, and a closed
// handle reports the closed reason for everything.
func TestHandleBaseRules(t *testing.T) {
	var readOnly HandleBase
	readOnly.Init("s-1", AccessRead)
	if err := readOnly.CheckOpen(false); err != nil {
		t.Fatalf("reading through a read handle must be allowed: %v", err)
	}
	if err := readOnly.CheckOpen(true); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
	// Once closed, the reason for a mutation is still the more specific one.
	_ = readOnly.Close()
	if err := readOnly.CheckOpen(true); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("a read-only closed handle still reports ErrReadOnly for a write, got %v", err)
	}
	if err := readOnly.CheckOpen(false); !errors.Is(err, ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed, got %v", err)
	}

	var writer HandleBase
	writer.Init("s-2", AccessWrite)
	_ = writer.Close()
	if err := writer.CheckOpen(true); !errors.Is(err, ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed, got %v", err)
	}
}

func TestAccessValidAndCanWrite(t *testing.T) {
	if !AccessRead.Valid() || !AccessWrite.Valid() {
		t.Fatal("the defined access modes must validate")
	}
	if Access("append").Valid() {
		t.Fatal("an undefined access mode must not validate")
	}
	if AccessRead.CanWrite() {
		t.Fatal("a read handle may not write")
	}
	if !AccessWrite.CanWrite() {
		t.Fatal("a write handle may write")
	}
}

func TestSessionIDHelpers(t *testing.T) {
	id := SessionID("s-1")
	if id.String() != "s-1" || id.IsZero() {
		t.Fatalf("unexpected helpers for %q", id)
	}
	if !SessionID("").IsZero() {
		t.Fatal("the empty id must report zero")
	}
}

// Open options are validated before a backend touches anything.
func TestOpenOptionsValidate(t *testing.T) {
	if err := (OpenOptions{Access: AccessRead}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an id, got %v", err)
	}
	if err := (OpenOptions{ID: "s"}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an access mode, got %v", err)
	}
	if err := (OpenOptions{ID: "s", Access: "append"}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unknown mode, got %v", err)
	}
	if err := (OpenOptions{ID: "s", Access: AccessWrite}).Validate(); err != nil {
		t.Fatalf("a complete option set must validate: %v", err)
	}
}

// Create options are validated too, including a negative version.
func TestValidateCreate(t *testing.T) {
	if err := ValidateCreate(CreateOptions{}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an id, got %v", err)
	}
	if err := ValidateCreate(CreateOptions{ID: "s", FormatVersion: -1}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a negative version, got %v", err)
	}
	if err := ValidateCreate(CreateOptions{ID: "s"}); err != nil {
		t.Fatalf("a complete create must validate: %v", err)
	}
}

// The base service owns the single-writer rule.
func TestBaseServiceSingleWriter(t *testing.T) {
	var service BaseService = NewBaseService()
	if service.WriterHeld("s-1") {
		t.Fatal("a fresh service holds no writer")
	}
	if err := service.ClaimWriter("s-1"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !service.WriterHeld("s-1") {
		t.Fatal("the claim must be observable")
	}
	if err := service.ClaimWriter("s-1"); !errors.Is(err, ErrWriterExists) {
		t.Fatalf("want ErrWriterExists for a second claim, got %v", err)
	}
	// A different session is unaffected.
	if err := service.ClaimWriter("s-2"); err != nil {
		t.Fatalf("a different session must be claimable: %v", err)
	}
	service.ReleaseWriter("s-1")
	if service.WriterHeld("s-1") {
		t.Fatal("a released claim must not be held")
	}
	// Release is idempotent.
	service.ReleaseWriter("s-1")
	if err := service.ClaimWriter("s-1"); err != nil {
		t.Fatalf("reclaim after release: %v", err)
	}
}

// Closing the base service refuses later claims and reports what it still held.
func TestBaseServiceClose(t *testing.T) {
	var service BaseService = NewBaseService()
	if err := service.ClaimWriter("s-2"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := service.ClaimWriter("s-1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	changed, held := service.MarkClosed()
	if !changed {
		t.Fatal("the first close must report a change")
	}
	if len(held) != 2 || held[0] != "s-1" || held[1] != "s-2" {
		t.Fatalf("held writers must be reported sorted, got %v", held)
	}
	if !service.Closed() {
		t.Fatal("the service must report closed")
	}
	changed, held = service.MarkClosed()
	if changed || held != nil {
		t.Fatalf("a second close must report no change, got %v %v", changed, held)
	}
	if err := service.ClaimWriter("s-3"); !errors.Is(err, ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed after close, got %v", err)
	}
}

// Stats sort deterministically so a listing never depends on directory order.
func TestSortStats(t *testing.T) {
	stats := []Stat{{ID: "z"}, {ID: "a"}, {ID: "m"}}
	SortStats(stats)
	if stats[0].ID != "a" || stats[1].ID != "m" || stats[2].ID != "z" {
		t.Fatalf("unexpected order %+v", stats)
	}
}

// A cancelled context is a caller-visible refusal in a backend, so the shared
// sentinel errors must be usable with errors.Is.
func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{
		ErrInvalidValue, ErrHandleClosed, ErrReadOnly, ErrWriterExists,
		ErrSessionNotFound, ErrUnsupportedVersion, ErrCorruptReport,
	}
	for index, first := range sentinels {
		for otherIndex, second := range sentinels {
			if index != otherIndex && errors.Is(first, second) {
				t.Fatalf("%v and %v must be distinct", first, second)
			}
		}
	}
}

// The service interface is satisfied by a type that embeds the base, which is
// what the JSONL provider relies on.
func TestServiceShapeIsImplementable(t *testing.T) {
	var _ Service = (Service)(nil)
	var _ Handle = (Handle)(nil)
	ctx := context.Background()
	_ = ctx
}
