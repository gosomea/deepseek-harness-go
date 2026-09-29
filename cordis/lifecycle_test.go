package cordis_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
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
