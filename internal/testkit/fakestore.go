package testkit

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// FakeStore is a persistence.Store whose scripted outcomes live in the test.
//
// It exists so a test can drive storage failures at an exact call: a store that
// can only succeed cannot show that a caller handles a broken disk. Every
// injectable failure is a field rather than a build tag, so the failure and the
// expectation sit next to each other in the test that needs them.
type FakeStore struct {
	// mu guards the fields below.
	mu sync.Mutex
	// sessions holds the scripted events per id.
	sessions map[persistence.SessionID][]session.Event
	// openErr, when set, is returned by every Open.
	openErr error
	// statErr, when set, is returned by every Stat.
	statErr error
	// appendErr, when set, is returned by every Append on a handle.
	appendErr error
	// flushErr, when set, is returned by every Flush on a handle.
	flushErr error
	// opened counts the handles this store handed out, so a test can assert a
	// caller closed what it opened.
	opened int
	// closed counts the handles closed through this store.
	closed int
}

// NewFakeStore returns an empty store.
func NewFakeStore() *FakeStore {
	return &FakeStore{sessions: map[persistence.SessionID][]session.Event{}}
}

// Seed scripts a session's history.
//
// The events are copied so a test cannot change a session's content by mutating
// the slice it seeded with.
func (s *FakeStore) Seed(id persistence.SessionID, events ...session.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := make([]session.Event, 0, len(events))
	for index, event := range events {
		cloned := event.Clone()
		// The log position is the index, so seeding does not depend on the
		// caller having numbered the events.
		cloned.Seq = session.Seq(index)
		stored = append(stored, cloned)
	}
	s.sessions[id] = stored
}

// FailOpen makes every Open fail with err.
func (s *FakeStore) FailOpen(err error) { s.mu.Lock(); s.openErr = err; s.mu.Unlock() }

// FailStat makes every Stat fail with err.
func (s *FakeStore) FailStat(err error) { s.mu.Lock(); s.statErr = err; s.mu.Unlock() }

// FailAppend makes every Append fail with err.
func (s *FakeStore) FailAppend(err error) { s.mu.Lock(); s.appendErr = err; s.mu.Unlock() }

// FailFlush makes every Flush fail with err.
func (s *FakeStore) FailFlush(err error) { s.mu.Lock(); s.flushErr = err; s.mu.Unlock() }

// Opened reports how many handles were opened and how many were closed.
//
// The pair is returned together because the interesting assertion is almost
// always "every handle was released", which comparing two counters expresses
// without the caller having to remember both.
func (s *FakeStore) Opened() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened, s.closed
}

// Events returns a copy of a session's stored events.
func (s *FakeStore) Events(id persistence.SessionID) []session.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.sessions[id]
	out := make([]session.Event, 0, len(stored))
	for _, event := range stored {
		out = append(out, event.Clone())
	}
	return out
}

// Open returns a handle over a scripted session.
func (s *FakeStore) Open(ctx context.Context, options persistence.OpenOptions) (persistence.Handle, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openErr != nil {
		return nil, s.openErr
	}
	stored, ok := s.sessions[options.ID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", persistence.ErrSessionNotFound, options.ID)
	}
	handle := &fakeHandle{store: s, events: make([]session.Event, 0, len(stored))}
	for _, event := range stored {
		handle.events = append(handle.events, event.Clone())
	}
	handle.Init(options.ID, options.Access)
	s.opened++
	return handle, nil
}

// Stat describes a scripted session.
func (s *FakeStore) Stat(ctx context.Context, id persistence.SessionID) (persistence.Stat, error) {
	if id.IsZero() {
		return persistence.Stat{}, fmt.Errorf("%w: stat requires a session id", persistence.ErrInvalidValue)
	}
	if err := ctx.Err(); err != nil {
		return persistence.Stat{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.statErr != nil {
		return persistence.Stat{}, s.statErr
	}
	stored, ok := s.sessions[id]
	if !ok {
		return persistence.Stat{}, fmt.Errorf("%w: %s", persistence.ErrSessionNotFound, id)
	}
	return persistence.Stat{ID: id, FormatVersion: 1, Events: len(stored)}, nil
}

// fakeHandle is a handle over the scripted store.
type fakeHandle struct {
	// HandleBase carries the shared closed and read-only rules.
	persistence.HandleBase
	// store is the owning fake.
	store *FakeStore
	// events is this handle's view.
	events []session.Event
	// closedMarker records that this handle released through the store.
	closedMarker bool
}

// Read returns the stored events from an offset.
func (h *fakeHandle) Read(ctx context.Context, from session.Seq) ([]session.Event, error) {
	if err := h.HandleBase.CheckOpen(false); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	if int(from) >= len(h.events) {
		return nil, nil
	}
	out := make([]session.Event, 0, len(h.events)-int(from))
	for _, event := range h.events[int(from):] {
		out = append(out, event.Clone())
	}
	return out, nil
}

// Append records one event.
func (h *fakeHandle) Append(ctx context.Context, input session.AppendInput) (session.Event, error) {
	if err := h.HandleBase.CheckOpen(true); err != nil {
		return session.Event{}, err
	}
	if err := ctx.Err(); err != nil {
		return session.Event{}, err
	}
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	if h.store.appendErr != nil {
		return session.Event{}, h.store.appendErr
	}
	if input.Type == "" || len(input.Data) == 0 {
		return session.Event{}, fmt.Errorf("%w: event requires a type and data", persistence.ErrInvalidValue)
	}
	event := session.Event{
		Type: input.Type, Seq: session.Seq(len(h.events)), Data: append([]byte(nil), input.Data...),
		HasIgnorable: input.Ignorable, SourceEventSeqs: append([]session.Seq(nil), input.SourceEventSeqs...),
	}
	if input.SurfaceOp != nil {
		op := *input.SurfaceOp
		event.SurfaceOp = &op
	}
	if err := session.ValidateEventEnvelope(event); err != nil {
		return session.Event{}, err
	}
	h.events = append(h.events, event.Clone())
	h.store.sessions[h.ID()] = append([]session.Event(nil), h.events...)
	return event, nil
}

// Flush applies the scripted flush failure, if any.
func (h *fakeHandle) Flush(ctx context.Context) error {
	if err := h.HandleBase.CheckOpen(false); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	return h.store.flushErr
}

// Close releases the handle and counts the release once.
func (h *fakeHandle) Close() error {
	if h.HandleBase.Closed() {
		return nil
	}
	_ = h.HandleBase.Close()
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	if !h.closedMarker {
		h.closedMarker = true
		h.store.closed++
	}
	return nil
}

// UserMessage builds a user message event payload.
//
// It exists so a fixture does not have to construct the message JSON by hand:
// the shape is the log's, and getting it wrong produces an error that points at
// the fold rather than at the fixture.
func UserMessage(id, text string) ([]byte, error) {
	message, err := llm.NewUserMessage(text)
	if err != nil {
		return nil, err
	}
	message.ID = llm.MessageID(id)
	return llm.MarshalMessage(message)
}

// NestedMessage renders a message payload in the nested form the log expects.
func NestedMessage(message []byte) ([]byte, error) {
	return persistence.NestMessage(message)
}

// AssistantMessage builds a nested assistant message event payload.
func AssistantMessage(id, provider, model, text string) ([]byte, error) {
	message, err := llm.NewAssistantMessage(provider, model, llm.TextContent(text))
	if err != nil {
		return nil, err
	}
	message.ID = llm.MessageID(id)
	data, err := llm.MarshalMessage(message)
	if err != nil {
		return nil, err
	}
	return persistence.NestMessage(data)
}

var (
	_ persistence.Store  = (*FakeStore)(nil)
	_ persistence.Handle = (*fakeHandle)(nil)
)
