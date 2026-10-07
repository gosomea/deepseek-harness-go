package cordis_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

func mustPlugin(t *testing.T, ctx *cordis.Context, p cordis.Plugin, config any) *cordis.Fiber {
	t.Helper()
	f, err := ctx.Plugin(p, config)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func closeRoot(t *testing.T, ctx *cordis.Context) {
	t.Helper()
	deadline, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := ctx.Close(deadline); err != nil {
		t.Fatal(err)
	}
}

func TestConditionalAvailabilityGatesConsumers(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	var (
		ready        atomic.Bool
		panicking    atomic.Bool
		applyCount   atomic.Int32
		cleanupCount atomic.Int32
	)
	provider := mustPlugin(t, root, cordis.Plugin{Name: "provider", Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		_, err := ctx.ProvideWhen("gate", "value", func() bool {
			if panicking.Load() {
				panic("check exploded")
			}
			return ready.Load()
		})
		return nil, err
	}}, nil)
	consumer := mustPlugin(t, root, cordis.Plugin{Name: "consumer", Inject: []string{"gate"}, Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		applyCount.Add(1)
		return func() error {
			cleanupCount.Add(1)
			if ctx.GoContext().Err() == nil {
				t.Error("consumer cleanup ran without cancellation")
			}
			// The injected binding stays readable throughout cleanup.
			if _, err := cordis.Resolve(ctx, cordis.NewKey[string]("gate")); err != nil {
				t.Errorf("dependency unreadable during cleanup: %v", err)
			}
			return nil
		}, nil
	}}, nil)

	// The provider is Active, but its predicate reports the service unavailable.
	if provider.State() != cordis.Active {
		t.Fatalf("provider %v", provider.State())
	}
	if consumer.State() != cordis.Pending {
		t.Fatalf("consumer should wait for the predicate: %v", consumer.State())
	}

	ready.Store(true)
	root.Refresh()
	if consumer.State() != cordis.Active {
		t.Fatalf("consumer should activate once the predicate passes: %v", consumer.State())
	}

	// Retracting availability unloads the consumer without unloading the provider.
	ready.Store(false)
	root.Refresh()
	if provider.State() != cordis.Active {
		t.Fatalf("provider must stay active: %v", provider.State())
	}
	if consumer.State() != cordis.Pending {
		t.Fatalf("consumer should unload: %v", consumer.State())
	}

	// A throwing predicate is treated as unavailable, not as a crash.
	panicking.Store(true)
	root.Refresh()
	if consumer.State() != cordis.Pending {
		t.Fatalf("throwing predicate should keep the consumer pending: %v", consumer.State())
	}
	// Recovery needs both the predicate to stop panicking and availability to
	// genuinely hold: a panicking check must not be mistaken for success.
	panicking.Store(false)
	root.Refresh()
	if consumer.State() != cordis.Pending {
		t.Fatalf("availability is still retracted, consumer should stay pending: %v", consumer.State())
	}
	ready.Store(true)
	root.Refresh()
	if consumer.State() != cordis.Active {
		t.Fatalf("consumer should recover: %v", consumer.State())
	}

	if got := applyCount.Load(); got != 2 {
		t.Fatalf("apply count %d, want 2", got)
	}
	if got := cleanupCount.Load(); got != 1 {
		t.Fatalf("cleanup count %d, want 1", got)
	}
}

func TestUnconditionalProvideStaysAvailable(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	mustPlugin(t, root, cordis.Plugin{Name: "provider", Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		_, err := cordis.Provide(ctx, cordis.NewKey[int]("n"), 1)
		return nil, err
	}}, nil)
	root.Refresh()
	value, err := cordis.Resolve(root, cordis.NewKey[int]("n"))
	if err != nil || value != 1 {
		t.Fatalf("value %d err %v", value, err)
	}
	// Refresh must not disturb an unconditional binding.
	root.Refresh()
	if value, err := cordis.Resolve(root, cordis.NewKey[int]("n")); err != nil || value != 1 {
		t.Fatalf("after refresh value %d err %v", value, err)
	}
}

func TestDependencyLossRestartsAndKeepsCleanupAccess(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	key := cordis.NewKey[string]("model")
	var starts, stops []string
	consumer := mustPlugin(t, root, cordis.Plugin{Name: "consumer", Inject: []string{key.Name()}, Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		value, err := cordis.Resolve(ctx, key)
		if err != nil {
			return nil, err
		}
		starts = append(starts, value)
		return func() error {
			if ctx.GoContext().Err() == nil {
				t.Error("activation not canceled")
			}
			value, err := cordis.Resolve(ctx, key)
			stops = append(stops, value)
			return err
		}, nil
	}}, nil)
	if consumer.State() != cordis.Pending {
		t.Fatal(consumer.State())
	}
	provider := cordis.Plugin{Name: "provider", Apply: func(ctx *cordis.Context, config any) (cordis.Cleanup, error) {
		_, err := cordis.Provide(ctx, key, config.(string))
		return nil, err
	}}
	p := mustPlugin(t, root, provider, "v1")
	if consumer.State() != cordis.Active {
		t.Fatal(consumer.State())
	}
	if err := p.Dispose(); err != nil {
		t.Fatal(err)
	}
	if consumer.State() != cordis.Pending {
		t.Fatal(consumer.State())
	}
	if _, err := cordis.Resolve(root, key); !errors.Is(err, cordis.ErrServiceNotFound) {
		t.Fatal(err)
	}
	mustPlugin(t, root, provider, "v2")
	if err := consumer.Dispose(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(starts, []string{"v1", "v2"}) || !reflect.DeepEqual(stops, starts) {
		t.Fatalf("starts %v stops %v", starts, stops)
	}
}

func TestTransitiveConsumersUnloadBeforeProvider(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	var order []string
	makePlugin := func(name, needs, provides string) cordis.Plugin {
		p := cordis.Plugin{Name: name, Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
			if provides != "" {
				if _, err := ctx.Provide(provides, name); err != nil {
					return nil, err
				}
			}
			return func() error { order = append(order, name); return nil }, nil
		}}
		if needs != "" {
			p.Inject = []string{needs}
		}
		return p
	}
	a := mustPlugin(t, root, makePlugin("a", "", "a"), nil)
	mustPlugin(t, root, makePlugin("b", "a", "b"), nil)
	mustPlugin(t, root, makePlugin("c", "b", ""), nil)
	if err := a.Dispose(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"c", "b", "a"}) {
		t.Fatal(order)
	}
}

func TestScopesJoinWithoutLeakingOtherServices(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	_, err := root.Provide("shared", "root")
	if err != nil {
		t.Fatal(err)
	}
	_, err = root.Provide("model", "global")
	if err != nil {
		t.Fatal(err)
	}
	scope := cordis.NewScope()
	a, b := root.Isolate("model", scope), root.Isolate("model", scope)
	if _, err := a.Get("model"); !errors.Is(err, cordis.ErrServiceNotFound) {
		t.Fatal(err)
	}
	_, err = a.Provide("model", "private")
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []*cordis.Context{a, b} {
		v, err := ctx.Get("model")
		if err != nil || v != "private" {
			t.Fatal(v, err)
		}
	}
	if v, _ := root.Get("model"); v != "global" {
		t.Fatal(v)
	}
	if v, _ := a.Get("shared"); v != "root" {
		t.Fatal(v)
	}
	if _, err := b.Provide("model", "duplicate"); !errors.Is(err, cordis.ErrDuplicateService) {
		t.Fatal(err)
	}
	if a.SameScope(root, "model") || !a.SameScope(b, "model") {
		t.Fatal("scope identity")
	}
	if _, err := cordis.Resolve(root, cordis.NewKey[int]("model")); !errors.Is(err, cordis.ErrServiceType) {
		t.Fatal(err)
	}
	var pointer *int
	if _, err := root.Provide("nil", pointer); err == nil {
		t.Fatal("typed nil accepted")
	}
}

func TestFailureRollsBackAndUpdateValidatesBeforeRestart(t *testing.T) {
	root := cordis.New()
	var ctxOld *cordis.Context
	var stopped int
	startup := errors.New("startup failed")
	p := cordis.Plugin{Name: "config", Validate: func(value any) (any, error) {
		if value.(int) < 0 {
			return nil, errors.New("negative")
		}
		return value, nil
	}, Apply: func(ctx *cordis.Context, value any) (cordis.Cleanup, error) {
		ctxOld = ctx
		_, err := ctx.Provide("owned", value)
		if err != nil {
			return nil, err
		}
		_, err = ctx.OnDispose("resource", func() error { stopped++; return nil })
		if err != nil {
			return nil, err
		}
		if value.(int) == 0 {
			return nil, startup
		}
		return nil, nil
	}}
	f, err := root.Plugin(p, 0)
	if !errors.Is(err, startup) || f.State() != cordis.Failed || stopped != 1 {
		t.Fatal(err, f.State(), stopped)
	}
	if _, err := root.Get("owned"); !errors.Is(err, cordis.ErrServiceNotFound) {
		t.Fatal(err)
	}
	if err := f.Update(1); err != nil {
		t.Fatal(err)
	}
	old := ctxOld
	if err := f.Update(-1); err == nil || f.State() != cordis.Active || stopped != 1 {
		t.Fatal(err, f.State(), stopped)
	}
	if err := f.Update(2); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Provide("stale", "bad"); !errors.Is(err, cordis.ErrInactive) {
		t.Fatal(err)
	}
	if err := f.Restart(); err != nil {
		t.Fatal(err)
	}
	closeRoot(t, root)
	if stopped != 4 {
		t.Fatal(stopped)
	}
	if err := f.Restart(); !errors.Is(err, cordis.ErrInactive) {
		t.Fatal(err)
	}
}

func TestChildrenDisposedAndCleanupContinuesAfterPanic(t *testing.T) {
	root := cordis.New()
	var child *cordis.Fiber
	var order []int
	f := mustPlugin(t, root, cordis.Plugin{Name: "parent", Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		var err error
		child, err = ctx.Plugin(cordis.Plugin{Name: "child", Apply: func(c *cordis.Context, _ any) (cordis.Cleanup, error) {
			return func() error { order = append(order, 3); return nil }, nil
		}}, nil)
		if err != nil {
			return nil, err
		}
		_, err = ctx.OnDispose("first", func() error { order = append(order, 1); return nil })
		if err != nil {
			return nil, err
		}
		_, err = ctx.OnDispose("panic", func() error { order = append(order, 2); panic("cleanup") })
		return nil, err
	}}, nil)
	if err := f.Dispose(); err == nil {
		t.Fatal("missing cleanup error")
	}
	if child.State() != cordis.Disposed || !reflect.DeepEqual(order, []int{3, 2, 1}) {
		t.Fatal(child.State(), order)
	}
	if err := root.Close(context.Background()); err == nil {
		t.Fatal("cleanup error not reported")
	}
	if len(root.Fibers()) != 0 {
		t.Fatal(root.Fibers())
	}
}

func TestManualDisposerJoinedAndReentrantMutation(t *testing.T) {
	root := cordis.New()
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var dispose cordis.Cleanup
	mustPlugin(t, root, cordis.Plugin{Name: "manual", Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		var err error
		dispose, err = ctx.OnDispose("slow", func() error {
			close(entered)
			_, err := root.Provide("during-cleanup", 1)
			<-release
			return err
		})
		return nil, err
	}}, nil)
	go func() { _ = dispose(); close(finished) }()
	<-entered
	deadline, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := root.Wait(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(release)
	<-finished
	if err := dispose(); err != nil {
		t.Fatal(err)
	}
	closeRoot(t, root)
}

func TestConcurrentPluginRegistrationAndCloseCancelsStartup(t *testing.T) {
	root := cordis.New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := root.Plugin(cordis.Plugin{Name: "parallel-register", Apply: func(_ *cordis.Context, _ any) (cordis.Cleanup, error) { return nil, nil }}, nil)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := root.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(root.Fibers()) != 20 {
		t.Fatal(len(root.Fibers()))
	}
	entered, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = root.Plugin(cordis.Plugin{Name: "blocked", Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
			close(entered)
			<-ctx.GoContext().Done()
			return nil, nil
		}}, nil)
	}()
	<-entered
	closeRoot(t, root)
	<-done
	if root.Fiber().State() != cordis.Disposed {
		t.Fatal(root.Fiber().State())
	}
	if _, err := root.Plugin(cordis.Plugin{Name: "late", Apply: func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, nil }}, nil); !errors.Is(err, cordis.ErrInactive) {
		t.Fatal(err)
	}
}

func TestSelfDisposalDuringStartupOwnsReturnedCleanup(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	var cleanup int
	f := mustPlugin(t, root, cordis.Plugin{Name: "self", Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		_ = ctx.Fiber().Dispose()
		return func() error { cleanup++; return nil }, nil
	}}, nil)
	if f.State() != cordis.Disposed || cleanup != 1 {
		t.Fatal(f.State(), cleanup)
	}
}
