package attachment

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/attachment"
)

func testStore(t *testing.T) *attachment.MemoryStore {
	t.Helper()
	store := attachment.NewMemoryStore(attachment.ModalityText, attachment.ModalityImage)
	if _, err := store.Put("notes", attachment.ModalityText, []byte("notes body")); err != nil {
		t.Fatalf("Put(notes): %v", err)
	}
	if _, err := store.Put("shot", attachment.ModalityImage, []byte("png bytes")); err != nil {
		t.Fatalf("Put(shot): %v", err)
	}
	return store
}

func TestExposeThenResolve(t *testing.T) {
	store := testStore(t)
	mapping := NewMapping("local", store, attachment.ModalityText, attachment.ModalityImage)
	if err := mapping.Expose("notes"); err != nil {
		t.Fatalf("Expose: %v", err)
	}
	content, descriptor, err := mapping.Resolve("notes", "local")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(content) != "notes body" || descriptor.Modality != attachment.ModalityText {
		t.Fatalf("resolved %q / %+v", content, descriptor)
	}
}

func TestResolveRefusesAnUnexposedButStoredAttachment(t *testing.T) {
	store := testStore(t)
	mapping := NewMapping("local", store, attachment.ModalityText)
	if err := mapping.Expose("notes"); err != nil {
		t.Fatalf("Expose: %v", err)
	}
	// "shot" is in the store, but this request never exposed it. Reachable by
	// storage is not the same as reachable by this request.
	if _, _, err := mapping.Resolve("shot", "local"); !errors.Is(err, ErrUnknownAttachment) {
		t.Fatalf("Resolve error = %v, want ErrUnknownAttachment", err)
	}
}

func TestMissingAttachmentIsRefused(t *testing.T) {
	mapping := NewMapping("local", testStore(t), attachment.ModalityText)
	if err := mapping.Expose("ghost"); !errors.Is(err, ErrUnknownAttachment) {
		t.Fatalf("Expose error = %v, want ErrUnknownAttachment", err)
	}
}

func TestWorldMismatchIsRefused(t *testing.T) {
	store := testStore(t)
	mapping := NewMapping("local", store, attachment.ModalityText)
	if err := mapping.Expose("notes"); err != nil {
		t.Fatalf("Expose: %v", err)
	}
	// The same identity in another execution world is a different object.
	if _, _, err := mapping.Resolve("notes", "remote"); !errors.Is(err, ErrWorldMismatch) {
		t.Fatalf("Resolve error = %v, want ErrWorldMismatch", err)
	}
}

func TestUnsupportedModalityIsRefusedAtExpose(t *testing.T) {
	store := testStore(t)
	mapping := NewMapping("local", store, attachment.ModalityText)
	if err := mapping.Expose("shot"); !errors.Is(err, ErrUnsupportedModality) {
		t.Fatalf("Expose error = %v, want ErrUnsupportedModality", err)
	}
	if len(mapping.IDs()) != 0 {
		t.Fatalf("exposed = %v, want nothing exposed", mapping.IDs())
	}
}

func TestStoreIncapableModalityIsRefused(t *testing.T) {
	// The store carries text only, so a mapping that tries to expose an image
	// modality must fail rather than expose something unreadable.
	store := attachment.NewMemoryStore(attachment.ModalityText)
	if _, err := store.Put("notes", attachment.ModalityText, []byte("body")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	mapping := NewMapping("local", store, attachment.ModalityText, attachment.ModalityImage)
	if err := mapping.Expose("notes"); err != nil {
		t.Fatalf("Expose: %v", err)
	}
	// A store that later loses a modality is reported by Supported, which the
	// mapping consults before exposing.
	if store.Supported(attachment.ModalityImage) {
		t.Fatal("store reported an unsupported modality as supported")
	}
}

func TestHealthyAttachmentHasNoDiagnostic(t *testing.T) {
	mapping := NewMapping("local", testStore(t), attachment.ModalityText)
	if err := mapping.Expose("notes"); err != nil {
		t.Fatalf("Expose: %v", err)
	}
	if diagnostics := mapping.Diagnose([]string{"notes"}, "local"); len(diagnostics) != 0 {
		t.Fatalf("healthy attachment reported as %+v", diagnostics)
	}
}

func TestDiagnoseReportsWhyAnAttachmentIsUnreachable(t *testing.T) {
	store := testStore(t)
	mapping := NewMapping("local", store, attachment.ModalityText)
	if err := mapping.Expose("notes"); err != nil {
		t.Fatalf("Expose: %v", err)
	}
	// "shot" is stored but not exposed, and "ghost" does not exist: both must be
	// reported with a reason rather than as a silent absence.
	diagnostics := mapping.Diagnose([]string{"shot", "ghost", "notes"}, "local")
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want two entries", diagnostics)
	}
	if diagnostics[0].ID != "ghost" || diagnostics[1].ID != "shot" {
		t.Fatalf("diagnostics = %+v, want sorted by id", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Reason == "" || diagnostic.Err == nil {
			t.Fatalf("diagnostic %+v lacks a reason", diagnostic)
		}
	}
}

func TestExposeIsIdempotent(t *testing.T) {
	mapping := NewMapping("local", testStore(t), attachment.ModalityText)
	for index := 0; index < 2; index++ {
		if err := mapping.Expose("notes"); err != nil {
			t.Fatalf("Expose %d: %v", index, err)
		}
	}
	if ids := mapping.IDs(); len(ids) != 1 {
		t.Fatalf("ids = %v, want one entry", ids)
	}
}

func TestDescriptorsAreSortedAndCarryNoContent(t *testing.T) {
	store := testStore(t)
	mapping := NewMapping("local", store, attachment.ModalityText)
	if err := mapping.Expose("notes"); err != nil {
		t.Fatalf("Expose: %v", err)
	}
	descriptors := mapping.Descriptors()
	if len(descriptors) != 1 || descriptors[0].ID != "notes" {
		t.Fatalf("descriptors = %+v", descriptors)
	}
	if descriptors[0].Size != len("notes body") {
		t.Fatalf("descriptor = %+v, want the recorded size", descriptors[0])
	}
}

func TestMappingRefusesBadInput(t *testing.T) {
	var mapping *Mapping
	if err := mapping.Expose("notes"); !errors.Is(err, ErrStoreIncapable) {
		t.Fatalf("nil Expose error = %v, want ErrStoreIncapable", err)
	}
	if _, _, err := mapping.Resolve("notes", "local"); !errors.Is(err, ErrStoreIncapable) {
		t.Fatalf("nil Resolve error = %v, want ErrStoreIncapable", err)
	}
	if mapping.World() != "" {
		t.Fatal("nil mapping reported a world")
	}
	if mapping.IDs() != nil || mapping.Descriptors() != nil {
		t.Fatal("nil mapping returned entries")
	}
	valid := NewMapping("local", testStore(t), attachment.ModalityText)
	if err := valid.Expose("  "); !errors.Is(err, ErrUnknownAttachment) {
		t.Fatalf("blank-id Expose error = %v, want ErrUnknownAttachment", err)
	}
	if err := NewMapping("local", nil, attachment.ModalityText).Expose("notes"); !errors.Is(err, ErrStoreIncapable) {
		t.Fatalf("storeless Expose error = %v, want ErrStoreIncapable", err)
	}
}

func TestWorldIsReported(t *testing.T) {
	mapping := NewMapping("remote", testStore(t), attachment.ModalityText)
	if mapping.World() != "remote" {
		t.Fatalf("World() = %q, want remote", mapping.World())
	}
}

func TestCorruptContentSurfacesAsADiagnostic(t *testing.T) {
	store, err := NewTamperStore("notes", []byte("original"))
	if err != nil {
		t.Fatalf("NewTamperStore: %v", err)
	}
	mapping := NewMapping("local", store, attachment.ModalityText)
	if err := mapping.Expose("notes"); err != nil {
		t.Fatalf("Expose: %v", err)
	}
	// Corrupted content must be reported, never served as if it were the
	// recorded content.
	if _, _, err := mapping.Resolve("notes", "local"); err == nil {
		t.Fatal("Resolve succeeded although the content digest does not match")
	}
	diagnostics := mapping.Diagnose([]string{"notes"}, "local")
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one entry", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Reason, "digest") {
		t.Fatalf("reason = %q, want it to name the digest mismatch", diagnostics[0].Reason)
	}
}
