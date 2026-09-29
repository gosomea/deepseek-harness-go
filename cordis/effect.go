package cordis

import "fmt"

type effect struct {
	owner   *Fiber
	label   string
	cleanup Cleanup
	started bool
	done    chan struct{}
	err     error
}

func (c *Context) addEffectLocked(label string, cleanup Cleanup) *effect {
	e := &effect{owner: c.fiber, label: label, cleanup: cleanup, done: make(chan struct{})}
	c.fiber.effects = append(c.fiber.effects, e)
	return e
}

// OnDispose registers a cleanup in reverse registration order. The returned
// disposer is single-shot; the owner still joins manually started cleanup.
func (c *Context) OnDispose(label string, cleanup Cleanup) (Cleanup, error) {
	if cleanup == nil {
		return nil, fmt.Errorf("cordis: nil cleanup")
	}
	c.rt.mu.Lock()
	defer c.rt.mu.Unlock()
	if !c.writableLocked() {
		return nil, ErrInactive
	}
	return c.addEffectLocked(label, cleanup).dispose, nil
}

func (e *effect) dispose() error { return e.run(false) }

func (e *effect) run(join bool) error {
	r := e.owner.rt
	r.mu.Lock()
	if e.started {
		done := e.done
		r.mu.Unlock()
		if join {
			<-done
		}
		r.mu.Lock()
		err := e.err
		r.mu.Unlock()
		return err
	}
	e.started = true
	r.beginBusyLocked()
	r.callbacks++
	r.mu.Unlock()
	err := safeCleanup(e.cleanup)
	if err != nil {
		err = fmt.Errorf("cleanup %s/%s: %w", e.owner.plugin.Name, e.label, err)
	}
	r.mu.Lock()
	e.err = err
	if err != nil {
		r.cleanupErrors = append(r.cleanupErrors, err)
	}
	close(e.done)
	for i, item := range e.owner.effects {
		if item == e {
			copy(e.owner.effects[i:], e.owner.effects[i+1:])
			e.owner.effects[len(e.owner.effects)-1] = nil
			e.owner.effects = e.owner.effects[:len(e.owner.effects)-1]
			break
		}
	}
	r.callbacks--
	r.mu.Unlock()
	r.reconcile()
	return err
}
