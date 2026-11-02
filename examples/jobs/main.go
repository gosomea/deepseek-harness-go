// Command jobs demonstrates durable jobs on a controllable clock: a scheduler
// fires an interval job, the job is claimed and settled, and a crash with an
// unknown settlement is shown to be left resolvable rather than replayed.
package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/jobs"
	"github.com/gosomea/deepseek-harness-go/plugins/schedule"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	base := time.Unix(1000, 0)
	current := base
	now := func() time.Time { return current }

	store := jobs.NewStore()
	store.SetClock(now)
	store.SetLease(30 * time.Second)
	scheduler := schedule.New(store, now)
	if err := scheduler.Register("heartbeat", "beat", time.Minute); err != nil {
		return err
	}

	// Advance one interval: the heartbeat fires one job.
	current = base.Add(time.Minute)
	fired, err := scheduler.Tick()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "fired: %d\n", len(fired))

	// Claim and settle it.
	id := fired[0]
	if _, err := store.Claim(id, "runner-1"); err != nil {
		return err
	}
	settled, err := store.Settle(id)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "settled status: %s\n", settled.Status)

	// A second tick while the first fire already settled must not duplicate.
	again, err := scheduler.Tick()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "duplicate fires: %d\n", len(again))

	// A job whose settlement was lost is marked unknown and not claimable.
	_, _ = store.Enqueue(jobs.Job{ID: "orphan", Kind: "beat", MaxAttempts: 0})
	if _, err := store.Claim("orphan", "runner-1"); err != nil {
		return err
	}
	if _, err := store.MarkUnknown("orphan", "settlement lost"); err != nil {
		return err
	}
	_, claimErr := store.Claim("orphan", "runner-2")
	fmt.Fprintf(out, "unknown not claimable: %t\n", claimErr != nil)
	fmt.Fprintf(out, "unknown count: %d\n", len(store.ByStatus(jobs.StatusUnknown)))
	return nil
}
