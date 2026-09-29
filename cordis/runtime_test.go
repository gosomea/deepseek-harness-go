package cordis_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

func TestProviderRestartReactivatesConsumers(t *testing.T) {
	root := cordis.New(cordis.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer closeRoot(t, root)
	var starts int
	consumer, err := root.Inject([]string{"model"}, func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		starts++
		ctx.Logger().Info("activated")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := mustPlugin(t, root, cordis.Plugin{Name: "model", Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		_, err := ctx.Provide("model", starts+1)
		return nil, err
	}}, nil)
	if consumer.ID() == 0 || consumer.Name() != "inject" {
		t.Fatal(consumer.ID(), consumer.Name())
	}
	if err := provider.Restart(); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts != 2 {
		t.Fatal(starts)
	}
	if err := root.Fiber().Restart(); err == nil {
		t.Fatal("root restarted")
	}
}

func TestUpdateDuringLoadingUsesLatestConfig(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	entered := make(chan *cordis.Fiber, 1)
	release, done := make(chan struct{}), make(chan struct{})
	var got string
	go func() {
		defer close(done)
		_, _ = root.Plugin(cordis.Plugin{Name: "update", Apply: func(ctx *cordis.Context, value any) (cordis.Cleanup, error) {
			if value == "old" {
				entered <- ctx.Fiber()
				<-release
				return nil, errors.New("obsolete startup failed")
			}
			got = value.(string)
			return nil, nil
		}}, "old")
	}()
	f := <-entered
	if err := f.Update("new"); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if err := f.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "new" || f.State() != cordis.Active {
		t.Fatal(got, f.State())
	}
}

func TestCloseDeadlineBoundsWaitForUncooperativeCleanup(t *testing.T) {
	root := cordis.New()
	entered, release := make(chan struct{}), make(chan struct{})
	mustPlugin(t, root, cordis.Plugin{Name: "slow", Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
		return func() error { close(entered); <-release; return nil }, nil
	}}, nil)
	deadline, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := root.Close(deadline)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-entered
	close(release)
	if err := root.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if root.Fiber().State() != cordis.Disposed {
		t.Fatal(root.Fiber().State())
	}
}

func TestInvalidPluginAndCanceledContextRejectWork(t *testing.T) {
	root := cordis.New(cordis.WithLogger(nil))
	defer closeRoot(t, root)
	if _, err := root.Plugin(cordis.Plugin{}, nil); err == nil {
		t.Fatal("invalid plugin accepted")
	}
	if _, err := root.Plugin(cordis.Plugin{Name: "empty-dep", Inject: []string{""}, Apply: func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, nil }}, nil); err == nil {
		t.Fatal("empty dependency accepted")
	}
	if _, err := root.OnDispose("nil", nil); err == nil {
		t.Fatal("nil cleanup accepted")
	}
	if _, err := root.On("", nil, cordis.EventOptions{}); err == nil {
		t.Fatal("invalid listener accepted")
	}
	failure := errors.New("startup")
	f, err := root.Plugin(cordis.Plugin{Name: "failed", Apply: func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, failure }}, nil)
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := root.Wait(context.Background()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	_ = f.Dispose()
	closeRoot(t, root)
	if _, err := root.OnDispose("late", func() error { return nil }); !errors.Is(err, cordis.ErrInactive) {
		t.Fatal(err)
	}
	if _, err := root.On("late", func(cordis.Event, cordis.Next) (any, error) { return nil, nil }, cordis.EventOptions{}); !errors.Is(err, cordis.ErrInactive) {
		t.Fatal(err)
	}
	if err := root.Events().Parallel("late"); !errors.Is(err, cordis.ErrInactive) {
		t.Fatal(err)
	}
	if _, err := root.Events().Waterfall("late", nil); err == nil {
		t.Fatal("nil waterfall body")
	}
	if _, err := root.Events().Waterfall("late", func() (any, error) { return nil, nil }); !errors.Is(err, cordis.ErrInactive) {
		t.Fatal(err)
	}
}
