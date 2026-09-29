package cordis

import (
	"context"
	"errors"
	"fmt"
)

// reconcile is a single lifecycle driver. Reentrant calls add work for this
// driver instead of holding a lock while application callbacks execute.
func (r *runtime) reconcile() {
	r.mu.Lock()
	if r.pumping || r.callbacks > 0 {
		r.mu.Unlock()
		return
	}
	r.pumping = true
	r.beginBusyLocked()
	for {
		f, load := r.nextLocked()
		if f == nil {
			r.pumping = false
			if r.callbacks == 0 {
				r.busy = false
				close(r.idle)
			}
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
		if load {
			r.load(f)
		} else {
			r.unload(f)
		}
		r.mu.Lock()
	}
}

func (r *runtime) beginBusyLocked() {
	if r.busy {
		return
	}
	r.busy = true
	r.idle = make(chan struct{})
}

func (r *runtime) dependenciesLocked(f *Fiber) ([]*binding, bool) {
	if f.parent != nil && !f.parent.writableLocked() {
		return nil, false
	}
	deps := make([]*binding, 0, len(f.plugin.Inject))
	for _, name := range f.plugin.Inject {
		b := r.services[f.parent.key(name)]
		if !available(b) {
			return nil, false
		}
		deps = append(deps, b)
	}
	return deps, true
}

func sameBindings(a, b []*binding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// nextLocked first marks every invalid activation Unloading. This makes
// dependency invalidation propagate before any provider resource is destroyed.
func (r *runtime) nextLocked() (*Fiber, bool) {
	all := append(append([]*Fiber(nil), r.fibers...), r.root.fiber)
	for changed := true; changed; {
		changed = false
		for _, f := range all {
			if f.state != Active {
				continue
			}
			deps, ready := r.dependenciesLocked(f)
			if !f.disposed && f.err == nil && ready && f.revision == f.loadedRevision && sameBindings(deps, f.dependencies) {
				continue
			}
			f.state = Unloading
			f.cancel()
			for _, child := range r.fibers {
				if child.parent.fiber == f {
					r.disposeTreeLocked(child)
				}
			}
			changed = true
		}
	}
	for i := len(all) - 1; i >= 0; i-- {
		f := all[i]
		if f.state == Unloading && !r.hasLiveConsumerLocked(f, all) {
			return f, false
		}
	}
	for _, f := range all {
		if f.disposed && f.state != Disposed && f.state != Unloading {
			f.state = Disposed
		}
	}
	r.compactLocked()
	for _, f := range r.fibers {
		if f.state != Pending || f.disposed {
			continue
		}
		deps, ready := r.dependenciesLocked(f)
		if !ready {
			continue
		}
		f.state = Loading
		f.dependencies = deps
		return f, true
	}
	return nil, false
}

func (r *runtime) hasLiveConsumerLocked(provider *Fiber, all []*Fiber) bool {
	for _, consumer := range all {
		if consumer == provider || (consumer.state != Active && consumer.state != Unloading) {
			continue
		}
		if consumer.parent != nil && consumer.parent.fiber == provider {
			return true
		}
		for _, b := range consumer.dependencies {
			if b.owner == provider {
				return true
			}
		}
	}
	return false
}

func (r *runtime) compactLocked() {
	live := r.fibers[:0]
	for _, f := range r.fibers {
		if f.state != Disposed {
			live = append(live, f)
		}
	}
	clear(r.fibers[len(live):])
	r.fibers = live
}

func (r *runtime) load(f *Fiber) {
	r.mu.Lock()
	f.epoch++
	signal, cancel := context.WithCancel(f.parent.signal)
	f.cancel = cancel
	c := &Context{rt: r, fiber: f, epoch: f.epoch, scopes: f.parent.scopes, signal: signal}
	f.ctx = c
	f.loadedRevision = f.revision
	config := f.config
	r.mu.Unlock()
	var cleanup Cleanup
	_, err := safeValue(func() (any, error) {
		var applyErr error
		cleanup, applyErr = f.plugin.Apply(c, config)
		return nil, applyErr
	})
	r.mu.Lock()
	// A returned cleanup is owned even when startup fails or disposes itself.
	if cleanup != nil {
		c.addEffectLocked("apply", cleanup)
	}
	if err != nil && f.loadedRevision == f.revision {
		f.err = fmt.Errorf("start %s: %w", f.plugin.Name, err)
	}
	f.state = Active
	r.mu.Unlock()
}

func (r *runtime) unload(f *Fiber) {
	r.mu.Lock()
	effects := f.effects
	f.effects = nil
	r.mu.Unlock()
	var errs []error
	for i := len(effects) - 1; i >= 0; i-- {
		if err := effects[i].run(true); err != nil {
			errs = append(errs, err)
		}
	}
	r.mu.Lock()
	f.dependencies = nil
	if f.disposed {
		f.state = Disposed
		f.err = errors.Join(append([]error{f.err}, errs...)...)
	} else if f.err != nil {
		f.state = Failed
	} else {
		f.state = Pending
	}
	r.mu.Unlock()
}
