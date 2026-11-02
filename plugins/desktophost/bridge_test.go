package desktophost_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/desktophost"
)

func TestLifecycle(t *testing.T) {
	b := desktophost.New()
	if b.State() != desktophost.StateStarting {
		t.Fatalf("state = %s", b.State())
	}
	// Inject before ready is refused.
	if _, err := b.Inject("tok"); !errors.Is(err, desktophost.ErrNotReady) {
		t.Fatalf("inject before ready err = %v", err)
	}
	if err := b.Ready(); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	got, err := b.Inject("tok")
	if err != nil || got != "tok" {
		t.Fatalf("inject = %q err=%v", got, err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if b.CloseCount() != 1 {
		t.Fatalf("close count = %d", b.CloseCount())
	}
	// Close is idempotent.
	_ = b.Close()
	if b.CloseCount() != 1 {
		t.Fatalf("close count after second = %d", b.CloseCount())
	}
	if _, err := b.Inject("tok"); !errors.Is(err, desktophost.ErrClosed) {
		t.Fatalf("inject after close err = %v", err)
	}
}

func TestFatalFailure(t *testing.T) {
	b := desktophost.New()
	if err := b.Fail("port in use"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if b.State() != desktophost.StateFailed || b.FatalReason() != "port in use" {
		t.Fatalf("state=%s reason=%q", b.State(), b.FatalReason())
	}
	// Ready after close is refused.
	c := desktophost.New()
	_ = c.Close()
	if err := c.Ready(); !errors.Is(err, desktophost.ErrClosed) {
		t.Fatalf("ready after close err = %v", err)
	}
	if err := c.Fail("x"); !errors.Is(err, desktophost.ErrClosed) {
		t.Fatalf("fail after close err = %v", err)
	}
}
