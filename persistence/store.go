package persistence

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/session"
)

// MemoryStore is an in-process Store.
//
// It exists so a caller can exercise resume, checkpoint and cache behaviour
// without a filesystem, and so a test can inject a storage failure at an exact
// step. It is not a substitute for the JSONL provider: it keeps no bytes, and it
// makes no durability promise.
type MemoryStore struct {
	// mu guards the sessions.
	mu sync.Mutex
	// sessions maps each id to its events.
	sessions map[SessionID][]sessionEvent
	// failOpen, when set, makes every Open return this error.
	failOpen error
	// failStat, when set, makes every Stat return this error.
	failStat error
}

// sessionEvent is one stored event in the memory store.
type sessionEvent struct {
	// Type is the event type.
	Type string
	// Data is the payload.
	Data []byte
	// SurfaceOp is the surface metadata, when the event has any. It is stored
	// because dropping it would turn a message-producing event into a log-only
	// one: the restored log would fold to a shorter history than the one that was
	// written.
	SurfaceOp *session.SurfaceOp
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: map[SessionID][]sessionEvent{}}
}

// FailOpen makes every Open fail with the given error.
//
// It is the seam a test uses to prove that a storage failure surfaces instead of
// being reported as an empty session.
func (s *MemoryStore) FailOpen(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failOpen = err
}

// FailStat makes every Stat fail with the given error.
func (s *MemoryStore) FailStat(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failStat = err
}

// Seed records events for a session without a handle, for tests that need a
// pre-existing history.
func (s *MemoryStore) Seed(id SessionID, events ...session.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := make([]sessionEvent, 0, len(events))
	for _, event := range events {
		stored = append(stored, sessionEvent{
			Type: event.Type, Data: append([]byte(nil), event.Data...), SurfaceOp: cloneSurfaceOp(event.SurfaceOp),
		})
	}
	s.sessions[id] = stored
}

// Open returns a handle over the stored events.
func (s *MemoryStore) Open(ctx context.Context, options OpenOptions) (Handle, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failOpen != nil {
		return nil, s.failOpen
	}
	stored, ok := s.sessions[options.ID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, options.ID)
	}
	handle := &memoryHandle{store: s, events: append([]sessionEvent(nil), stored...)}
	handle.Init(options.ID, options.Access)
	return handle, nil
}

// Stat describes one stored session.
func (s *MemoryStore) Stat(ctx context.Context, id SessionID) (Stat, error) {
	if id.IsZero() {
		return Stat{}, fmt.Errorf("%w: stat requires a session id", ErrInvalidValue)
	}
	if err := ctx.Err(); err != nil {
		return Stat{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failStat != nil {
		return Stat{}, s.failStat
	}
	stored, ok := s.sessions[id]
	if !ok {
		return Stat{}, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	return Stat{ID: id, Events: len(stored), FormatVersion: 1}, nil
}

// memoryHandle is a handle over the in-process store.
type memoryHandle struct {
	// HandleBase carries the shared closed and read-only rules.
	HandleBase
	// store is the owning store.
	store *MemoryStore
	// events is this handle's view.
	events []sessionEvent
}

// Read returns the stored events from an offset.
func (h *memoryHandle) Read(ctx context.Context, from session.Seq) ([]session.Event, error) {
	if err := h.HandleBase.CheckOpen(false); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	events := make([]session.Event, 0, len(h.events))
	for index, stored := range h.events {
		events = append(events, session.Event{
			Type: stored.Type, Seq: session.Seq(index), Data: append([]byte(nil), stored.Data...),
			SurfaceOp: cloneSurfaceOp(stored.SurfaceOp),
		})
	}
	if int(from) >= len(events) {
		return nil, nil
	}
	return events[from:], nil
}

// Append adds one event to the store.
func (h *memoryHandle) Append(ctx context.Context, input session.AppendInput) (session.Event, error) {
	if err := h.HandleBase.CheckOpen(true); err != nil {
		return session.Event{}, err
	}
	if err := ctx.Err(); err != nil {
		return session.Event{}, err
	}
	if input.Type == "" {
		return session.Event{}, fmt.Errorf("%w: event requires a type", ErrInvalidValue)
	}
	if len(input.Data) == 0 {
		return session.Event{}, fmt.Errorf("%w: event requires data", ErrInvalidValue)
	}
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	// The sequence is the store's current length, not this handle's view: two
	// handles may be open on one session, and numbering from a stale view would
	// hand both the same sequence and drop one of the writes.
	current := h.store.sessions[h.ID()]
	event := session.Event{Type: input.Type, Seq: session.Seq(len(current)), Data: append([]byte(nil), input.Data...)}
	if input.SurfaceOp != nil {
		op := *input.SurfaceOp
		event.SurfaceOp = &op
	}
	if err := session.ValidateEventEnvelope(event); err != nil {
		return session.Event{}, err
	}
	// A refused event must not be stored, and the write must be appended to the
	// store's own slice so every other handle sees it.
	h.store.sessions[h.ID()] = append(current, sessionEvent{
		Type: event.Type, Data: append([]byte(nil), event.Data...), SurfaceOp: cloneSurfaceOp(event.SurfaceOp),
	})
	// The handle's view is refreshed from the store so its own reads see every
	// append the store accepted, not only its own.
	h.events = append([]sessionEvent(nil), h.store.sessions[h.ID()]...)
	return event, nil
}

// Flush is a no-op: the memory store has no buffer to drain.
func (h *memoryHandle) Flush(ctx context.Context) error {
	return h.HandleBase.CheckOpen(false)
}

var (
	_ Store  = (*MemoryStore)(nil)
	_ Handle = (*memoryHandle)(nil)
)

// cloneSurfaceOp copies surface metadata so a stored event cannot be mutated
// through a slice a caller still holds.
func cloneSurfaceOp(op *session.SurfaceOp) *session.SurfaceOp {
	if op == nil {
		return nil
	}
	cloned := *op
	return &cloned
}
