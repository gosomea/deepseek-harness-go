package protocol_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/protocol"
)

func TestToFromACP(t *testing.T) {
	env := protocol.Envelope{Version: protocol.Version, ID: "r1", Method: "session.list", Payload: []byte(`{"x":1}`)}
	msg, err := protocol.ToACP(env)
	if err != nil {
		t.Fatalf("ToACP: %v", err)
	}
	if msg.Kind != protocol.ACPRequest || msg.ID != "r1" || msg.Method != "session.list" {
		t.Fatalf("msg = %+v", msg)
	}
	back, err := protocol.FromACP(msg)
	if err != nil {
		t.Fatalf("FromACP: %v", err)
	}
	if back.ID != "r1" || back.Method != "session.list" || back.Version != protocol.Version {
		t.Fatalf("back = %+v", back)
	}
}

func TestACPResponseKind(t *testing.T) {
	env := protocol.Envelope{Version: protocol.Version, ID: "r2", Method: "response", Payload: []byte(`{}`)}
	msg, err := protocol.ToACP(env)
	if err != nil {
		t.Fatalf("ToACP: %v", err)
	}
	if msg.Kind != protocol.ACPResponse {
		t.Fatalf("kind = %s", msg.Kind)
	}
	back, err := protocol.FromACP(protocol.ACPMessage{Kind: protocol.ACPResponse, ID: "r3"})
	if err != nil {
		t.Fatalf("FromACP response: %v", err)
	}
	if back.Method != "response" {
		t.Fatalf("method = %s", back.Method)
	}
}

func TestACPValidate(t *testing.T) {
	if err := (protocol.ACPMessage{ID: "r", Kind: protocol.ACPRequest}).Validate(); !errors.Is(err, protocol.ErrInvalidEnvelope) {
		t.Fatalf("request no method err = %v", err)
	}
	if err := (protocol.ACPMessage{ID: "", Kind: protocol.ACPResponse}).Validate(); !errors.Is(err, protocol.ErrInvalidEnvelope) {
		t.Fatalf("no id err = %v", err)
	}
	if err := (protocol.ACPMessage{ID: "r", Kind: "bogus"}).Validate(); !errors.Is(err, protocol.ErrInvalidEnvelope) {
		t.Fatalf("bad kind err = %v", err)
	}
}

func TestToACPRejectsInvalid(t *testing.T) {
	if _, err := protocol.ToACP(protocol.Envelope{Version: 99, ID: "r", Method: "m"}); err == nil {
		t.Fatal("invalid envelope should fail ToACP")
	}
}
