package lsp_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/lsp"
)

func TestCallCorrelates(t *testing.T) {
	in := strings.NewReader("{\"id\":1,\"result\":{\"capabilities\":{}}}\n")
	var out bytes.Buffer
	closed := false
	c := lsp.New(&out, in, func() error { closed = true; return nil })
	resp, err := c.Call(lsp.Request{Method: "initialize"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(string(resp.Result), "capabilities") {
		t.Fatalf("result = %s", resp.Result)
	}
	if err := c.Close(); err != nil || !closed || !c.Closed() {
		t.Fatalf("close: %v closed=%v", err, c.Closed())
	}
}

func TestCallMismatchedID(t *testing.T) {
	c := lsp.New(&bytes.Buffer{}, strings.NewReader("{\"id\":7,\"result\":{}}\n"), nil)
	if _, err := c.Call(lsp.Request{Method: "x"}); !errors.Is(err, lsp.ErrMismatchedID) {
		t.Fatalf("err = %v", err)
	}
}

func TestCallClosedAndErrors(t *testing.T) {
	c := lsp.New(&bytes.Buffer{}, strings.NewReader(""), nil)
	_ = c.Close()
	if _, err := c.Call(lsp.Request{Method: "x"}); !errors.Is(err, lsp.ErrClosed) {
		t.Fatalf("closed err = %v", err)
	}
	d := lsp.New(&bytes.Buffer{}, strings.NewReader(""), nil)
	if _, err := d.Call(lsp.Request{Method: "x"}); err == nil {
		t.Fatal("expected read error")
	}
	if e := (lsp.RPCError{Code: 1, Message: "boom"}); !strings.Contains(e.Error(), "boom") {
		t.Fatalf("err string = %q", e.Error())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestCallWriteError(t *testing.T) {
	c := lsp.New(failWriter{}, strings.NewReader(""), nil)
	if _, err := c.Call(lsp.Request{Method: "x"}); err == nil {
		t.Fatal("expected write error")
	}
}

func TestCloseNoHook(t *testing.T) {
	c := lsp.New(&bytes.Buffer{}, strings.NewReader(""), nil)
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
