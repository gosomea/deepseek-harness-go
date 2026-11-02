package telemetry_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/telemetry"
)

type memSink struct{ got []telemetry.Event }

func (m *memSink) Record(e telemetry.Event) error { m.got = append(m.got, e); return nil }

func TestEmitAllowed(t *testing.T) {
	sink := &memSink{}
	r, err := telemetry.New(sink)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.Emit(telemetry.Event{Name: "turn", Fields: map[string]string{"status": "ok", "count": "3"}}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(sink.got) != 1 || len(r.Sent()) != 1 {
		t.Fatalf("not delivered")
	}
}

func TestEmitForbiddenField(t *testing.T) {
	sink := &memSink{}
	r, _ := telemetry.New(sink)
	err := r.Emit(telemetry.Event{Name: "turn", Fields: map[string]string{"prompt": "secret text"}})
	if !errors.Is(err, telemetry.ErrForbiddenField) {
		t.Fatalf("err = %v", err)
	}
	if len(sink.got) != 0 {
		t.Fatal("forbidden event reached the sink")
	}
}

func TestEmitValidation(t *testing.T) {
	if _, err := telemetry.New(nil); err == nil {
		t.Fatal("nil sink should fail")
	}
	r, _ := telemetry.New(&memSink{})
	if err := r.Emit(telemetry.Event{}); err == nil {
		t.Fatal("empty name should fail")
	}
}
