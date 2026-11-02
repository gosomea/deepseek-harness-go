package hooks_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/hooks"
)

func TestEmitRunsHandlers(t *testing.T) {
	b := hooks.New()
	var order []string
	_ = b.Register("a", "turn.start", func(_ string, p map[string]string) error {
		order = append(order, "a")
		p["seen"] = "a"
		return nil
	})
	_ = b.Register("b", "turn.start", func(_ string, p map[string]string) error {
		order = append(order, "b")
		return nil
	})
	payload := map[string]string{}
	if err := b.Emit("turn.start", payload); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order = %v", order)
	}
	if payload["seen"] != "a" {
		t.Fatalf("payload not mutated: %v", payload)
	}
}

func TestEmitVisibleFailure(t *testing.T) {
	b := hooks.New()
	_ = b.Register("ok", "e", func(string, map[string]string) error { return nil })
	_ = b.Register("bad", "e", func(string, map[string]string) error { return errors.New("boom") })
	err := b.Emit("e", map[string]string{})
	if err == nil {
		t.Fatal("expected joined error")
	}
	if len(b.Errors()) != 1 {
		t.Fatalf("recorded errors = %d", len(b.Errors()))
	}
}

func TestRegisterValidation(t *testing.T) {
	b := hooks.New()
	h := func(string, map[string]string) error { return nil }
	if err := b.Register("", "e", h); err == nil {
		t.Fatal("empty name should fail")
	}
	if err := b.Register("n", "", h); err == nil {
		t.Fatal("empty event should fail")
	}
	if err := b.Register("n", "e", nil); err == nil {
		t.Fatal("nil handler should fail")
	}
	_ = b.Register("n", "e", h)
	if err := b.Register("n", "e", h); !errors.Is(err, hooks.ErrDuplicateHook) {
		t.Fatalf("dup err = %v", err)
	}
	if len(b.Events()) != 1 {
		t.Fatalf("events = %v", b.Events())
	}
}
