package attachment

import (
	"errors"

	"github.com/gosomea/deepseek-harness-go/capability/attachment"
)

// TamperStore serves content under a descriptor whose digest does not match it,
// standing in for a file-backed provider whose file changed underneath it.
type TamperStore struct {
	inner      *attachment.MemoryStore
	tamperWith string
}

// NewTamperStore returns a store that reports a digest mismatch for one identity.
func NewTamperStore(id string, content []byte) (*TamperStore, error) {
	inner := attachment.NewMemoryStore(attachment.ModalityText)
	if _, err := inner.Put(id, attachment.ModalityText, content); err != nil {
		return nil, err
	}
	return &TamperStore{inner: inner, tamperWith: id}, nil
}

// Put stores content under an identity.
func (s *TamperStore) Put(id string, modality attachment.Modality, content []byte) (attachment.Descriptor, error) {
	return s.inner.Put(id, modality, content)
}

// Get reports a digest mismatch for the tampered identity.
//
// It returns the store's own corruption error rather than serving content, which
// is what a provider must do when the bytes no longer match the descriptor.
func (s *TamperStore) Get(id string) ([]byte, attachment.Descriptor, error) {
	if id == s.tamperWith {
		return nil, attachment.Descriptor{}, attachment.ErrCorrupt
	}
	return s.inner.Get(id)
}

// Describe returns a descriptor without the content.
func (s *TamperStore) Describe(id string) (attachment.Descriptor, error) {
	return s.inner.Describe(id)
}

// Supported reports whether the store carries a modality.
func (s *TamperStore) Supported(modality attachment.Modality) bool {
	return s.inner.Supported(modality)
}

// ErrTamperWorld is unused; kept so a reader sees the failure set explicitly.
var ErrTamperWorld = errors.New("attachment: tamper store")
