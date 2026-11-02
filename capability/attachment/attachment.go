// Package attachment defines the attachment seam: immutable content with a
// stable identity, addressed by identity rather than by path.
//
// The package exists because a path is not an identity. A file at a path can be
// replaced, moved or deleted, and a request that cited the path would then
// silently describe something else. Content that must stay the same across a
// session is therefore stored once under an identity, and a request cites that
// identity.
//
// The seam is deliberately small: it stores bytes and reports what it cannot do.
// Real storage, eviction and platform directories belong to a provider.
package attachment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Digest returns the content digest this package records.
//
// It is exported so a provider and a test compute the same value rather than
// each inventing a comparison. A digest is used instead of raw bytes in a
// descriptor because describing content should not cost what carrying it costs.
func Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Errors this package returns.
var (
	// ErrInvalidAttachment reports an attachment that cannot be stored.
	ErrInvalidAttachment = errors.New("attachment: invalid attachment")
	// ErrNotFound reports an identity the store does not hold.
	ErrNotFound = errors.New("attachment: not found")
	// ErrCorrupt reports stored content that does not match its recorded digest.
	ErrCorrupt = errors.New("attachment: content digest does not match")
	// ErrUnsupportedModality reports a modality this store cannot carry.
	ErrUnsupportedModality = errors.New("attachment: unsupported modality")
	// ErrMissingCapability reports a store that cannot serve a request at all.
	ErrMissingCapability = errors.New("attachment: store lacks the capability")
)

// Modality is the kind of content an attachment carries.
type Modality string

// The modalities this seam names. Whether a given store carries one is a
// per-store capability question, not a property of the name.
const (
	// ModalityText is plain text content.
	ModalityText Modality = "text"
	// ModalityImage is image content.
	ModalityImage Modality = "image"
	// ModalityBinary is opaque bytes.
	ModalityBinary Modality = "binary"
)

// Descriptor describes content without carrying it.
//
// Size and Digest are recorded at store time so a later read can prove the
// content is the same content, rather than trusting a path that may now point
// elsewhere.
type Descriptor struct {
	// ID is the attachment's stable identity. Required and unique.
	ID string
	// Modality is the content kind.
	Modality Modality
	// Size is the content length in bytes.
	Size int
	// Digest is the content digest recorded at store time.
	Digest string
	// Name is a human-readable label. Optional.
	Name string
}

// Store is the attachment seam.
//
// It is a small interface rather than a concrete type so a provider can be
// swapped without the consumers changing, and so a test can supply a store that
// fails a specific way.
type Store interface {
	// Put stores content under an identity and returns its descriptor.
	Put(id string, modality Modality, content []byte) (Descriptor, error)
	// Get returns the content and descriptor for an identity.
	Get(id string) ([]byte, Descriptor, error)
	// Describe returns a descriptor without the content.
	Describe(id string) (Descriptor, error)
	// Supported reports whether the store carries a modality.
	Supported(modality Modality) bool
}

// MemoryStore is the in-memory reference implementation.
//
// It is safe for concurrent use, which matters because an attachment may be
// written by one goroutine and read by another during the same turn.
type MemoryStore struct {
	mu       sync.RWMutex
	contents map[string][]byte
	records  map[string]Descriptor
	modality map[Modality]bool
}

// NewMemoryStore returns a store carrying the given modalities.
//
// An empty modality list means "no modality is supported", which is the honest
// reading: a store that named nothing did not claim to carry anything. A caller
// that wants everything lists everything explicitly.
func NewMemoryStore(modalities ...Modality) *MemoryStore {
	supported := make(map[Modality]bool)
	for _, modality := range modalities {
		supported[modality] = true
	}
	return &MemoryStore{
		contents: make(map[string][]byte),
		records:  make(map[string]Descriptor),
		modality: supported,
	}
}

// Supported reports whether the store carries a modality.
func (s *MemoryStore) Supported(modality Modality) bool {
	if s == nil {
		return false
	}
	return s.modality[modality]
}

// Put stores content under an identity.
//
// It refuses a repeated identity rather than replacing: content that a request
// already cited must not change underneath it. It also refuses an unsupported
// modality rather than accepting content it cannot serve, and copies the content
// so a caller cannot mutate what the store holds.
func (s *MemoryStore) Put(id string, modality Modality, content []byte) (Descriptor, error) {
	if s == nil {
		return Descriptor{}, fmt.Errorf("%w: nil store", ErrMissingCapability)
	}
	if strings.TrimSpace(id) == "" {
		return Descriptor{}, fmt.Errorf("%w: an id is required", ErrInvalidAttachment)
	}
	if len(content) == 0 {
		return Descriptor{}, fmt.Errorf("%w: %q has empty content", ErrInvalidAttachment, id)
	}
	if !s.Supported(modality) {
		return Descriptor{}, fmt.Errorf("%w: %q", ErrUnsupportedModality, modality)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[id]; exists {
		return Descriptor{}, fmt.Errorf("%w: %q is already stored", ErrInvalidAttachment, id)
	}
	stored := make([]byte, len(content))
	copy(stored, content)
	record := Descriptor{
		ID:       id,
		Modality: modality,
		Size:     len(stored),
		Digest:   Digest(stored),
	}
	s.contents[id] = stored
	s.records[id] = record
	return record, nil
}

// Get returns content and descriptor for an identity.
//
// The digest is re-checked on read, so content that changed underneath the store
// (a provider backed by a file, say) is reported as corrupt rather than served as
// if it were the recorded content.
func (s *MemoryStore) Get(id string) ([]byte, Descriptor, error) {
	if s == nil {
		return nil, Descriptor{}, fmt.Errorf("%w: nil store", ErrMissingCapability)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[id]
	if !ok {
		return nil, Descriptor{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	content, ok := s.contents[id]
	if !ok {
		return nil, Descriptor{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if Digest(content) != record.Digest {
		return nil, Descriptor{}, fmt.Errorf("%w: %q", ErrCorrupt, id)
	}
	out := make([]byte, len(content))
	copy(out, content)
	return out, record, nil
}

// Describe returns a descriptor without the content.
func (s *MemoryStore) Describe(id string) (Descriptor, error) {
	if s == nil {
		return Descriptor{}, fmt.Errorf("%w: nil store", ErrMissingCapability)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[id]
	if !ok {
		return Descriptor{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	return record, nil
}

// Len reports how many attachments the store holds.
func (s *MemoryStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}
