package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/agent"
)

func TestAssertOwnerChecksBothSides(t *testing.T) {
	if err := agent.AssertOwner(newStubOwner("a", "s")); err != nil {
		t.Fatalf("a value implementing both must pass: %v", err)
	}
	if err := agent.AssertOwner(nil); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for nil, got %v", err)
	}
	if err := agent.AssertOwner(struct{}{}); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a value implementing neither, got %v", err)
	}
	if err := agent.AssertOwner(handleOnly{}); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a handle without an owner, got %v", err)
	}
	if err := agent.AssertOwner(ownerOnly{}); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an owner without a handle, got %v", err)
	}
}

type handleOnly struct{}

func (handleOnly) Info() agent.Info                        { return agent.Info{} }
func (handleOnly) State() agent.State                      { return agent.StateIdle }
func (handleOnly) Send(context.Context, agent.Input) error { return nil }
func (handleOnly) Wait(context.Context) error              { return nil }
func (handleOnly) Done() <-chan struct{}                   { return nil }

type ownerOnly struct{}

func (ownerOnly) Dispose(context.Context) error { return nil }
func (ownerOnly) Handle() agent.Handle          { return nil }

func TestWaitAllJoinsErrorsAndStopsOnCancellation(t *testing.T) {
	first := newStubOwner("a", "s")
	second := newStubOwner("b", "s")
	if err := agent.WaitAll(context.Background(), first, second); err != nil {
		t.Fatalf("settled handles must wait cleanly: %v", err)
	}
	if err := agent.WaitAll(context.Background(), nil); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a nil handle, got %v", err)
	}
	canceled := &cancelingHandle{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := agent.WaitAll(ctx, canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

type cancelingHandle struct{}

func (cancelingHandle) Info() agent.Info                        { return agent.Info{} }
func (cancelingHandle) State() agent.State                      { return agent.StateRunning }
func (cancelingHandle) Send(context.Context, agent.Input) error { return nil }
func (cancelingHandle) Wait(ctx context.Context) error          { return ctx.Err() }
func (cancelingHandle) Done() <-chan struct{}                   { return nil }
