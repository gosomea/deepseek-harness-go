package cordis_test

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

func on(t *testing.T, c *cordis.Context, name string, fn cordis.Listener, opts cordis.EventOptions) cordis.Cleanup {
	t.Helper()
	dispose, err := c.On(name, fn, opts)
	if err != nil {
		t.Fatal(err)
	}
	return dispose
}

func TestEventOrderBailValuesAndOwnedRemoval(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	var order []int
	on(t, root, "order", func(cordis.Event, cordis.Next) (any, error) { order = append(order, 1); return nil, nil }, cordis.EventOptions{})
	on(t, root, "order", func(cordis.Event, cordis.Next) (any, error) { order = append(order, 0); return nil, nil }, cordis.EventOptions{Prepend: true, Once: true})
	if err := root.Events().Emit("order"); err != nil {
		t.Fatal(err)
	}
	if err := root.Events().Emit("order"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []int{0, 1, 1}) {
		t.Fatal(order)
	}
	for _, value := range []any{nil, false, 0, "", true} {
		t.Run("bail", func(t *testing.T) {
			var tail int
			d1 := on(t, root, "bail", func(cordis.Event, cordis.Next) (any, error) { return value, nil }, cordis.EventOptions{})
			d2 := on(t, root, "bail", func(cordis.Event, cordis.Next) (any, error) { tail++; return "tail", nil }, cordis.EventOptions{})
			_, err := root.Events().Bail("bail")
			if err != nil {
				t.Fatal(err)
			}
			if (tail == 0) != cordis.IsBailed(value) {
				t.Fatal(value, tail)
			}
			_ = d1()
			_ = d2()
		})
	}
	var calls int
	f := mustPlugin(t, root, cordis.Plugin{Name: "listener", Apply: func(c *cordis.Context, _ any) (cordis.Cleanup, error) {
		_, err := c.On("owned", func(cordis.Event, cordis.Next) (any, error) { calls++; return nil, nil }, cordis.EventOptions{})
		return nil, err
	}}, nil)
	_ = root.Events().Emit("owned")
	_ = f.Dispose()
	_ = root.Events().Emit("owned")
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestWaterfallDelegationAndVeto(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	var order []string
	on(t, root, "wrap", func(e cordis.Event, next cordis.Next) (any, error) {
		if e.Args[0] != "payload" {
			t.Fatal(e.Args)
		}
		order = append(order, "before")
		v, err := next()
		order = append(order, "after")
		return v, err
	}, cordis.EventOptions{})
	on(t, root, "wrap", func(_ cordis.Event, next cordis.Next) (any, error) { order = append(order, "middle"); return next() }, cordis.EventOptions{})
	value, err := root.Events().Waterfall("wrap", func() (any, error) { order = append(order, "inner"); return 42, nil }, "payload")
	if err != nil || value != 42 || !reflect.DeepEqual(order, []string{"before", "middle", "inner", "after"}) {
		t.Fatal(value, err, order)
	}
	on(t, root, "veto", func(cordis.Event, cordis.Next) (any, error) { return "denied", nil }, cordis.EventOptions{})
	value, err = root.Events().Waterfall("veto", func() (any, error) { t.Fatal("veto failed"); return nil, nil })
	if err != nil || value != "denied" {
		t.Fatal(value, err)
	}
}

func TestWaterfallRepeatedNextAdvancesAndSkipsRemovedListener(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	var remove cordis.Cleanup
	var middle, inner int
	on(t, root, "next", func(_ cordis.Event, next cordis.Next) (any, error) {
		_ = remove()
		if _, err := next(); err != nil {
			return nil, err
		}
		return next()
	}, cordis.EventOptions{})
	remove = on(t, root, "next", func(cordis.Event, cordis.Next) (any, error) {
		t.Fatal("removed waterfall listener called")
		return nil, nil
	}, cordis.EventOptions{})
	on(t, root, "next", func(_ cordis.Event, next cordis.Next) (any, error) {
		middle++
		return next()
	}, cordis.EventOptions{})
	_, err := root.Events().Waterfall("next", func() (any, error) { inner++; return nil, nil })
	if err != nil || middle != 1 || inner != 2 {
		t.Fatal(err, middle, inner)
	}
}

func TestParallelJoinsAllErrorsAndOnceIsAtomic(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	err1, err2 := errors.New("one"), errors.New("two")
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	for _, err := range []error{err1, err2} {
		err := err
		on(t, root, "parallel", func(cordis.Event, cordis.Next) (any, error) { entered <- struct{}{}; <-release; return nil, err }, cordis.EventOptions{})
	}
	done := make(chan error, 1)
	go func() { done <- root.Events().Parallel("parallel") }()
	<-entered
	<-entered
	close(release)
	err := <-done
	if !errors.Is(err, err1) || !errors.Is(err, err2) {
		t.Fatal(err)
	}
	var count atomic.Int32
	on(t, root, "once", func(cordis.Event, cordis.Next) (any, error) { count.Add(1); return nil, nil }, cordis.EventOptions{Once: true})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = root.Events().Emit("once") }()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal(count.Load())
	}
}

func TestFilterIsExplicitAndGlobalBypassesIt(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	a, b := root.Isolate("model", nil), root.Isolate("model", nil)
	var count [3]int
	for i, c := range []*cordis.Context{a, b, root} {
		i := i
		on(t, c, "scope", func(cordis.Event, cordis.Next) (any, error) { count[i]++; return nil, nil }, cordis.EventOptions{Global: i == 2})
	}
	if err := a.Events().Emit("scope"); err != nil {
		t.Fatal(err)
	}
	if count != [3]int{1, 1, 1} {
		t.Fatal(count)
	}
	if err := a.Filter(func(listener *cordis.Context) bool { return a.SameScope(listener, "model") }).Emit("scope"); err != nil {
		t.Fatal(err)
	}
	if count != [3]int{2, 1, 2} {
		t.Fatal(count)
	}
}

func TestDispatchSkipsRemovedListenersAndRecoversPanics(t *testing.T) {
	root := cordis.New()
	defer closeRoot(t, root)
	var remove cordis.Cleanup
	on(t, root, "remove", func(cordis.Event, cordis.Next) (any, error) { _ = remove(); return nil, nil }, cordis.EventOptions{})
	remove = on(t, root, "remove", func(cordis.Event, cordis.Next) (any, error) { t.Fatal("removed listener called"); return nil, nil }, cordis.EventOptions{})
	if err := root.Events().Emit("remove"); err != nil {
		t.Fatal(err)
	}
	on(t, root, "panic", func(cordis.Event, cordis.Next) (any, error) { panic("event") }, cordis.EventOptions{})
	if err := root.Events().Emit("panic"); err == nil {
		t.Fatal("panic not reported")
	}
	if _, err := root.Events().Serial("panic"); err == nil {
		t.Fatal("serial panic not reported")
	}
}
