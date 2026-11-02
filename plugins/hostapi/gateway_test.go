package hostapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/hostapi"
	"github.com/gosomea/deepseek-harness-go/protocol"
)

func env(id, method, auth string, payload any) protocol.Envelope {
	var raw json.RawMessage
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	return protocol.Envelope{Version: protocol.Version, ID: id, Method: method, Auth: auth, Payload: raw}
}

func auth(token string) (string, error) {
	if token == "good" {
		return "alice", nil
	}
	return "", errors.New("bad token")
}

func TestDispatchUnknownMethod(t *testing.T) {
	g := hostapi.New(hostapi.Options{})
	resp := g.Dispatch(context.Background(), env("r1", "nope", "", nil))
	if resp.Error == nil || resp.Error.Code != protocol.CodeUnsupported {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestDispatchAuth(t *testing.T) {
	g := hostapi.New(hostapi.Options{Authenticate: auth})
	_ = g.Register(hostapi.Method{Name: "secure", RequiresAuth: true, Handler: func(_ context.Context, a hostapi.AuthContext, _ json.RawMessage) (any, error) {
		return map[string]string{"principal": a.Principal}, nil
	}})
	// No token.
	resp := g.Dispatch(context.Background(), env("r1", "secure", "", nil))
	if resp.Error == nil || resp.Error.Code != protocol.CodeUnauthorized {
		t.Fatalf("no-token resp = %+v", resp)
	}
	// Bad token.
	resp = g.Dispatch(context.Background(), env("r2", "secure", "bad", nil))
	if resp.Error == nil || resp.Error.Code != protocol.CodeUnauthorized {
		t.Fatalf("bad-token resp = %+v", resp)
	}
	// Good token.
	resp = g.Dispatch(context.Background(), env("r3", "secure", "good", nil))
	if resp.Error != nil {
		t.Fatalf("good resp = %+v", resp)
	}
	var out map[string]string
	if err := json.Unmarshal(resp.Result, &out); err != nil || out["principal"] != "alice" {
		t.Fatalf("result = %s", resp.Result)
	}
}

func TestDispatchVersionMismatch(t *testing.T) {
	g := hostapi.New(hostapi.Options{})
	e := env("r1", "x", "", nil)
	e.Version = 99
	resp := g.Dispatch(context.Background(), e)
	if resp.Error == nil || resp.Error.Code != protocol.CodeInvalid {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestDuplicateMethod(t *testing.T) {
	g := hostapi.New(hostapi.Options{})
	h := func(context.Context, hostapi.AuthContext, json.RawMessage) (any, error) { return nil, nil }
	if err := g.Register(hostapi.Method{Name: "m", Handler: h}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := g.Register(hostapi.Method{Name: "m", Handler: h}); !errors.Is(err, hostapi.ErrDuplicateMethod) {
		t.Fatalf("dup err = %v", err)
	}
	if err := g.Register(hostapi.Method{Name: ""}); err == nil {
		t.Fatal("empty name should fail")
	}
}

func TestHandlerError(t *testing.T) {
	g := hostapi.New(hostapi.Options{})
	_ = g.Register(hostapi.Method{Name: "boom", Handler: func(context.Context, hostapi.AuthContext, json.RawMessage) (any, error) {
		return nil, errors.New("kaboom")
	}})
	resp := g.Dispatch(context.Background(), env("r1", "boom", "", nil))
	if resp.Error == nil || resp.Error.Code != protocol.CodeInternal {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestSubscribePublish(t *testing.T) {
	g := hostapi.New(hostapi.Options{})
	ch, unsub := g.Subscribe("session.updated")
	if err := g.Publish("session.updated", map[string]string{"id": "s1"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case n := <-ch:
		if n.Event != "session.updated" {
			t.Fatalf("event = %s", n.Event)
		}
	default:
		t.Fatal("no notification delivered")
	}
	unsub()
	// After unsubscribe, the channel is closed.
	if _, ok := <-ch; ok {
		t.Fatal("channel not closed after unsubscribe")
	}
}

func TestMethodsList(t *testing.T) {
	g := hostapi.New(hostapi.Options{})
	h := func(context.Context, hostapi.AuthContext, json.RawMessage) (any, error) { return nil, nil }
	_ = g.Register(hostapi.Method{Name: "a", Handler: h})
	_ = g.Register(hostapi.Method{Name: "b", Handler: h})
	got := g.Methods()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("methods = %v", got)
	}
}
