package schedule_test

import (
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/jobs"
	"github.com/gosomea/deepseek-harness-go/plugins/schedule"
)

func TestRegisterAndTick(t *testing.T) {
	base := time.Unix(1000, 0)
	current := base
	now := func() time.Time { return current }
	store := jobs.NewStore()
	scheduler := schedule.New(store, now)
	if err := scheduler.Register("heartbeat", "beat", time.Minute); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := scheduler.Register("heartbeat", "beat", time.Minute); err == nil {
		t.Fatal("duplicate registration should fail")
	}
	// Not due yet.
	fired, err := scheduler.Tick()
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(fired) != 0 {
		t.Fatalf("fired too early: %v", fired)
	}
	// Advance one interval: one trigger.
	current = base.Add(time.Minute)
	fired, err = scheduler.Tick()
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(fired) != 1 {
		t.Fatalf("fired = %v", fired)
	}
	// Same tick again must not duplicate (idempotency key).
	fired, _ = scheduler.Tick()
	if len(fired) != 0 {
		t.Fatalf("re-tick duplicated: %v", fired)
	}
	if next, ok := scheduler.NextRun("heartbeat"); !ok || !next.After(current) {
		t.Fatalf("next run = %v ok=%v", next, ok)
	}
}

func TestRegisterValidation(t *testing.T) {
	scheduler := schedule.New(jobs.NewStore(), nil)
	if err := scheduler.Register("", "k", time.Minute); err == nil {
		t.Fatal("empty name should fail")
	}
	if err := scheduler.Register("n", "", time.Minute); err == nil {
		t.Fatal("empty kind should fail")
	}
	if err := scheduler.Register("n", "k", 0); err == nil {
		t.Fatal("zero interval should fail")
	}
}
