// Command subagents demonstrates spawning and forking children through the
// in-process subagent provider, and shows that a fork records an inheritance cut
// rather than re-creating the parent's facts.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/capability/subagent"
	provider "github.com/gosomea/deepseek-harness-go/plugins/subagent"
)

// stubHandle is a minimal agent.Handle for the demonstration.
type stubHandle struct{ id agent.ID }

// Info describes the stub Agent.
func (h *stubHandle) Info() agent.Info { return agent.Info{ID: h.id} }

// State reports the stub Agent state.
func (h *stubHandle) State() agent.State { return "running" }

// Send ignores input for the stub.
func (h *stubHandle) Send(context.Context, agent.Input) error { return nil }

// Wait returns immediately for the stub.
func (h *stubHandle) Wait(context.Context) error { return nil }

// Done returns a closed channel for the stub.
func (h *stubHandle) Done() <-chan struct{} { c := make(chan struct{}); close(c); return c }

// stubInstance is a minimal agent.Owner.
type stubInstance struct{ h *stubHandle }

// Dispose releases the stub instance.
func (i *stubInstance) Dispose(context.Context) error { return nil }

// Handle returns the stub handle.
func (i *stubInstance) Handle() agent.Handle { return i.h }

// stubFactory creates stub children.
type stubFactory struct{ n int }

// Create builds a stub child.
func (f *stubFactory) Create(_ context.Context, spec agent.Spec) (agent.Owner, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	f.n++
	return &stubInstance{h: &stubHandle{id: agent.ID(fmt.Sprintf("child-%d", f.n))}}, nil
}

// Active returns no handles for the stub.
func (f *stubFactory) Active() []agent.Handle { return nil }

// DisposeAll is a no-op for the stub.
func (f *stubFactory) DisposeAll(context.Context) error { return nil }

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	ctx := context.Background()
	p, err := provider.New(provider.Options{Factory: &stubFactory{}})
	if err != nil {
		return err
	}
	if err := p.RegisterRoot(agent.ID("root")); err != nil {
		return err
	}

	spawned, err := p.Spawn(ctx, subagent.Spec{Parent: agent.ID("root"), Mode: subagent.ModeSpawn, Depth: 1})
	if err != nil {
		return err
	}
	forked, err := p.Spawn(ctx, subagent.Spec{Parent: agent.ID("root"), Mode: subagent.ModeFork, InheritThrough: 7, Depth: 1})
	if err != nil {
		return err
	}

	spawnLineage, _ := p.LineageOf(spawned.Handle().Info().ID)
	forkLineage, _ := p.LineageOf(forked.Handle().Info().ID)
	fmt.Fprintf(out, "spawn lineage: mode=%s inherit=%d\n", spawnLineage.Mode, spawnLineage.InheritThrough)
	fmt.Fprintf(out, "fork lineage: mode=%s inherit=%d\n", forkLineage.Mode, forkLineage.InheritThrough)
	fmt.Fprintf(out, "children: %d\n", len(p.Children()))
	fmt.Fprintf(out, "provider: %s\n", p.Info().ID)
	return nil
}
