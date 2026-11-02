package attachment

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gosomea/deepseek-harness-go/capability/attachment"
)

// Errors this package returns.
var (
	// ErrUnknownAttachment reports an identity the store does not hold.
	ErrUnknownAttachment = errors.New("attachment: unknown attachment")
	// ErrWorldMismatch reports a mapping built for another execution world.
	ErrWorldMismatch = errors.New("attachment: mapping belongs to another execution world")
	// ErrStoreIncapable reports a modality the store cannot carry.
	ErrStoreIncapable = errors.New("attachment: store cannot carry this modality")
	// ErrUnsupportedModality reports a modality this mapping will not expose.
	ErrUnsupportedModality = errors.New("attachment: modality is not exposed")
)

// World names the execution world a mapping was built in.
//
// It is recorded because an attachment resolved in one world (a local machine)
// and read in another (a remote host) is a different object even when the
// identity matches, and treating the identity as sufficient would let a request
// read content it cannot actually reach.
type World string

// Mapping is the set of attachments one request may reach.
//
// It holds identities, not content: the content stays in the store, so a mapping
// cannot leak content by being copied around.
type Mapping struct {
	world    World
	store    attachment.Store
	exposed  map[string]attachment.Modality
	order    []string
	modality map[attachment.Modality]bool
}

// NewMapping returns an empty mapping for a world, over a store.
//
// The exposed modalities are named explicitly. A mapping that exposed everything
// the store happened to carry would make a store upgrade silently widen what a
// request can read.
func NewMapping(world World, store attachment.Store, exposed ...attachment.Modality) *Mapping {
	supported := make(map[attachment.Modality]bool)
	for _, modality := range exposed {
		supported[modality] = true
	}
	return &Mapping{
		world:    world,
		store:    store,
		exposed:  make(map[string]attachment.Modality),
		modality: supported,
	}
}

// World returns the mapping's execution world.
func (m *Mapping) World() World {
	if m == nil {
		return ""
	}
	return m.world
}

// Expose adds an attachment identity to what this request may reach.
//
// It checks the store can actually carry the modality before exposing it, so an
// identity is never exposed on the strength of its name alone. A repeated expose
// of the same identity is a no-op.
func (m *Mapping) Expose(id string) error {
	if m == nil || m.store == nil {
		return fmt.Errorf("%w: no store", ErrStoreIncapable)
	}
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: an id is required", ErrUnknownAttachment)
	}
	descriptor, err := m.store.Describe(id)
	if err != nil {
		return fmt.Errorf("%w: %q: %v", ErrUnknownAttachment, id, err)
	}
	if !m.modality[descriptor.Modality] {
		return fmt.Errorf("%w: %q carries %q", ErrUnsupportedModality, id, descriptor.Modality)
	}
	if !m.store.Supported(descriptor.Modality) {
		return fmt.Errorf("%w: %q", ErrStoreIncapable, descriptor.Modality)
	}
	if _, exists := m.exposed[id]; exists {
		return nil
	}
	m.exposed[id] = descriptor.Modality
	m.order = append(m.order, id)
	return nil
}

// Resolve reads an exposed attachment in this mapping's world.
//
// It refuses an identity that was never exposed: reachable-by-storage is not the
// same as reachable-by-this-request. Content is re-verified by the store, so
// corrupted content surfaces as a diagnostic rather than as content.
func (m *Mapping) Resolve(id string, world World) ([]byte, attachment.Descriptor, error) {
	if m == nil || m.store == nil {
		return nil, attachment.Descriptor{}, fmt.Errorf("%w: no store", ErrStoreIncapable)
	}
	if world != m.world {
		return nil, attachment.Descriptor{}, fmt.Errorf("%w: %q != %q", ErrWorldMismatch, world, m.world)
	}
	if _, ok := m.exposed[id]; !ok {
		return nil, attachment.Descriptor{}, fmt.Errorf("%w: %q is not exposed to this request", ErrUnknownAttachment, id)
	}
	content, descriptor, err := m.store.Get(id)
	if err != nil {
		return nil, attachment.Descriptor{}, fmt.Errorf("%w: %q: %v", ErrUnknownAttachment, id, err)
	}
	return content, descriptor, nil
}

// Descriptors returns the exposed attachments' descriptors, sorted by identity.
//
// It returns descriptors rather than content because a request usually needs to
// describe what it can reach without paying for the bytes.
func (m *Mapping) Descriptors() []attachment.Descriptor {
	if m == nil {
		return nil
	}
	out := make([]attachment.Descriptor, 0, len(m.order))
	for _, id := range m.order {
		descriptor, err := m.store.Describe(id)
		if err != nil {
			continue
		}
		out = append(out, descriptor)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// IDs returns the exposed identities in expose order.
func (m *Mapping) IDs() []string {
	if m == nil {
		return nil
	}
	out := make([]string, len(m.order))
	copy(out, m.order)
	return out
}

// Diagnostic explains why one identity could not be reached.
//
// It exists so a caller can report a specific reason instead of a silent absence:
// "the attachment is corrupted" and "the attachment is not exposed here" lead to
// different fixes.
type Diagnostic struct {
	// ID is the attachment identity.
	ID string
	// Reason is the human-readable explanation.
	Reason string
	// Err is the underlying cause.
	Err error
}

// Diagnose reports why each named identity cannot be resolved in world.
//
// An identity that resolves is omitted, so an empty result means every named
// attachment is reachable.
func (m *Mapping) Diagnose(ids []string, world World) []Diagnostic {
	var diagnostics []Diagnostic
	for _, id := range ids {
		if _, _, err := m.Resolve(id, world); err != nil {
			diagnostics = append(diagnostics, Diagnostic{ID: id, Reason: err.Error(), Err: err})
		}
	}
	sort.SliceStable(diagnostics, func(i, j int) bool { return diagnostics[i].ID < diagnostics[j].ID })
	return diagnostics
}
