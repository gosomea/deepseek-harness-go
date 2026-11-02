package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/protocol"
)

// SendFunc carries one envelope to the host and returns the response envelope.
//
// It is the only transport the client depends on, so a test swaps in the gateway
// directly and a networked client swaps in a socket writer with no other change.
type SendFunc func(ctx context.Context, env protocol.Envelope) (protocol.Response, error)

// Client calls Host API methods.
type Client struct {
	send  SendFunc
	token string

	mu   sync.Mutex
	next int
}

// New returns a client over a send function and an optional bearer token.
func New(send SendFunc, token string) (*Client, error) {
	if send == nil {
		return nil, fmt.Errorf("sdk: a send function is required")
	}
	return &Client{send: send, token: token}, nil
}

// Call invokes a method and decodes the result into out.
//
// A cancelled context returns before sending, so cancel is observable rather than
// swallowed. A wire error is returned as a typed error carrying its code.
func (c *Client) Call(ctx context.Context, method string, payload any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.next++
	id := fmt.Sprintf("req-%d", c.next)
	c.mu.Unlock()

	var body json.RawMessage
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = encoded
	}
	env := protocol.Envelope{Version: protocol.Version, ID: id, Method: method, Auth: c.token, Payload: body}
	resp, err := c.send(ctx, env)
	if err != nil {
		return err
	}
	if resp.ID != id {
		// A response for another request must not be accepted as this one: that is
		// exactly the correlation bug request ids exist to prevent.
		return fmt.Errorf("sdk: response id %q does not match request %q", resp.ID, id)
	}
	if resp.Error != nil {
		return *resp.Error
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("sdk: decode result: %w", err)
		}
	}
	return nil
}
