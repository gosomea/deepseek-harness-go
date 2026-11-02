package sdk_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/hostapi"
	"github.com/gosomea/deepseek-harness-go/plugins/sdk"
	"github.com/gosomea/deepseek-harness-go/protocol"
)

// gatewaySend adapts a gateway to a SendFunc, proving the client and the gateway
// agree on the wire without a socket.
func gatewaySend(g *hostapi.Gateway) sdk.SendFunc {
	return func(_ context.Context, env protocol.Envelope) (protocol.Response, error) {
		return g.Dispatch(context.Background(), env), nil
	}
}

func TestClientCall(t *testing.T) {
	g := hostapi.New(hostapi.Options{})
	_ = g.Register(hostapi.Method{Name: "echo", Handler: func(_ context.Context, _ hostapi.AuthContext, payload json.RawMessage) (any, error) {
		var in map[string]string
		_ = json.Unmarshal(payload, &in)
		return map[string]string{"echo": in["v"]}, nil
	}})
	client, err := sdk.New(gatewaySend(g), "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var out map[string]string
	if err := client.Call(context.Background(), "echo", map[string]string{"v": "hi"}, &out); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out["echo"] != "hi" {
		t.Fatalf("out = %+v", out)
	}
}

func TestClientWireError(t *testing.T) {
	g := hostapi.New(hostapi.Options{})
	client, _ := sdk.New(gatewaySend(g), "")
	var out any
	err := client.Call(context.Background(), "missing", nil, &out)
	var wire protocol.WireError
	if !errors.As(err, &wire) || wire.Code != protocol.CodeUnsupported {
		t.Fatalf("err = %v", err)
	}
}

func TestClientCancel(t *testing.T) {
	g := hostapi.New(hostapi.Options{})
	client, _ := sdk.New(gatewaySend(g), "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Call(ctx, "x", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled err = %v", err)
	}
}

func TestClientResponseMismatch(t *testing.T) {
	send := func(_ context.Context, env protocol.Envelope) (protocol.Response, error) {
		return protocol.Response{ID: "different", Result: []byte(`{}`)}, nil
	}
	client, _ := sdk.New(send, "")
	if err := client.Call(context.Background(), "m", nil, nil); err == nil {
		t.Fatal("expected correlation error")
	}
}

func TestClientNilSend(t *testing.T) {
	if _, err := sdk.New(nil, ""); err == nil {
		t.Fatal("nil send should fail")
	}
}

func TestClientDecodeError(t *testing.T) {
	send := func(_ context.Context, env protocol.Envelope) (protocol.Response, error) {
		return protocol.Response{ID: env.ID, Result: []byte(`"not an object"`)}, nil
	}
	client, _ := sdk.New(send, "")
	var out map[string]string
	if err := client.Call(context.Background(), "m", nil, &out); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestClientSendError(t *testing.T) {
	send := func(_ context.Context, _ protocol.Envelope) (protocol.Response, error) {
		return protocol.Response{}, errors.New("transport down")
	}
	client, _ := sdk.New(send, "")
	if err := client.Call(context.Background(), "m", nil, nil); err == nil {
		t.Fatal("expected send error")
	}
}
