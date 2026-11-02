package persistence

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
)

// Cache is a rebuildable derived view of one session's history.
//
// It is deliberately not authoritative. The log is the record; the cache is a
// convenience that must be discardable at any moment, and the contract is that
// rebuilding from the log after a discard produces the same answer. A cache that
// could disagree with the log would be worse than no cache, because a reader
// would not know which one to trust.
type Cache struct {
	// mu guards the cached value.
	mu sync.Mutex
	// events is the log length the cache was built from.
	builtFrom int
	// messages is the derived history.
	messages []llm.Message
	// valid reports whether the cache currently holds a usable value.
	valid bool
}

// NewCache returns an empty cache.
func NewCache() *Cache { return &Cache{} }

// Messages returns the derived history, rebuilding it when the log has grown.
//
// A cache with no value, or one built from a shorter log, rebuilds rather than
// returning something stale: a reader must never see a history shorter than the
// log it came from.
func (c *Cache) Messages(events []session.Event, projections ...session.MessageProjection) ([]llm.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && c.builtFrom == len(events) {
		return append([]llm.Message(nil), c.messages...), nil
	}
	messages, err := session.DeriveMessages(events, projections...)
	if err != nil {
		// A failed rebuild leaves no usable value, so the next call tries again
		// instead of serving a partial history.
		c.valid = false
		c.messages = nil
		return nil, err
	}
	c.messages = append([]llm.Message(nil), messages...)
	c.builtFrom = len(events)
	c.valid = true
	return append([]llm.Message(nil), messages...), nil
}

// Discard drops the cached value.
//
// It is the operation the contract's test uses to prove the cache is not
// authoritative: after a discard, the next read must rebuild and agree.
func (c *Cache) Discard() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid = false
	c.messages = nil
	c.builtFrom = 0
}

// Valid reports whether the cache currently holds a value.
func (c *Cache) Valid() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.valid
}

// ErrMissingProjection reports a log whose surface needs an interpreter that was
// not supplied.
//
// It exists so a caller cannot receive a shorter history and mistake it for the
// session's actual content: a missing interpreter is a configuration mistake,
// and the honest answer is to fail.
var ErrMissingProjection = session.ErrInvalidEvent

// CheckProjections reports whether the supplied interpreters can account for
// every surface-eligible event in the log.
//
// It answers "would a fold succeed" before the fold runs, which is what lets a
// caller report the missing event type rather than a generic failure.
func CheckProjections(events []session.Event, registry *session.Registry) error {
	if registry == nil {
		return fmt.Errorf("%w: projection check requires a registry", ErrInvalidValue)
	}
	for index, event := range events {
		if !event.IsSurfaceEligible() {
			continue
		}
		if !registry.Knows(event.Type) {
			return fmt.Errorf("%w: event %d has unknown type %q", ErrMissingProjection, index, event.Type)
		}
	}
	return nil
}

// Store is the seam a session's durable state is read and written through.
//
// It is narrower than Service on purpose: a component that only needs to reopen
// one session does not need the ability to create, list or delete others.
type Store interface {
	// Open returns a handle for one session with the requested access.
	Open(ctx context.Context, options OpenOptions) (Handle, error)
	// Stat describes one session without opening it.
	Stat(ctx context.Context, id SessionID) (Stat, error)
}

// Restore reopens a session and returns its restored state together with the
// history the log derives.
//
// The two are returned together because they answer the two questions a resume
// must answer: what happened, and what is still owed. Rebuilding the cache here
// rather than at first use means a caller cannot accidentally proceed on an
// empty view.
func Restore(ctx context.Context, store Store, id SessionID, cache *Cache) (ResumeState, []llm.Message, error) {
	if store == nil {
		return ResumeState{}, nil, fmt.Errorf("%w: restore requires a store", ErrInvalidValue)
	}
	handle, err := store.Open(ctx, OpenOptions{ID: id, Access: AccessRead})
	if err != nil {
		return ResumeState{}, nil, err
	}
	defer func() { _ = handle.Close() }()
	state, err := Resume(ctx, handle)
	if err != nil {
		return ResumeState{}, nil, err
	}
	if cache == nil {
		cache = NewCache()
	}
	messages, err := cache.Messages(state.Events)
	if err != nil {
		return ResumeState{}, nil, err
	}
	return state, messages, nil
}
