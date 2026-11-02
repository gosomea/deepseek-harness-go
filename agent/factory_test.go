package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/agent"
)

// stubOwner is a minimal Owner+Handle pair used to exercise the registry
// without depending on the default driver.
type stubOwner struct {
	info     agent.Info
	state    agent.State
	disposed bool
	inputs   []agent.Input
	done     chan struct{}
}

func newStubOwner(id, scope string) *stubOwner {
	return &stubOwner{
		info:  agent.Info{ID: agent.ID(id), Scope: agent.ScopeKey(scope)},
		state: agent.StateIdle,
		done:  make(chan struct{}),
	}
}

func (s *stubOwner) Info() agent.Info   { return s.info }
func (s *stubOwner) State() agent.State { return s.state }
func (s *stubOwner) Send(ctx context.Context, input agent.Input) error {
	s.inputs = append(s.inputs, input)
	return nil
}
func (s *stubOwner) Wait(ctx context.Context) error { return nil }
func (s *stubOwner) Done() <-chan struct{}          { return s.done }
func (s *stubOwner) Handle() agent.Handle           { return s }
func (s *stubOwner) Dispose(ctx context.Context) error {
	if s.disposed {
		return nil
	}
	s.disposed = true
	s.state = agent.StateDisposed
	close(s.done)
	return nil
}

// stubFactory records what it created.
type stubFactory struct {
	created  []*stubOwner
	createFn func(agent.Spec) (agent.Owner, error)
	closed   bool
}

func (f *stubFactory) Create(ctx context.Context, spec agent.Spec) (agent.Owner, error) {
	if f.createFn != nil {
		return f.createFn(spec)
	}
	owner := newStubOwner("a1", spec.Scope.String())
	f.created = append(f.created, owner)
	return owner, nil
}

func (f *stubFactory) Active() []agent.Handle {
	handles := make([]agent.Handle, 0, len(f.created))
	for _, owner := range f.created {
		handles = append(handles, owner)
	}
	return handles
}

func (f *stubFactory) DisposeAll(ctx context.Context) error {
	f.closed = true
	var errs []error
	for _, owner := range f.created {
		if err := owner.Dispose(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func validSpec() agent.Spec {
	return agent.Spec{Scope: "main", SessionID: "session-1"}
}

func TestSpecValidateRequiresScopeAndSession(t *testing.T) {
	if err := (agent.Spec{SessionID: "s"}).Validate(); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a scope, got %v", err)
	}
	if err := (agent.Spec{Scope: "main"}).Validate(); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a session id, got %v", err)
	}
	input := agent.Input{}
	spec := validSpec()
	spec.InitialInput = &input
	if err := spec.Validate(); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank initial input, got %v", err)
	}
	if err := validSpec().Validate(); err != nil {
		t.Fatalf("a complete spec must validate: %v", err)
	}
}

func TestInfoValidateRequiresIDAndScope(t *testing.T) {
	if err := (agent.Info{Scope: "s"}).Validate(); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an id, got %v", err)
	}
	if err := (agent.Info{ID: "a"}).Validate(); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a scope, got %v", err)
	}
	if err := (agent.Info{ID: "a", Scope: "s"}).Validate(); err != nil {
		t.Fatalf("a complete info must validate: %v", err)
	}
}

func TestInputValidateRejectsBlankText(t *testing.T) {
	if err := (agent.Input{Text: "   "}).Validate(); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for blank text, got %v", err)
	}
	if err := (agent.Input{Text: "hi"}).Validate(); err != nil {
		t.Fatalf("a non-empty input must validate: %v", err)
	}
}

func TestStateValidAcceptsEveryDefinedState(t *testing.T) {
	for _, state := range []agent.State{
		agent.StatePending, agent.StateRunning, agent.StateIdle, agent.StateStopping, agent.StateDisposed,
	} {
		if !state.Valid() {
			t.Fatalf("%s must be valid", state)
		}
	}
	if agent.State("invented").Valid() {
		t.Fatal("an undefined state must not validate")
	}
}

// A registry holds one driver: two competing factories would make "which driver
// ran this turn" unattributable.
func TestRegistryInstallsExactlyOneFactory(t *testing.T) {
	registry := agent.NewRegistry()
	if _, ok := registry.Factory(); ok {
		t.Fatal("a fresh registry has no factory")
	}
	first := &stubFactory{}
	if err := registry.Install(first); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := registry.Install(&stubFactory{}); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a second factory, got %v", err)
	}
	if err := registry.Install(nil); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a nil factory, got %v", err)
	}
	if installed, ok := registry.Factory(); !ok || installed != first {
		t.Fatal("the installed factory must be returned")
	}
}

// An absent factory is a configuration error, not an empty success.
func TestRegistryCreateWithoutAFactoryIsRefused(t *testing.T) {
	registry := agent.NewRegistry()
	if _, err := registry.Create(context.Background(), validSpec()); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}
}

func TestRegistryCloseDisposesInstancesAndRefusesLaterUse(t *testing.T) {
	registry := agent.NewRegistry()
	factory := &stubFactory{}
	if err := registry.Install(factory); err != nil {
		t.Fatalf("install: %v", err)
	}
	owner, err := registry.Create(context.Background(), validSpec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	stub, ok := owner.(*stubOwner)
	if !ok {
		t.Fatalf("unexpected owner type %T", owner)
	}
	if err := registry.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !stub.disposed {
		t.Fatal("closing the registry must dispose the instances it owns")
	}
	if !registry.Closed() {
		t.Fatal("the registry must report closed")
	}
	if err := registry.Close(context.Background()); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
	if _, err := registry.Create(context.Background(), validSpec()); !errors.Is(err, agent.ErrClosed) {
		t.Fatalf("want ErrClosed after close, got %v", err)
	}
	if err := registry.Install(&stubFactory{}); !errors.Is(err, agent.ErrClosed) {
		t.Fatalf("want ErrClosed when installing after close, got %v", err)
	}
}

// A factory that fails creation must not publish a half-created instance.
func TestFactoryFailurePublishesNothing(t *testing.T) {
	registry := agent.NewRegistry()
	boom := errors.New("setup failed")
	factory := &stubFactory{createFn: func(agent.Spec) (agent.Owner, error) { return nil, boom }}
	if err := registry.Install(factory); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := registry.Create(context.Background(), validSpec()); !errors.Is(err, boom) {
		t.Fatalf("want the creation failure, got %v", err)
	}
	if got := len(factory.Active()); got != 0 {
		t.Fatalf("nothing must be published after a failed creation, got %d", got)
	}
}
