package cordis

import (
	"context"
	"errors"
)

// State identifies a plugin instance lifecycle phase.
type State string

const (
	// Pending waits for the parent or required services.
	Pending State = "pending"
	// Loading means Apply is executing.
	Loading State = "loading"
	// Active publishes services and accepts registrations.
	Active State = "active"
	// Failed means startup rollback completed; Update or Restart retries.
	Failed State = "failed"
	// Unloading means cancellation occurred and cleanup is pending or running.
	Unloading State = "unloading"
	// Disposed means the instance is permanently removed.
	Disposed State = "disposed"
)

// Fiber is one registered plugin instance, potentially with many activations.
type Fiber struct {
	rt             *runtime
	id             uint64
	plugin         Plugin
	parent         *Context
	ctx            *Context
	state          State
	config         any
	epoch          uint64
	revision       uint64
	loadedRevision uint64
	disposed       bool
	err            error
	dependencies   []*binding
	effects        []*effect
	cancel         context.CancelFunc
}

// ID is stable across restarts and unique within a root; root has ID zero.
func (f *Fiber) ID() uint64 { return f.id }

// Name returns the plugin diagnostic label.
func (f *Fiber) Name() string { return f.plugin.Name }

// State returns a synchronized lifecycle state snapshot.
func (f *Fiber) State() State { f.rt.mu.Lock(); defer f.rt.mu.Unlock(); return f.state }

// Err returns the current startup or final disposal error without waiting.
func (f *Fiber) Err() error { f.rt.mu.Lock(); defer f.rt.mu.Unlock(); return f.err }

// Wait joins root lifecycle work and includes this Fiber retained error.
// Pending dependencies are settled; Wait does not wait for a provider.
func (f *Fiber) Wait(ctx context.Context) error {
	err := f.rt.root.Wait(ctx)
	return errors.Join(err, f.Err())
}

// Dispose permanently removes this instance and its descendants. Inside a
// lifecycle callback it schedules disposal, which settles after the callback.
func (f *Fiber) Dispose() error {
	r := f.rt
	r.mu.Lock()
	r.disposeTreeLocked(f)
	r.beginBusyLocked()
	r.mu.Unlock()
	r.reconcile()
	return f.Err()
}

func (r *runtime) disposeTreeLocked(f *Fiber) {
	f.disposed = true
	if f.cancel != nil {
		f.cancel()
	}
	for _, child := range r.fibers {
		if child.parent.fiber == f && !child.disposed {
			r.disposeTreeLocked(child)
		}
	}
}

// Update validates before changing the running instance. Invalid config leaves
// the current activation untouched; valid config requests a fresh activation.
func (f *Fiber) Update(config any) error {
	value, err := f.plugin.validate(config)
	if err != nil {
		return err
	}
	f.rt.mu.Lock()
	if f.disposed {
		f.rt.mu.Unlock()
		return ErrInactive
	}
	f.config, f.err = value, nil
	if f.state == Failed {
		f.state = Pending
	}
	f.revision++
	f.rt.beginBusyLocked()
	f.rt.mu.Unlock()
	f.rt.reconcile()
	return f.Err()
}

// Restart requests a fresh activation with the current validated configuration.
func (f *Fiber) Restart() error {
	if f.parent == nil {
		return errors.New("cordis: root cannot restart; create a new root")
	}
	f.rt.mu.Lock()
	if f.disposed {
		f.rt.mu.Unlock()
		return ErrInactive
	}
	f.err = nil
	if f.state == Failed {
		f.state = Pending
	}
	f.revision++
	f.rt.beginBusyLocked()
	f.rt.mu.Unlock()
	f.rt.reconcile()
	return f.Err()
}
