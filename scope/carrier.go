package scope

import (
	"errors"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// Carrier routes a scoped event dispatch to the listeners that may observe it.
//
// The reference expresses this with a compile-time brand so that dispatching with
// a bare subject is a type error. Go has no equivalent, so this type carries the
// routing state and IsCarrier/CarrierKey provide the runtime test. A wrong
// receiver is therefore a runtime mistake rather than a compile error; See
// Carrier.Admits for the rule that is enforced.
type Carrier struct {
	// key is the routing key. A nil key is an unkeyed carrier: it admits only
	// untagged listeners, which is how a subject-less dispatch is expressed.
	key Key
}

// NewCarrier builds the carrier a scope-filtered event dispatches with. A nil key
// produces an unkeyed carrier.
func NewCarrier(key Key) *Carrier {
	carrier := &Carrier{key: key}
	carriers[carrier] = key
	return carrier
}

// Key returns the routing key, or nil for an unkeyed carrier.
func (c *Carrier) Key() Key {
	if c == nil {
		return nil
	}
	return c.key
}

// Admit reports whether a listener tagged with listener may observe a dispatch
// routed by this carrier.
//
// An untagged listener is always admitted, because it is a global observer. A
// tagged listener is admitted only when its tag is the dispatch key or an
// ancestor of it: events flow up the chain, never down, so a listener belonging
// to one sibling never hears another sibling's dispatch.
func (c *Carrier) Admit(listener Key) bool {
	if c == nil {
		return listener == nil
	}
	if listener == nil {
		return true
	}
	if c.key == nil {
		return false
	}
	for _, candidate := range ChainOf(c.key) {
		if candidate == listener {
			return true
		}
	}
	return false
}

// Filter returns a dispatcher that selects listeners by this carrier's admission
// rule.
//
// It composes with base's own filtering, because cordis applies the dispatcher
// predicate on top of whatever the caller already selected. A listener marked
// Global still bypasses both, which is how a truly global observer stays global.
// base must be a live context; a dispatcher bound to no context would panic inside
// cordis on first use, so a nil base is rejected here with a clear message instead.
func (c *Carrier) Filter(base *cordis.Context) cordis.Dispatcher {
	if base == nil {
		panic("scope: Carrier.Filter needs a live context")
	}
	return base.Filter(func(ctx *cordis.Context) bool {
		return c.Admit(ScopeOf(ctx))
	})
}

// IsCarrier reports whether value is a carrier minted by this package. It
// distinguishes an unkeyed carrier from a value that is not a carrier at all.
func IsCarrier(value any) bool {
	carrier, ok := value.(*Carrier)
	if !ok {
		return false
	}
	_, exists := carriers[carrier]
	return exists
}

// CarrierKeyOf returns the routing key of a carrier, or nil when the value is not
// a carrier or the carrier is unkeyed.
func CarrierKeyOf(value any) Key {
	carrier, ok := value.(*Carrier)
	if !ok {
		return nil
	}
	key, exists := carriers[carrier]
	if !exists {
		return nil
	}
	return key
}

// ErrMissingCarrier reports a scope-filtered event dispatched without a carrier.
var ErrMissingCarrier = errors.New("scope: scope-filtered event dispatched without a scope carrier")

// CheckDispatch enforces the two runtime checks the reference's invariant
// companion performs on every scope-filtered dispatch.
//
// A scope-filtered event dispatched without a carrier would be admitted by every
// listener, and a carrier keyed to a different subject than the event names would
// route to the wrong agent. Neither is expressible as a type error in Go, so the
// checks live here and a caller that manages scope-filtered events must run them.
// subject must be the routing key the event's own arguments name.
func CheckDispatch(event string, carrier *Carrier, subject Key) error {
	if carrier == nil || !IsCarrier(carrier) {
		return fmt.Errorf("%w: %q", ErrMissingCarrier, event)
	}
	if carrier.Key() != subject {
		return fmt.Errorf("%w: %q was dispatched with a carrier keyed to a different subject", ErrDispatchSubject, event)
	}
	return nil
}

// ErrDispatchSubject reports a carrier keyed to a subject other than the one the
// event's arguments name.
var ErrDispatchSubject = errors.New("scope: carrier subject does not match the event arguments")
