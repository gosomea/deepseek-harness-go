package spill

import (
	"errors"
	"strings"
	"testing"
)

func TestSpillAndResolveRoundTrip(t *testing.T) {
	store := NewMemoryStore()
	reference, err := store.Spill("r1", "tool-output", []byte("full content"))
	if err != nil {
		t.Fatalf("Spill: %v", err)
	}
	if reference.Size != len("full content") || reference.Origin != "tool-output" {
		t.Fatalf("reference = %+v, want the recorded size and origin", reference)
	}
	record, err := store.Resolve("r1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(record.Content) != "full content" {
		t.Fatalf("content = %q, want the original", record.Content)
	}
}

func TestReferenceIsRefusedWhenAlreadyStored(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Spill("r1", "o", []byte("first")); err != nil {
		t.Fatalf("first Spill: %v", err)
	}
	// A citation already in a request must keep pointing at what it described.
	if _, err := store.Spill("r1", "o", []byte("second")); !errors.Is(err, ErrInvalidSpill) {
		t.Fatalf("second Spill error = %v, want ErrInvalidSpill", err)
	}
	record, err := store.Resolve("r1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(record.Content) != "first" {
		t.Fatalf("content = %q, want the first value", record.Content)
	}
}

func TestSpillRefusesEmptyIDAndContent(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Spill("  ", "o", []byte("x")); !errors.Is(err, ErrInvalidSpill) {
		t.Fatalf("empty-id Spill error = %v, want ErrInvalidSpill", err)
	}
	// An empty spill has nothing to retrieve, so "the original is available"
	// would be false.
	if _, err := store.Spill("r1", "o", nil); !errors.Is(err, ErrInvalidSpill) {
		t.Fatalf("empty-content Spill error = %v, want ErrInvalidSpill", err)
	}
}

func TestResolveRefusesAnUnknownReference(t *testing.T) {
	if _, err := NewMemoryStore().Resolve("ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve error = %v, want ErrNotFound", err)
	}
}

func TestStoreCopiesContentInAndOut(t *testing.T) {
	store := NewMemoryStore()
	input := []byte("hello")
	if _, err := store.Spill("r1", "o", input); err != nil {
		t.Fatalf("Spill: %v", err)
	}
	input[0] = 'X'
	record, err := store.Resolve("r1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(record.Content) != "hello" {
		t.Fatalf("content = %q, want the stored value", record.Content)
	}
	record.Content[0] = 'Y'
	again, err := store.Resolve("r1")
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if string(again.Content) != "hello" {
		t.Fatalf("content = %q, want the store to be unaffected", again.Content)
	}
}

func TestHasLenDiscardAndReferences(t *testing.T) {
	store := NewMemoryStore()
	for _, id := range []string{"a", "b"} {
		if _, err := store.Spill(id, "o", []byte(id)); err != nil {
			t.Fatalf("Spill(%s): %v", id, err)
		}
	}
	if !store.Has("a") || store.Len() != 2 {
		t.Fatalf("Has/Len = %v/%d, want true/2", store.Has("a"), store.Len())
	}
	references := store.References()
	if len(references) != 2 || references[0].ID != "a" {
		t.Fatalf("References = %+v, want insertion order", references)
	}
	if !store.Discard("a") {
		t.Fatal("Discard(a) reported nothing removed")
	}
	if store.Has("a") || store.Len() != 1 {
		t.Fatalf("after discard Has/Len = %v/%d, want false/1", store.Has("a"), store.Len())
	}
	// Discarding twice distinguishes "already gone" from "just removed".
	if store.Discard("a") {
		t.Fatal("second Discard reported a removal")
	}
	if ids := store.References(); len(ids) != 1 || ids[0].ID != "b" {
		t.Fatalf("References = %+v, want only b", ids)
	}
}

func TestNilStoreIsRefused(t *testing.T) {
	var store *MemoryStore
	if _, err := store.Spill("r1", "o", []byte("x")); !errors.Is(err, ErrMissingCapability) {
		t.Fatalf("nil Spill error = %v, want ErrMissingCapability", err)
	}
	if _, err := store.Resolve("r1"); !errors.Is(err, ErrMissingCapability) {
		t.Fatalf("nil Resolve error = %v, want ErrMissingCapability", err)
	}
	if store.Has("r1") || store.Len() != 0 || store.References() != nil {
		t.Fatal("nil store reported state")
	}
	if store.Discard("r1") {
		t.Fatal("nil Discard reported a removal")
	}
}

func TestStoredContentIsRetrievableVerbatim(t *testing.T) {
	store := NewMemoryStore()
	content := []byte(strings.Repeat("line\n", 200))
	if _, err := store.Spill("big", "tool-output", content); err != nil {
		t.Fatalf("Spill: %v", err)
	}
	record, err := store.Resolve("big")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// The whole point of spill: the original comes back byte for byte.
	if string(record.Content) != string(content) {
		t.Fatal("resolved content differs from what was spilled")
	}
}
