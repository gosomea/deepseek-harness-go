package subprocess_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/subprocess"
)

// A request is checked before a process is started.
func TestRequestValidation(t *testing.T) {
	if err := (subprocess.Request{Program: "echo"}).Validate(); err != nil {
		t.Fatalf("a plain request must validate: %v", err)
	}
	if err := (subprocess.Request{}).Validate(); !errors.Is(err, subprocess.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a program, got %v", err)
	}
	if err := (subprocess.Request{Program: "  "}).Validate(); !errors.Is(err, subprocess.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank program, got %v", err)
	}
	if err := (subprocess.Request{Program: "echo", Timeout: -time.Second}).Validate(); !errors.Is(err, subprocess.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a negative timeout, got %v", err)
	}
	if err := (subprocess.Request{Program: "echo", MaxOutputBytes: -1}).Validate(); !errors.Is(err, subprocess.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a negative limit, got %v", err)
	}
}

// Success is more than a zero exit code: a truncated or stopped run is not a
// successful one, however it exited.
func TestSucceededRequiresACleanRun(t *testing.T) {
	clean := subprocess.Result{ExitCode: 0}
	if !clean.Succeeded() {
		t.Fatal("a clean zero exit must succeed")
	}
	for name, result := range map[string]subprocess.Result{
		"nonzero exit": {ExitCode: 1},
		"timed out":    {ExitCode: 0, TimedOut: true},
		"cancelled":    {ExitCode: 0, Cancelled: true},
		"truncated":    {ExitCode: 0, Truncated: true},
	} {
		if result.Succeeded() {
			t.Fatalf("%s must not count as success", name)
		}
	}
	// A negative exit code means the process was signalled.
	if (subprocess.Result{ExitCode: -1}).Succeeded() {
		t.Fatal("a signalled process must not succeed")
	}
}

// A missing platform capability is a refusal, not a silent downgrade.
func TestCapabilitiesRefuseMissingFeatures(t *testing.T) {
	full := subprocess.Capabilities{ProcessTreeKill: true, Sandboxed: true, Platform: "linux"}
	if err := full.Supports(subprocess.Capabilities{ProcessTreeKill: true, Sandboxed: true}); err != nil {
		t.Fatalf("a capable platform must satisfy the requirement: %v", err)
	}
	// Asking for nothing is always satisfiable.
	if err := full.Supports(subprocess.Capabilities{}); err != nil {
		t.Fatalf("an empty requirement must be satisfiable: %v", err)
	}
	partial := subprocess.Capabilities{ProcessTreeKill: true, Sandboxed: false, Platform: "darwin"}
	if err := partial.Supports(subprocess.Capabilities{Sandboxed: true}); !errors.Is(err, subprocess.ErrUnsupported) {
		t.Fatalf("want ErrUnsupported for missing sandboxing, got %v", err)
	}
	noTreeKill := subprocess.Capabilities{ProcessTreeKill: false, Platform: "js"}
	if err := noTreeKill.Supports(subprocess.Capabilities{ProcessTreeKill: true}); !errors.Is(err, subprocess.ErrUnsupported) {
		t.Fatalf("want ErrUnsupported for missing tree kill, got %v", err)
	}
	// The refusal names the platform, so the report is actionable.
	if err := partial.Supports(subprocess.Capabilities{Sandboxed: true}); err == nil || !contains(err.Error(), "darwin") {
		t.Fatalf("the refusal must name the platform, got %v", err)
	}
}

// The defaults must be positive and bounded.
func TestDefaultsAreUsable(t *testing.T) {
	if subprocess.DefaultOutputLimit <= 0 {
		t.Fatal("the default output limit must be positive")
	}
	if subprocess.DefaultTimeout <= 0 {
		t.Fatal("the default timeout must be positive")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
