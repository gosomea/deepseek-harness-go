package schedule

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/jobs"
)

// Errors this package returns.
var (
	// ErrDuplicate reports a second registration under one name.
	ErrDuplicate = fmt.Errorf("schedule: duplicate entry")
)

// Entry is one scheduled trigger for a job kind.
//
// It exists so "what is due" is a question about named entries and their next
// time, not about goroutines the scheduler happens to have started. A fixed
// interval is enough for the fake half; a cron expression is a later refinement.
type Entry struct {
	// Name is the entry's stable identity. Required.
	Name string
	// Kind is the job kind a trigger enqueues. Required.
	Kind string
	// Interval is the period between triggers. Required and positive.
	Interval time.Duration
	// NextRun is when the entry next becomes due.
	NextRun time.Time
}

// Scheduler fires due entries into a jobs store on a controllable clock.
type Scheduler struct {
	store   *jobs.Store
	now     func() time.Time
	mu      sync.Mutex
	entries map[string]*Entry
	order   []string
	// seq is a monotonic counter so enqueued jobs get unique ids deterministically.
	seq int
}

// New returns a scheduler over a jobs store and a clock.
func New(store *jobs.Store, now func() time.Time) *Scheduler {
	if now == nil {
		now = time.Now
	}
	return &Scheduler{store: store, now: now, entries: map[string]*Entry{}}
}

// Register adds a fixed-interval entry, first due one interval from now.
func (s *Scheduler) Register(name, kind string, interval time.Duration) error {
	if name == "" || kind == "" {
		return fmt.Errorf("schedule: name and kind are required")
	}
	if interval <= 0 {
		return fmt.Errorf("schedule: interval must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.entries[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicate, name)
	}
	s.entries[name] = &Entry{Name: name, Kind: kind, Interval: interval, NextRun: s.now().Add(interval)}
	s.order = append(s.order, name)
	return nil
}

// Tick enqueues a job for every entry that is due at the current clock, returning
// the enqueued job ids.
//
// It advances each fired entry to its next time past now, so a single tick never
// enqueues the same entry twice. Enqueuing through the jobs store gives each job
// an idempotency key derived from entry and fire time, so a re-run of the same
// tick does not duplicate work.
func (s *Scheduler) Tick() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	names := append([]string(nil), s.order...)
	sort.Strings(names)
	var fired []string
	for _, name := range names {
		entry := s.entries[name]
		for !entry.NextRun.After(now) {
			fireAt := entry.NextRun
			s.seq++
			id := fmt.Sprintf("%s@%d", name, fireAt.UnixNano())
			job, err := s.store.Enqueue(jobs.Job{
				ID:             id,
				Kind:           entry.Kind,
				IdempotencyKey: id,
			})
			if err != nil {
				return fired, err
			}
			fired = append(fired, job.ID)
			entry.NextRun = entry.NextRun.Add(entry.Interval)
		}
	}
	return fired, nil
}

// NextRun returns the next due time for an entry.
func (s *Scheduler) NextRun(name string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[name]
	if !ok {
		return time.Time{}, false
	}
	return entry.NextRun, true
}
