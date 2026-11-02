package agent

import (
	"context"
	"errors"
	"fmt"
)

// Handle is what a consumer holds to interact with one Agent.
//
// It is an interface, and a narrow one, so that a consumer can depend on the
// ability to send input, observe state and wait for settlement without depending
// on how any of those are implemented. It deliberately has no disposal method:
// see Owner.
type Handle interface {
	// Info describes this Agent.
	Info() Info
	// State reports the current lifecycle state.
	State() State
	// Send delivers input to the Agent's inbox. The error is non-nil when the
	// input is rejected, for example because the Agent is disposed.
	Send(ctx context.Context, input Input) error
	// Wait blocks until the Agent has no work left, or until ctx is done. A
	// disposed Agent returns immediately.
	Wait(ctx context.Context) error
	// Done returns a channel closed when the Agent is disposed. It lets a caller
	// select on disposal without polling.
	Done() <-chan struct{}
}

// Owner is the capability to dispose an Agent.
//
// It is separate from Handle on purpose: a component that can *find* an Agent —
// through a registry, a parent's child list or a diagnostic query — must not
// therefore be able to destroy it. Only whoever created the instance, or the
// factory that owns it, receives an Owner.
type Owner interface {
	// Dispose stops the Agent and releases its resources. It is idempotent.
	Dispose(ctx context.Context) error
	// Handle returns the read/send handle for this owned instance.
	Handle() Handle
}

// Canceler requests cancellation of an Agent's active work without disposing it.
//
// Cancellation and disposal are different operations: cancelling a turn leaves
// the Agent alive and its inbox intact, while disposing ends its life. Keeping
// them on separate interfaces means a caller that only wants to interrupt work
// cannot accidentally destroy the instance.
type Canceler interface {
	// Cancel requests cancellation, reporting whether there was active work to
	// cancel. It is safe to call when the Agent is idle.
	Cancel(ctx context.Context) (bool, error)
}

// OwnerHandle is the common case: an Owner that also exposes the read/send
// handle directly. Implementations may satisfy both without this interface, but
// naming it lets a constructor assert the pairing it promises.
type OwnerHandle interface {
	Owner
	Handle
}

// AssertOwner validates the pairing a constructor promises: an implementation
// that returns an Owner must also be usable as a Handle, and vice versa.
//
// It exists so a construction mistake fails at the boundary that made the
// promise instead of at a later call site that happens to need the other side.
func AssertOwner(value any) error {
	if value == nil {
		return fmt.Errorf("%w: owner is nil", ErrInvalidValue)
	}
	if _, ok := value.(Owner); !ok {
		return fmt.Errorf("%w: value does not implement Owner", ErrInvalidValue)
	}
	if _, ok := value.(Handle); !ok {
		return fmt.Errorf("%w: value does not implement Handle", ErrInvalidValue)
	}
	return nil
}

// WaitAll waits for every handle to settle, returning the joined errors.
//
// It stops waiting early when ctx is done and reports ctx's error alongside the
// handles that had already settled, so a caller can tell "one Agent failed" from
// "the wait itself was canceled".
func WaitAll(ctx context.Context, handles ...Handle) error {
	var errs []error
	for _, handle := range handles {
		if handle == nil {
			errs = append(errs, fmt.Errorf("%w: nil handle", ErrInvalidValue))
			continue
		}
		if err := handle.Wait(ctx); err != nil {
			errs = append(errs, err)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
		}
	}
	return errors.Join(errs...)
}
