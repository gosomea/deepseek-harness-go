package subagent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/capability/subagent"
)

type fakeProvider struct {
	info     subagent.ProviderInfo
	spawned  []subagent.Spec
	failWith error
}

func (f *fakeProvider) Spawn(_ context.Context, spec subagent.Spec) (agent.Owner, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	f.spawned = append(f.spawned, spec)
	return nil, nil
}

func (f *fakeProvider) Info() subagent.ProviderInfo { return f.info }

func TestSpecValidate(t *testing.T) {
	ok := subagent.Spec{Parent: agent.ID("root"), Mode: subagent.ModeSpawn}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid spawn rejected: %v", err)
	}
	fork := subagent.Spec{Parent: agent.ID("root"), Mode: subagent.ModeFork, InheritThrough: 3}
	if err := fork.Validate(); err != nil {
		t.Fatalf("valid fork rejected: %v", err)
	}
	cases := []subagent.Spec{
		{Mode: subagent.ModeSpawn},
		{Parent: agent.ID("r"), Mode: subagent.ModeSpawn, InheritThrough: 2},
		{Parent: agent.ID("r"), Mode: subagent.ModeFork},
		{Parent: agent.ID("r"), Mode: "bogus"},
		{Parent: agent.ID("r"), Mode: subagent.ModeSpawn, Depth: -1},
	}
	for i, spec := range cases {
		if err := spec.Validate(); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestRegistryRegisterAndResolve(t *testing.T) {
	r := subagent.NewRegistry()
	p := &fakeProvider{info: subagent.ProviderInfo{ID: "in-process", Capabilities: []subagent.Capability{subagent.CapabilitySpawn}}}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(p); !errors.Is(err, subagent.ErrDuplicateProvider) {
		t.Fatalf("duplicate err = %v", err)
	}
	if _, ok := r.Resolve("in-process"); !ok {
		t.Fatal("resolve failed")
	}
	if infos := r.Infos(); len(infos) != 1 || infos[0].ID != "in-process" {
		t.Fatalf("infos = %+v", infos)
	}
}

func TestRegistrySpawnChecksCapability(t *testing.T) {
	r := subagent.NewRegistry()
	p := &fakeProvider{info: subagent.ProviderInfo{ID: "spawn-only", Capabilities: []subagent.Capability{subagent.CapabilitySpawn}}}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// A fork to a spawn-only provider must be refused before Spawn.
	fork := subagent.Spec{Parent: agent.ID("r"), Mode: subagent.ModeFork, InheritThrough: 1}
	if _, err := r.Spawn(context.Background(), "spawn-only", fork); !errors.Is(err, subagent.ErrUnsupported) {
		t.Fatalf("fork err = %v, want ErrUnsupported", err)
	}
	if len(p.spawned) != 0 {
		t.Fatalf("spawn-only provider was asked to fork")
	}
}

func TestRegistrySpawnDepthAndMissing(t *testing.T) {
	r := subagent.NewRegistry()
	p := &fakeProvider{info: subagent.ProviderInfo{ID: "p", Capabilities: []subagent.Capability{subagent.CapabilitySpawn}}}
	_ = r.Register(p)
	deep := subagent.Spec{Parent: agent.ID("r"), Mode: subagent.ModeSpawn, Depth: 99}
	if _, err := r.Spawn(context.Background(), "p", deep); !errors.Is(err, subagent.ErrDepthExceeded) {
		t.Fatalf("depth err = %v", err)
	}
	if _, err := r.Spawn(context.Background(), "nope", subagent.Spec{Parent: agent.ID("r"), Mode: subagent.ModeSpawn}); !errors.Is(err, subagent.ErrProviderNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestRegistryClose(t *testing.T) {
	r := subagent.NewRegistry()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !r.Closed() {
		t.Fatal("not closed")
	}
	err := r.Register(&fakeProvider{info: subagent.ProviderInfo{ID: "x"}})
	if !errors.Is(err, subagent.ErrRegistryClosed) {
		t.Fatalf("register after close err = %v", err)
	}
}
