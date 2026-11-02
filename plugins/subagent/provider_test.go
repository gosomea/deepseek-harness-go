package subagent_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gosomea/deepseek-harness-go/agent"
	seam "github.com/gosomea/deepseek-harness-go/capability/subagent"
	plugin "github.com/gosomea/deepseek-harness-go/plugins/subagent"
)

// fakeHandle is a minimal agent.Handle.
type fakeHandle struct{ id agent.ID }

func (h *fakeHandle) Info() agent.Info                        { return agent.Info{ID: h.id} }
func (h *fakeHandle) State() agent.State                      { return "running" }
func (h *fakeHandle) Send(context.Context, agent.Input) error { return nil }
func (h *fakeHandle) Wait(context.Context) error              { return nil }
func (h *fakeHandle) Done() <-chan struct{}                   { return closedCh }

var closedCh = func() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}()

type fakeInstance struct {
	handle   *fakeHandle
	disposed bool
}

func (i *fakeInstance) Dispose(context.Context) error { i.disposed = true; return nil }
func (i *fakeInstance) Handle() agent.Handle          { return i.handle }

type fakeFactory struct {
	mu   sync.Mutex
	seq  int
	made []*fakeInstance
}

func (f *fakeFactory) Create(_ context.Context, spec agent.Spec) (agent.Owner, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	inst := &fakeInstance{handle: &fakeHandle{id: agent.ID("child-" + string(rune('0'+f.seq)))}}
	f.made = append(f.made, inst)
	return inst, nil
}
func (f *fakeFactory) Active() []agent.Handle           { return nil }
func (f *fakeFactory) DisposeAll(context.Context) error { return nil }

func TestSpawnRequiresRegisteredParent(t *testing.T) {
	p, err := plugin.New(plugin.Options{Factory: &fakeFactory{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Spawn(context.Background(), seam.Spec{Parent: agent.ID("unknown"), Mode: seam.ModeSpawn})
	if !errors.Is(err, plugin.ErrParentMissing) {
		t.Fatalf("err = %v, want ErrParentMissing", err)
	}
}

func TestSpawnAndLineage(t *testing.T) {
	factory := &fakeFactory{}
	p, err := plugin.New(plugin.Options{Factory: factory})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.RegisterRoot(agent.ID("root")); err != nil {
		t.Fatalf("RegisterRoot: %v", err)
	}
	owner, err := p.Spawn(context.Background(), seam.Spec{Parent: agent.ID("root"), Mode: seam.ModeSpawn})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	child := owner.Handle().Info().ID
	lineage, ok := p.LineageOf(child)
	if !ok || lineage.Parent != agent.ID("root") || lineage.Mode != seam.ModeSpawn {
		t.Fatalf("lineage = %+v ok=%v", lineage, ok)
	}
	if len(p.Children()) != 1 {
		t.Fatalf("children = %v", p.Children())
	}
	if p.Info().ID != "in-process" {
		t.Fatalf("default id = %q", p.Info().ID)
	}
}

func TestForkLineageAndDispose(t *testing.T) {
	factory := &fakeFactory{}
	p, _ := plugin.New(plugin.Options{Factory: factory, ID: "x", Name: "X"})
	_ = p.RegisterRoot(agent.ID("root"))
	owner, err := p.Spawn(context.Background(), seam.Spec{Parent: agent.ID("root"), Mode: seam.ModeFork, InheritThrough: 5, Depth: 1})
	if err != nil {
		t.Fatalf("Spawn fork: %v", err)
	}
	child := owner.Handle().Info().ID
	lineage, _ := p.LineageOf(child)
	if lineage.InheritThrough != 5 || lineage.Depth != 1 {
		t.Fatalf("fork lineage = %+v", lineage)
	}
	if err := p.DisposeChild(context.Background(), child); err != nil {
		t.Fatalf("DisposeChild: %v", err)
	}
	if err := p.DisposeChild(context.Background(), agent.ID("nope")); !errors.Is(err, plugin.ErrParentMissing) {
		t.Fatalf("dispose unknown err = %v", err)
	}
}

func TestRegisterRootEmptyAndNoFactory(t *testing.T) {
	if _, err := plugin.New(plugin.Options{}); !errors.Is(err, plugin.ErrNoFactory) {
		t.Fatalf("no factory err = %v", err)
	}
	p, _ := plugin.New(plugin.Options{Factory: &fakeFactory{}})
	if err := p.RegisterRoot(agent.ID("")); err == nil {
		t.Fatal("empty root should fail")
	}
}
