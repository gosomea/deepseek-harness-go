package attachment

import (
	"errors"
	"testing"
)

func allModalities() *MemoryStore {
	return NewMemoryStore(ModalityText, ModalityImage, ModalityBinary)
}

func TestPutAndGetRoundTrip(t *testing.T) {
	store := allModalities()
	record, err := store.Put("a", ModalityText, []byte("hello"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if record.Size != 5 || record.Digest != Digest([]byte("hello")) {
		t.Fatalf("descriptor = %+v, want size 5 and the content digest", record)
	}
	content, descriptor, err := store.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(content) != "hello" || descriptor.ID != "a" {
		t.Fatalf("Get returned %q / %+v", content, descriptor)
	}
}

func TestDescribeNeedsNoContent(t *testing.T) {
	store := allModalities()
	if _, err := store.Put("a", ModalityText, []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	descriptor, err := store.Describe("a")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if descriptor.Size != 5 {
		t.Fatalf("descriptor = %+v, want size 5", descriptor)
	}
}

func TestIdentityIsRefusedWhenAlreadyStored(t *testing.T) {
	store := allModalities()
	if _, err := store.Put("a", ModalityText, []byte("first")); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	// Content a request already cited must not change underneath it.
	if _, err := store.Put("a", ModalityText, []byte("second")); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("second Put error = %v, want ErrInvalidAttachment", err)
	}
	content, _, err := store.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(content) != "first" {
		t.Fatalf("content = %q, want the first value", content)
	}
}

func TestPutRefusesEmptyIDAndContent(t *testing.T) {
	store := allModalities()
	if _, err := store.Put("  ", ModalityText, []byte("x")); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("empty-id Put error = %v, want ErrInvalidAttachment", err)
	}
	if _, err := store.Put("a", ModalityText, nil); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("empty-content Put error = %v, want ErrInvalidAttachment", err)
	}
}

func TestUnsupportedModalityIsRefused(t *testing.T) {
	store := NewMemoryStore(ModalityText)
	if _, err := store.Put("img", ModalityImage, []byte("x")); !errors.Is(err, ErrUnsupportedModality) {
		t.Fatalf("Put error = %v, want ErrUnsupportedModality", err)
	}
	if store.Supported(ModalityImage) {
		t.Fatal("store reported an unsupported modality as supported")
	}
}

func TestEmptyModalityListSupportsNothing(t *testing.T) {
	// A store that named nothing did not claim to carry anything.
	store := NewMemoryStore()
	for _, modality := range []Modality{ModalityText, ModalityImage, ModalityBinary} {
		if store.Supported(modality) {
			t.Fatalf("empty store claims to support %q", modality)
		}
	}
}

func TestGetRefusesAnUnknownIdentity(t *testing.T) {
	if _, _, err := allModalities().Get("ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get error = %v, want ErrNotFound", err)
	}
}

func TestCorruptContentIsDetected(t *testing.T) {
	store := allModalities()
	if _, err := store.Put("a", ModalityText, []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Simulate content changing underneath the store, as a file-backed provider
	// would when the file is replaced.
	store.mu.Lock()
	store.contents["a"] = []byte("tampered")
	store.mu.Unlock()
	if _, _, err := store.Get("a"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get error = %v, want ErrCorrupt", err)
	}
}

func TestStoreCopiesContentInAndOut(t *testing.T) {
	store := allModalities()
	input := []byte("hello")
	if _, err := store.Put("a", ModalityText, input); err != nil {
		t.Fatalf("Put: %v", err)
	}
	input[0] = 'X'
	content, _, err := store.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("content = %q, want the stored value", content)
	}
	content[0] = 'Y'
	again, _, err := store.Get("a")
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if string(again) != "hello" {
		t.Fatalf("content = %q, want the store to be unaffected by the caller", again)
	}
}

func TestNilStoreIsRefused(t *testing.T) {
	var store *MemoryStore
	if _, err := store.Put("a", ModalityText, []byte("x")); !errors.Is(err, ErrMissingCapability) {
		t.Fatalf("nil Put error = %v, want ErrMissingCapability", err)
	}
	if _, _, err := store.Get("a"); !errors.Is(err, ErrMissingCapability) {
		t.Fatalf("nil Get error = %v, want ErrMissingCapability", err)
	}
	if _, err := store.Describe("a"); !errors.Is(err, ErrMissingCapability) {
		t.Fatalf("nil Describe error = %v, want ErrMissingCapability", err)
	}
	if store.Supported(ModalityText) {
		t.Fatal("nil store claims support")
	}
	if store.Len() != 0 {
		t.Fatalf("nil Len = %d, want 0", store.Len())
	}
}

func TestLenCountsStoredAttachments(t *testing.T) {
	store := allModalities()
	for _, id := range []string{"a", "b"} {
		if _, err := store.Put(id, ModalityText, []byte(id)); err != nil {
			t.Fatalf("Put(%s): %v", id, err)
		}
	}
	if store.Len() != 2 {
		t.Fatalf("Len = %d, want 2", store.Len())
	}
}
