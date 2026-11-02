package hostapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// SessionController serves session/workspace methods over the gateway.
//
// It exists so a client can list, attach to and read a session's projection
// through the same authenticated dispatch as every other method. Authorization is
// per session: a principal may only read the sessions it owns, so one client
// cannot read another's conversation by guessing an id.
type SessionController struct {
	mu       sync.Mutex
	sessions map[string][]byte // session id -> snapshot payload
	owners   map[string]string // session id -> principal
}

// NewSessionController returns an empty controller.
func NewSessionController() *SessionController {
	return &SessionController{sessions: map[string][]byte{}, owners: map[string]string{}}
}

// Put records a session's snapshot payload owned by a principal.
func (c *SessionController) Put(sessionID, principal string, snapshot []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[sessionID] = append([]byte(nil), snapshot...)
	c.owners[sessionID] = principal
}

// Read returns a session's snapshot for a principal, enforcing ownership.
func (c *SessionController) Read(sessionID, principal string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	owner, ok := c.owners[sessionID]
	if !ok {
		return nil, fmt.Errorf("hostapi: no such session %q", sessionID)
	}
	if owner != principal {
		return nil, fmt.Errorf("hostapi: principal %q may not read session %q", principal, sessionID)
	}
	return append([]byte(nil), c.sessions[sessionID]...), nil
}

// Register installs the session methods on a gateway.
//
// The methods require auth, and the handler reads the principal from AuthContext
// — never from the payload — so a client cannot claim another's identity in the
// body.
func (c *SessionController) Register(g *Gateway) error {
	if err := g.Register(Method{Name: "session.read", RequiresAuth: true, Handler: func(_ context.Context, auth AuthContext, payload json.RawMessage) (any, error) {
		var in struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(payload, &in); err != nil {
			return nil, err
		}
		data, err := c.Read(in.SessionID, auth.Principal)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(data), nil
	}}); err != nil {
		return err
	}
	return g.Register(Method{Name: "session.list", RequiresAuth: true, Handler: func(_ context.Context, auth AuthContext, _ json.RawMessage) (any, error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		var ids []string
		for id, owner := range c.owners {
			if owner == auth.Principal {
				ids = append(ids, id)
			}
		}
		return ids, nil
	}})
}
