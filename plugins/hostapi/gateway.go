package hostapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/protocol"
)

// Errors this package returns.
var (
	// ErrDuplicateMethod reports two handlers registered for one method.
	ErrDuplicateMethod = fmt.Errorf("hostapi: duplicate method")
	// ErrUnknownMethod reports a method the gateway does not implement.
	ErrUnknownMethod = fmt.Errorf("hostapi: unknown method")
)

// Handler serves one protocol method.
type Handler func(ctx context.Context, auth AuthContext, payload json.RawMessage) (any, error)

// AuthContext is the authenticated principal of a request.
//
// It is passed to a handler rather than read from the envelope inside the handler
// so authorization is decided once, by the gateway, and a handler cannot mistake
// an unauthenticated call for an authenticated one.
type AuthContext struct {
	// Principal is the authenticated identity, empty when unauthenticated.
	Principal string
}

// Authenticated reports whether the request carried a valid credential.
func (a AuthContext) Authenticated() bool { return a.Principal != "" }

// Method describes one registered method.
type Method struct {
	// Name is the method name on the wire.
	Name string
	// RequiresAuth reports whether a credential is required.
	RequiresAuth bool
	// Handler serves the method.
	Handler Handler
}

// Authenticator validates a bearer token, returning the principal or an error.
type Authenticator func(token string) (string, error)

// Gateway dispatches envelopes to handlers.
type Gateway struct {
	mu           sync.RWMutex
	methods      map[string]Method
	order        []string
	authenticate Authenticator
	subscribers  map[string][]chan Notification
}

// Notification is one server-pushed event for a subscriber.
type Notification struct {
	// Event is the notification event name.
	Event string `json:"event"`
	// Data is the event body.
	Data json.RawMessage `json:"data,omitempty"`
}

// Options configures a gateway.
type Options struct {
	// Authenticate validates bearer tokens. Required when any method needs auth.
	Authenticate Authenticator
}

// New returns an empty gateway.
func New(options Options) *Gateway {
	return &Gateway{
		methods:      map[string]Method{},
		authenticate: options.Authenticate,
		subscribers:  map[string][]chan Notification{},
	}
}

// Register adds one method.
func (g *Gateway) Register(method Method) error {
	if method.Name == "" || method.Handler == nil {
		return fmt.Errorf("hostapi: method name and handler are required")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.methods[method.Name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateMethod, method.Name)
	}
	g.methods[method.Name] = method
	g.order = append(g.order, method.Name)
	return nil
}

// Methods returns the registered method names in registration order.
func (g *Gateway) Methods() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]string, len(g.order))
	copy(out, g.order)
	return out
}

// Dispatch handles one envelope request and returns the response.
//
// The order is: validate the envelope (version/id/method), resolve the method,
// enforce authentication, then call the handler. A version mismatch or unknown
// method is refused before any handler runs, so a client learns the problem from
// a definite error code rather than a handler that half-ran.
func (g *Gateway) Dispatch(ctx context.Context, env protocol.Envelope) protocol.Response {
	resp := protocol.Response{ID: env.ID}
	if err := env.Validate(); err != nil {
		resp.Error = wireError(protocol.CodeInvalid, err)
		return resp
	}
	g.mu.RLock()
	method, ok := g.methods[env.Method]
	authenticate := g.authenticate
	g.mu.RUnlock()
	if !ok {
		resp.Error = wireError(protocol.CodeUnsupported, fmt.Errorf("%w: %q", ErrUnknownMethod, env.Method))
		return resp
	}
	auth := AuthContext{}
	if method.RequiresAuth {
		if authenticate == nil || env.Auth == "" {
			resp.Error = wireError(protocol.CodeUnauthorized, protocol.ErrUnauthorized)
			return resp
		}
		principal, err := authenticate(env.Auth)
		if err != nil {
			resp.Error = wireError(protocol.CodeUnauthorized, err)
			return resp
		}
		auth.Principal = principal
	}
	result, err := method.Handler(ctx, auth, env.Payload)
	if err != nil {
		resp.Error = wireError(protocol.CodeInternal, err)
		return resp
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		resp.Error = wireError(protocol.CodeInternal, err)
		return resp
	}
	resp.Result = encoded
	return resp
}

// Subscribe registers a subscriber for an event and returns an unsubscribe
// function and the channel notifications are delivered on.
//
// The channel is buffered; Publish drops a notification when a subscriber is full
// rather than blocking the publisher. A slow consumer therefore misses events
// instead of stalling every other subscriber — the drop is the documented
// trade-off, not a silent hang.
func (g *Gateway) Subscribe(event string) (<-chan Notification, func()) {
	ch := make(chan Notification, 8)
	g.mu.Lock()
	g.subscribers[event] = append(g.subscribers[event], ch)
	g.mu.Unlock()
	unsubscribe := func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		list := g.subscribers[event]
		for i, candidate := range list {
			if candidate == ch {
				g.subscribers[event] = append(list[:i], list[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, unsubscribe
}

// Publish delivers a notification to every subscriber of its event.
func (g *Gateway) Publish(event string, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	g.mu.RLock()
	list := append([]chan Notification(nil), g.subscribers[event]...)
	g.mu.RUnlock()
	for _, ch := range list {
		select {
		case ch <- Notification{Event: event, Data: encoded}:
		default:
			// Slow consumer: drop rather than block.
		}
	}
	return nil
}

// wireError builds a wire error.
func wireError(code protocol.ErrorCode, err error) *protocol.WireError {
	return &protocol.WireError{Code: code, Message: err.Error()}
}
