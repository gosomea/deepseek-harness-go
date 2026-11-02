package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Errors this client returns.
var (
	// ErrClosed reports use of a client after Close.
	ErrClosed = errors.New("mcp: client closed")
	// ErrMismatchedID reports a response whose id does not match the request.
	ErrMismatchedID = errors.New("mcp: response id does not match request")
)

// Request is one JSON-RPC request.
type Request struct {
	// ID correlates the response.
	ID int `json:"id"`
	// Method is the RPC method.
	Method string `json:"method"`
	// Params is the method parameters.
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is one JSON-RPC response.
type Response struct {
	// ID echoes the request id.
	ID int `json:"id"`
	// Result is the success body.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is the failure body.
	Error *RPCError `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	// Code is the numeric error code.
	Code int `json:"code"`
	// Message is the human message.
	Message string `json:"message"`
}

// Error implements the error interface.
func (e RPCError) Error() string { return fmt.Sprintf("mcp: rpc error %d: %s", e.Code, e.Message) }

// Client speaks length-delimited JSON over a reader/writer pair.
//
// It is not tied to os/exec so a test can supply buffers; a production starter
// wires the same client to a child process's stdin/stdout and owns the process
// teardown alongside Close.
type Client struct {
	mu     sync.Mutex
	out    io.Writer
	in     *bufio.Reader
	nextID int
	closed bool
	closef func() error
}

// New returns a client over a reader and writer, with an optional close hook that
// tears down the underlying transport (for example a child process).
func New(out io.Writer, in io.Reader, closef func() error) *Client {
	return &Client{out: out, in: bufio.NewReader(in), closef: closef}
}

// Call sends one request and reads its response, matching by id.
//
// The correlation check is explicit: a response for another id is refused rather
// than returned, which is what keeps a pipelined client from stealing an answer.
func (c *Client) Call(request Request) (Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Response{}, fmt.Errorf("%w", ErrClosed)
	}
	if request.ID == 0 {
		c.nextID++
		request.ID = c.nextID
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	if _, err := c.out.Write(append(encoded, '\n')); err != nil {
		return Response{}, err
	}
	line, err := c.in.ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, err
	}
	if resp.ID != request.ID {
		return Response{}, fmt.Errorf("%w: got %d, sent %d", ErrMismatchedID, resp.ID, request.ID)
	}
	return resp, nil
}

// Close tears down the client and its transport exactly once.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.closef != nil {
		return c.closef()
	}
	return nil
}

// Closed reports whether Close was called.
func (c *Client) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}
