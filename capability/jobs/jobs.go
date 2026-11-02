package jobs

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Errors this package returns.
var (
	// ErrUnknownJob reports an id the store does not hold.
	ErrUnknownJob = errors.New("jobs: unknown job")
	// ErrLeaseHeld reports a claim on a job another owner already holds.
	ErrLeaseHeld = errors.New("jobs: lease held by another owner")
	// ErrNotClaimable reports a claim on a job that is not runnable.
	ErrNotClaimable = errors.New("jobs: job is not claimable")
	// ErrInvalidJob reports a job record that cannot be stored.
	ErrInvalidJob = errors.New("jobs: invalid job")
)

// Status is a job's state in its machine.
type Status string

// The job states this package names.
const (
	// StatusPending is requested but not yet claimed.
	StatusPending Status = "pending"
	// StatusRunning is claimed and in progress.
	StatusRunning Status = "running"
	// StatusSettled is finished successfully.
	StatusSettled Status = "settled"
	// StatusFailed is finished with a recorded failure.
	StatusFailed Status = "failed"
	// StatusUnknown is a job whose external effect may or may not have happened
	// and whose settlement was not recorded. A human must resolve it; it is never
	// replayed automatically.
	StatusUnknown Status = "unknown"
)

// Job is one durable task record.
type Job struct {
	// ID is the job's stable identity. Required.
	ID string
	// Kind names the work, used by the runner to dispatch. Required.
	Kind string
	// Status is the current state.
	Status Status
	// IdempotencyKey, when set, makes a duplicate request an existing job rather
	// than a second one.
	IdempotencyKey string
	// Attempts is how many times the job has been claimed and run.
	Attempts int
	// MaxAttempts bounds retries. Zero means no retry after the first failure.
	MaxAttempts int
	// Owner is who currently holds the lease.
	Owner string
	// LeaseUntil is when the current lease expires.
	LeaseUntil time.Time
	// LastError records the most recent failure.
	LastError string
}

// Validate reports whether a job record can be stored.
func (j Job) Validate() error {
	if strings.TrimSpace(j.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidJob)
	}
	if strings.TrimSpace(j.Kind) == "" {
		return fmt.Errorf("%w: kind is required", ErrInvalidJob)
	}
	if j.MaxAttempts < 0 {
		return fmt.Errorf("%w: MaxAttempts must not be negative", ErrInvalidJob)
	}
	return nil
}

// Clock reports the current time, so tests can advance it deterministically.
type Clock func() time.Time

// Store is an in-memory durable job store.
//
// It is a store rather than a scheduler: it records jobs and lets an owner claim
// them under a lease, but it does not run them. Keeping the record and the runner
// apart is what lets a test drive the state machine with a controlled clock.
type Store struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	byKey  map[string]string
	order  []string
	now    Clock
	leaseT time.Duration
}

// NewStore returns an empty store using a real clock.
func NewStore() *Store {
	return &Store{jobs: map[string]*Job{}, byKey: map[string]string{}, now: time.Now, leaseT: time.Minute}
}

// SetClock installs a deterministic clock.
func (s *Store) SetClock(now Clock) {
	if s == nil || now == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// SetLease sets the lease duration granted on claim.
func (s *Store) SetLease(d time.Duration) {
	if s == nil || d <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leaseT = d
}

// Enqueue records a new job, returning the existing record when an idempotency
// key already names one.
//
// The idempotency check is what makes a repeated request safe: the second request
// gets the first job back instead of creating a duplicate that would run twice.
func (s *Store) Enqueue(job Job) (Job, error) {
	if err := job.Validate(); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.IdempotencyKey != "" {
		if existingID, ok := s.byKey[job.IdempotencyKey]; ok {
			return *s.jobs[existingID], nil
		}
		s.byKey[job.IdempotencyKey] = job.ID
	}
	if _, exists := s.jobs[job.ID]; exists {
		return Job{}, fmt.Errorf("%w: duplicate id %q", ErrInvalidJob, job.ID)
	}
	if job.Status == "" {
		job.Status = StatusPending
	}
	stored := job
	s.jobs[job.ID] = &stored
	s.order = append(s.order, job.ID)
	return stored, nil
}

// Get returns a job by id, reporting whether it exists.
func (s *Store) Get(id string) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *job, true
}

// Claim takes the lease on a pending job for an owner.
//
// It refuses a job that is not pending (so a settled job cannot be restarted) and
// a job whose lease is still held by someone else (so two runners do not both run
// it). An expired lease is reclaimable, which is how a crashed runner's job is
// recovered rather than stuck forever.
func (s *Store) Claim(id, owner string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("%w: %q", ErrUnknownJob, id)
	}
	now := s.now()
	if job.Status == StatusRunning && job.LeaseUntil.After(now) && job.Owner != owner {
		return Job{}, fmt.Errorf("%w: %q by %q", ErrLeaseHeld, id, job.Owner)
	}
	if job.Status != StatusPending && !(job.Status == StatusRunning && !job.LeaseUntil.After(now)) {
		return Job{}, fmt.Errorf("%w: %q is %s", ErrNotClaimable, id, job.Status)
	}
	job.Status = StatusRunning
	job.Owner = owner
	job.LeaseUntil = now.Add(s.leaseT)
	job.Attempts++
	return *job, nil
}

// Settle marks a running job finished successfully.
func (s *Store) Settle(id string) (Job, error) {
	return s.finish(id, StatusSettled, "")
}

// Fail records a failure, retrying while attempts remain, otherwise failing.
//
// A job with attempts left returns to pending so it can be claimed again; one
// with none is failed. Neither case is "unknown": a recorded failure is a
// settlement, so it is not replayed blindly.
func (s *Store) Fail(id string, reason string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("%w: %q", ErrUnknownJob, id)
	}
	if job.Status != StatusRunning {
		return Job{}, fmt.Errorf("%w: %q is %s", ErrNotClaimable, id, job.Status)
	}
	job.LastError = reason
	job.Owner = ""
	job.LeaseUntil = time.Time{}
	if job.Attempts <= job.MaxAttempts {
		job.Status = StatusPending
	} else {
		job.Status = StatusFailed
	}
	return *job, nil
}

// MarkUnknown records that a running job may have produced an external effect but
// its settlement was lost, so it must not be replayed automatically.
func (s *Store) MarkUnknown(id, reason string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("%w: %q", ErrUnknownJob, id)
	}
	job.Status = StatusUnknown
	job.LastError = reason
	return *job, nil
}

// finish sets a terminal status.
func (s *Store) finish(id string, status Status, reason string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("%w: %q", ErrUnknownJob, id)
	}
	job.Status = status
	job.LastError = reason
	job.Owner = ""
	job.LeaseUntil = time.Time{}
	return *job, nil
}

// IDs returns the stored job ids in insertion order.
func (s *Store) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// ByStatus returns the jobs in a status, in id order.
func (s *Store) ByStatus(status Status) []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Job
	for _, id := range s.order {
		if s.jobs[id].Status == status {
			out = append(out, *s.jobs[id])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
