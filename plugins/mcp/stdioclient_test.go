package mcp_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/mcp"
)

func TestCallCorrelates(t *testing.T) {
	in := strings.NewReader("{\"id\":1,\"result\":{\"ok\":true}}\n")
	var out bytes.Buffer
	closed := false
	c := mcp.New(&out, in, func() error { closed = true; return nil })
	resp, err := c.Call(mcp.Request{Method: "tools/list"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(resp.Result) != `{"ok":true}` {
		t.Fatalf("result = %s", resp.Result)
	}
	if !strings.Contains(out.String(), `"method":"tools/list"`) {
		t.Fatalf("out = %q", out.String())
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !closed || !c.Closed() {
		t.Fatal("not closed")
	}
	// Close is idempotent.
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCallMismatchedID(t *testing.T) {
	in := strings.NewReader("{\"id\":99,\"result\":{}}\n")
	c := mcp.New(&bytes.Buffer{}, in, nil)
	if _, err := c.Call(mcp.Request{Method: "x"}); !errors.Is(err, mcp.ErrMismatchedID) {
		t.Fatalf("err = %v", err)
	}
}

func TestCallClosed(t *testing.T) {
	c := mcp.New(&bytes.Buffer{}, strings.NewReader(""), nil)
	_ = c.Close()
	if _, err := c.Call(mcp.Request{Method: "x"}); !errors.Is(err, mcp.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestRPCError(t *testing.T) {
	e := mcp.RPCError{Code: -32601, Message: "method not found"}
	if !strings.Contains(e.Error(), "method not found") {
		t.Fatalf("err string = %q", e.Error())
	}
}

func TestCallReadError(t *testing.T) {
	c := mcp.New(&bytes.Buffer{}, strings.NewReader(""), nil)
	if _, err := c.Call(mcp.Request{Method: "x"}); err == nil {
		t.Fatal("expected read error")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestCallWriteError(t *testing.T) {
	c := mcp.New(failWriter{}, strings.NewReader(""), nil)
	if _, err := c.Call(mcp.Request{Method: "x"}); err == nil {
		t.Fatal("expected write error")
	}
}

func TestCloseNoHook(t *testing.T) {
	c := mcp.New(&bytes.Buffer{}, strings.NewReader(""), nil)
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
