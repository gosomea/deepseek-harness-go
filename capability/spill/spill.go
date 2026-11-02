// Package spill defines the spill seam: content moved out of a request, kept
// somewhere addressable, and replaced by a citation that can find it again.
//
// The package exists because shortening content and losing content look the same
// from inside a request. A truncated tool output that only says "…" leaves a
// reader unable to tell whether the rest was discarded or is retrievable. Spill
// therefore always pairs a shortened form with a stable reference to the original,
// and the reference survives as long as the record does.
//
// The seam is deliberately small: it stores spilled content, hands back a
// reference, and reports what it cannot do. Eviction policy and real storage
// belong to a provider.
package spill

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Errors this package returns.
var (
	// ErrInvalidSpill reports a spill that cannot be stored.
	ErrInvalidSpill = errors.New("spill: invalid spill")
	// ErrNotFound reports a reference the store does not hold.
	ErrNotFound = errors.New("spill: reference not found")
	// ErrMissingCapability reports a store that cannot serve a request at all.
	ErrMissingCapability = errors.New("spill: store lacks the capability")
)

// Reference identifies spilled content.
//
// It is a value a request can carry and a reader can resolve, and it is never the
// content itself: a citation that carried the content would defeat the point of
// spilling.
type Reference struct {
	// ID is the reference's stable identity. Required.
	ID string
	// Origin names where the content came from, for diagnostics.
	Origin string
	// Size is the original content length in bytes.
	Size int
}

// Record is a stored spill plus the reference that finds it.
type Record struct {
	// Reference finds the stored content.
	Reference Reference
	// Content is the full original content.
	Content []byte
}

// Store is the spill seam.
//
// It is a small interface rather than a concrete type so a provider can be
// swapped without the consumers changing.
type Store interface {
	// Spill stores content and returns the reference that finds it.
	Spill(id, origin string, content []byte) (Reference, error)
	// Resolve returns the content a reference points at.
	Resolve(id string) (Record, error)
	// Has reports whether a reference is still stored.
	Has(id string) bool
}

// MemoryStore is the in-memory reference implementation.
//
// It is safe for concurrent use, which matters because one request may spill
// content while another resolves a reference.
type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]Record
	order   []string
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: make(map[string]Record)}
}

// Spill stores content under a reference identity.
//
// It refuses a repeated identity rather than replacing: a citation that already
// appears in a request must keep pointing at the content that request described.
// It also refuses empty content, because an empty spill has nothing to retrieve
// and would make "the original is available" false.
func (s *MemoryStore) Spill(id, origin string, content []byte) (Reference, error) {
	if s == nil {
		return Reference{}, fmt.Errorf("%w: nil store", ErrMissingCapability)
	}
	if strings.TrimSpace(id) == "" {
		return Reference{}, fmt.Errorf("%w: a reference id is required", ErrInvalidSpill)
	}
	if len(content) == 0 {
		return Reference{}, fmt.Errorf("%w: %q has empty content", ErrInvalidSpill, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[id]; exists {
		return Reference{}, fmt.Errorf("%w: %q is already stored", ErrInvalidSpill, id)
	}
	stored := make([]byte, len(content))
	copy(stored, content)
	reference := Reference{ID: id, Origin: origin, Size: len(stored)}
	s.records[id] = Record{Reference: reference, Content: stored}
	s.order = append(s.order, id)
	return reference, nil
}

// Resolve returns the content a reference points at.
//
// It copies on the way out, so a caller cannot mutate what the store holds. A
// missing reference is reported as an error rather than as empty content, because
// "the original was never kept" and "the original is empty" need different fixes.
func (s *MemoryStore) Resolve(id string) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("%w: nil store", ErrMissingCapability)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[id]
	if !ok {
		return Record{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	copied := make([]byte, len(record.Content))
	copy(copied, record.Content)
	return Record{Reference: record.Reference, Content: copied}, nil
}

// Has reports whether a reference is still stored.
func (s *MemoryStore) Has(id string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.records[id]
	return ok
}

// Len reports how many spills the store holds.
func (s *MemoryStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// Discard removes a reference.
//
// It exists so a provider can express the end of a spill's life explicitly. It
// reports whether anything was removed, so a caller can tell "already gone" from
// "just removed" without a second lookup.
func (s *MemoryStore) Discard(id string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return false
	}
	delete(s.records, id)
	remaining := s.order[:0]
	for _, candidate := range s.order {
		if candidate != id {
			remaining = append(remaining, candidate)
		}
	}
	s.order = remaining
	return true
}

// References returns the stored references in insertion order.
func (s *MemoryStore) References() []Reference {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Reference, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.records[id].Reference)
	}
	return out
}
