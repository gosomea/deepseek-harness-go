package hostapi_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/hostapi"
	"github.com/gosomea/deepseek-harness-go/protocol"
)

func TestControllerOwnership(t *testing.T) {
	g := hostapi.New(hostapi.Options{Authenticate: func(token string) (string, error) {
		return map[string]string{"tok-alice": "alice", "tok-bob": "bob"}[token], nil
	}})
	c := hostapi.NewSessionController()
	if err := c.Register(g); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.Put("s1", "alice", []byte(`{"snap":1}`))

	// Alice reads her own session.
	resp := g.Dispatch(context.Background(), env("r1", "session.read", "tok-alice", map[string]string{"sessionId": "s1"}))
	if resp.Error != nil {
		t.Fatalf("alice read: %+v", resp.Error)
	}
	// Bob may not read Alice's session.
	resp = g.Dispatch(context.Background(), env("r2", "session.read", "tok-bob", map[string]string{"sessionId": "s1"}))
	if resp.Error == nil || resp.Error.Code != protocol.CodeInternal {
		t.Fatalf("bob read should fail: %+v", resp)
	}
	// Bob's list is empty; Alice's has s1.
	resp = g.Dispatch(context.Background(), env("r3", "session.list", "tok-alice", nil))
	var ids []string
	_ = json.Unmarshal(resp.Result, &ids)
	if len(ids) != 1 || ids[0] != "s1" {
		t.Fatalf("alice list = %v", ids)
	}
	resp = g.Dispatch(context.Background(), env("r4", "session.list", "tok-bob", nil))
	_ = json.Unmarshal(resp.Result, &ids)
	if len(ids) != 0 {
		t.Fatalf("bob list = %v", ids)
	}
}

func TestControllerMissingSession(t *testing.T) {
	c := hostapi.NewSessionController()
	if _, err := c.Read("nope", "alice"); err == nil {
		t.Fatal("missing session should error")
	}
}
