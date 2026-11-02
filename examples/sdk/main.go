// Command sdk demonstrates calling a Host API method through the Go SDK client
// over an in-process gateway, and observing a wire error for an unknown method.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/gosomea/deepseek-harness-go/plugins/hostapi"
	"github.com/gosomea/deepseek-harness-go/plugins/sdk"
	"github.com/gosomea/deepseek-harness-go/protocol"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	g := hostapi.New(hostapi.Options{})
	if err := g.Register(hostapi.Method{
		Name: "echo",
		Handler: func(_ context.Context, _ hostapi.AuthContext, payload json.RawMessage) (any, error) {
			var in map[string]string
			_ = json.Unmarshal(payload, &in)
			return map[string]string{"echo": in["v"]}, nil
		},
	}); err != nil {
		return err
	}
	send := func(_ context.Context, env protocol.Envelope) (protocol.Response, error) {
		return g.Dispatch(context.Background(), env), nil
	}
	client, err := sdk.New(send, "")
	if err != nil {
		return err
	}
	var result map[string]string
	if err := client.Call(context.Background(), "echo", map[string]string{"v": "hi"}, &result); err != nil {
		return err
	}
	fmt.Fprintf(out, "echo result: %s\n", result["echo"])

	var ignored any
	err = client.Call(context.Background(), "missing.method", nil, &ignored)
	var wire protocol.WireError
	if ok := asWire(err, &wire); ok {
		fmt.Fprintf(out, "unknown method code: %s\n", wire.Code)
	} else {
		fmt.Fprintf(out, "unknown method error: %v\n", err)
	}
	fmt.Fprintf(out, "protocol version: %d\n", protocol.Version)
	return nil
}

// asWire reports whether err is a protocol.WireError, copying it into out.
func asWire(err error, out *protocol.WireError) bool {
	if w, ok := err.(protocol.WireError); ok {
		*out = w
		return true
	}
	return false
}
