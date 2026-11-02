package jobs_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/jobs"
)

func fixedClock(base time.Time) func() time.Time {
	current := base
	return func() time.Time { return current }
}

func TestEnqueueIdempotency(t *testing.T) {
	s := jobs.NewStore()
	first, err := s.Enqueue(jobs.Job{ID: "j1", Kind: "k", IdempotencyKey: "key"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	second, err := s.Enqueue(jobs.Job{ID: "j2", Kind: "k", IdempotencyKey: "key"})
	if err != nil {
		t.Fatalf("Enqueue dup: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("idempotency returned %q, want %q", second.ID, first.ID)
	}
	if len(s.IDs()) != 1 {
		t.Fatalf("ids = %v", s.IDs())
	}
}

func TestClaimLeaseAndExpiry(t *testing.T) {
	base := time.Unix(1000, 0)
	clock := fixedClock(base)
	s := jobs.NewStore()
	s.SetClock(clock)
	s.SetLease(10 * time.Second)
	_, _ = s.Enqueue(jobs.Job{ID: "j1", Kind: "k"})

	job, err := s.Claim("j1", "owner-a")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if job.Status != jobs.StatusRunning || job.Attempts != 1 {
		t.Fatalf("job = %+v", job)
	}
	// A second claimer while the lease is live is refused.
	if _, err := s.Claim("j1", "owner-b"); !errors.Is(err, jobs.ErrLeaseHeld) {
		t.Fatalf("second claim err = %v", err)
	}
	// Advance past the lease: the job becomes reclaimable (crash recovery).
	clock2 := func() time.Time { return base.Add(20 * time.Second) }
	s.SetClock(clock2)
	if _, err := s.Claim("j1", "owner-b"); err != nil {
		t.Fatalf("reclaim after expiry: %v", err)
	}
}

func TestSettleAndNoRestart(t *testing.T) {
	s := jobs.NewStore()
	_, _ = s.Enqueue(jobs.Job{ID: "j1", Kind: "k"})
	if _, err := s.Claim("j1", "o"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := s.Settle("j1"); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if _, err := s.Claim("j1", "o"); !errors.Is(err, jobs.ErrNotClaimable) {
		t.Fatalf("settled job reclaimable: %v", err)
	}
}

func TestFailRetry(t *testing.T) {
	s := jobs.NewStore()
	_, _ = s.Enqueue(jobs.Job{ID: "j1", Kind: "k", MaxAttempts: 1})
	if _, err := s.Claim("j1", "o"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	job, err := s.Fail("j1", "boom")
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if job.Status != jobs.StatusPending {
		t.Fatalf("first fail should retry, got %s", job.Status)
	}
	// Claim again (attempt 2) then fail: attempts (2) > MaxAttempts (1) -> failed.
	if _, err := s.Claim("j1", "o"); err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	job, err = s.Fail("j1", "boom again")
	if err != nil {
		t.Fatalf("Fail 2: %v", err)
	}
	if job.Status != jobs.StatusFailed {
		t.Fatalf("second fail should be terminal, got %s", job.Status)
	}
}

func TestMarkUnknown(t *testing.T) {
	s := jobs.NewStore()
	_, _ = s.Enqueue(jobs.Job{ID: "j1", Kind: "k"})
	job, err := s.MarkUnknown("j1", "lost settlement")
	if err != nil {
		t.Fatalf("MarkUnknown: %v", err)
	}
	if job.Status != jobs.StatusUnknown {
		t.Fatalf("status = %s", job.Status)
	}
	if _, err := s.Claim("j1", "o"); !errors.Is(err, jobs.ErrNotClaimable) {
		t.Fatalf("unknown job claimable: %v", err)
	}
	if len(s.ByStatus(jobs.StatusUnknown)) != 1 {
		t.Fatal("ByStatus unknown")
	}
}

func TestValidateAndUnknown(t *testing.T) {
	s := jobs.NewStore()
	if _, err := s.Enqueue(jobs.Job{ID: "", Kind: "k"}); !errors.Is(err, jobs.ErrInvalidJob) {
		t.Fatalf("empty id err = %v", err)
	}
	if _, err := s.Claim("nope", "o"); !errors.Is(err, jobs.ErrUnknownJob) {
		t.Fatalf("unknown err = %v", err)
	}
}

func TestDuplicateID(t *testing.T) {
	s := jobs.NewStore()
	_, _ = s.Enqueue(jobs.Job{ID: "j1", Kind: "k"})
	if _, err := s.Enqueue(jobs.Job{ID: "j1", Kind: "k"}); !errors.Is(err, jobs.ErrInvalidJob) {
		t.Fatalf("duplicate id err = %v", err)
	}
}

func TestFinishUnknownAndWrongState(t *testing.T) {
	s := jobs.NewStore()
	if _, err := s.Settle("nope"); !errors.Is(err, jobs.ErrUnknownJob) {
		t.Fatalf("settle unknown err = %v", err)
	}
	_, _ = s.Enqueue(jobs.Job{ID: "j1", Kind: "k"})
	// Failing a pending (not running) job is refused.
	if _, err := s.Fail("j1", "x"); !errors.Is(err, jobs.ErrNotClaimable) {
		t.Fatalf("fail non-running err = %v", err)
	}
	if _, err := s.Fail("nope", "x"); !errors.Is(err, jobs.ErrUnknownJob) {
		t.Fatalf("fail unknown err = %v", err)
	}
	if _, err := s.MarkUnknown("nope", "x"); !errors.Is(err, jobs.ErrUnknownJob) {
		t.Fatalf("markunknown unknown err = %v", err)
	}
}

func TestSetLeaseInvalidIgnored(t *testing.T) {
	s := jobs.NewStore()
	s.SetLease(0) // ignored, keeps default
	_, _ = s.Enqueue(jobs.Job{ID: "j1", Kind: "k"})
	job, err := s.Claim("j1", "o")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if job.LeaseUntil.IsZero() {
		t.Fatal("lease not set")
	}
}

func TestByStatusOrdering(t *testing.T) {
	s := jobs.NewStore()
	for _, id := range []string{"b", "a", "c"} {
		_, _ = s.Enqueue(jobs.Job{ID: id, Kind: "k"})
	}
	pend := s.ByStatus(jobs.StatusPending)
	if len(pend) != 3 || pend[0].ID != "a" || pend[2].ID != "c" {
		t.Fatalf("ordering = %v", []string{pend[0].ID, pend[1].ID, pend[2].ID})
	}
}

func TestGetAndDefaultStatus(t *testing.T) {
	s := jobs.NewStore()
	job, _ := s.Enqueue(jobs.Job{ID: "j1", Kind: "k"})
	if job.Status != jobs.StatusPending {
		t.Fatalf("default status = %s", job.Status)
	}
	got, ok := s.Get("j1")
	if !ok || got.ID != "j1" {
		t.Fatalf("get = %+v ok=%v", got, ok)
	}
	if _, ok := s.Get("nope"); ok {
		t.Fatal("get unknown should be false")
	}
}
