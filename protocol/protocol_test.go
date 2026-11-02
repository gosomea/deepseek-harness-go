package protocol_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/protocol"
)

func TestEnvelopeValidate(t *testing.T) {
	ok := protocol.Envelope{Version: protocol.Version, ID: "r1", Method: "session.list"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	if err := (protocol.Envelope{Version: 2, ID: "r", Method: "m"}).Validate(); !errors.Is(err, protocol.ErrVersionMismatch) {
		t.Fatalf("version err = %v", err)
	}
	if err := (protocol.Envelope{Version: protocol.Version, Method: "m"}).Validate(); !errors.Is(err, protocol.ErrInvalidEnvelope) {
		t.Fatalf("no id err = %v", err)
	}
	if err := (protocol.Envelope{Version: protocol.Version, ID: "r"}).Validate(); !errors.Is(err, protocol.ErrInvalidEnvelope) {
		t.Fatalf("no method err = %v", err)
	}
}

func TestDecodeEncode(t *testing.T) {
	raw := []byte(`{"version":1,"id":"r1","method":"m","auth":"t"}`)
	env, err := protocol.DecodeEnvelope(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.ID != "r1" || env.Auth != "t" {
		t.Fatalf("env = %+v", env)
	}
	encoded, err := env.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := protocol.DecodeEnvelope(encoded); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if _, err := protocol.DecodeEnvelope([]byte("not json")); !errors.Is(err, protocol.ErrInvalidEnvelope) {
		t.Fatalf("bad json err = %v", err)
	}
}

func TestResponseValidate(t *testing.T) {
	ok := protocol.Response{ID: "r1", Result: []byte(`{}`)}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	if err := (protocol.Response{ID: "r1"}).Validate(); !errors.Is(err, protocol.ErrInvalidEnvelope) {
		t.Fatalf("empty err = %v", err)
	}
	both := protocol.Response{ID: "r1", Result: []byte("{}"), Error: &protocol.WireError{Code: protocol.CodeInvalid}}
	if err := both.Validate(); !errors.Is(err, protocol.ErrInvalidEnvelope) {
		t.Fatalf("both err = %v", err)
	}
	if err := (protocol.Response{Result: []byte("{}")}).Validate(); !errors.Is(err, protocol.ErrInvalidEnvelope) {
		t.Fatalf("no id err = %v", err)
	}
}

func TestWireError(t *testing.T) {
	e := protocol.WireError{Code: protocol.CodeNotFound, Message: "gone"}
	if e.Error() != "not-found: gone" {
		t.Fatalf("error string = %q", e.Error())
	}
}
