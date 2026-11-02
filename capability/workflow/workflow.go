package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Errors this package returns.
var (
	// ErrInvalidScript reports a script whose meta or steps are unusable.
	ErrInvalidScript = errors.New("workflow: invalid script")
	// ErrInvalidArgs reports arguments that do not satisfy the script's meta.
	ErrInvalidArgs = errors.New("workflow: invalid arguments")
	// ErrParentRequired reports a run with no parent Agent.
	ErrParentRequired = errors.New("workflow: a parent Agent is required")
	// ErrUnknownCapability reports a script capability no provider declares.
	ErrUnknownCapability = errors.New("workflow: unknown script capability")
)

// Script is one orchestration described as data.
//
// Meta names the arguments the script needs and the capability an executor must
// declare. Steps are the observable ordered actions; the seam does not interpret
// them, a Provider does. Keeping the steps opaque to the seam is what lets a fake
// engine run the same script the production engine would, for validation.
type Script struct {
	// ID is the script's stable name.
	ID string
	// Capability is the engine capability the script requires, for example
	// "two-step-delegation". Required.
	Capability string
	// Args lists the argument names the script needs. May be empty.
	Args []string
	// Steps is the ordered action names, for tracing and fake execution. May be
	// empty only for a script whose steps are supplied at run time.
	Steps []string
}

// Validate reports whether a script can be run.
func (s Script) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: a script requires an id", ErrInvalidScript)
	}
	if strings.TrimSpace(s.Capability) == "" {
		return fmt.Errorf("%w: script %q requires a capability", ErrInvalidScript, s.ID)
	}
	for index, step := range s.Steps {
		if strings.TrimSpace(step) == "" {
			return fmt.Errorf("%w: script %q step %d is empty", ErrInvalidScript, s.ID, index)
		}
	}
	return nil
}

// ValidateArgs reports whether the arguments satisfy the script's meta.
//
// Every arg the script names must be present and non-empty, and an argument the
// script does not name is refused: accepting extra arguments would let a caller
// believe an unused value had an effect.
func (s Script) ValidateArgs(args map[string]string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	declared := map[string]bool{}
	for _, name := range s.Args {
		declared[name] = true
		if strings.TrimSpace(args[name]) == "" {
			return fmt.Errorf("%w: script %q requires arg %q", ErrInvalidArgs, s.ID, name)
		}
	}
	for name := range args {
		if !declared[name] {
			return fmt.Errorf("%w: script %q does not take arg %q", ErrInvalidArgs, s.ID, name)
		}
	}
	return nil
}

// Result is what a run produced.
type Result struct {
	// ScriptID is the script that ran.
	ScriptID string
	// Trace is the ordered step outcomes, for a reader to compare runs.
	Trace []string
	// Completed reports whether the script ran to the end.
	Completed bool
}

// Request is one run request.
type Request struct {
	// Parent is the parent Agent's id. Required: a workflow runs on behalf of
	// somebody, and an orchestration without an owner cannot be attributed.
	Parent string
	// Script is the orchestration to run.
	Script Script
	// Args are the script arguments.
	Args map[string]string
}

// Validate reports whether the request can be run.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Parent) == "" {
		return fmt.Errorf("%w", ErrParentRequired)
	}
	return r.Script.ValidateArgs(r.Args)
}

// ProviderInfo describes one workflow engine route.
type ProviderInfo struct {
	// ID is the route key.
	ID string
	// Name is the diagnostics name.
	Name string
	// Capabilities lists the script capabilities this engine can run.
	Capabilities []string
}

// Supports reports whether the engine declares a capability.
func (i ProviderInfo) Supports(capability string) bool {
	for _, declared := range i.Capabilities {
		if declared == capability {
			return true
		}
	}
	return false
}

// Provider runs a workflow script.
type Provider interface {
	// Run executes one request.
	Run(ctx context.Context, request Request) (Result, error)
	// Info describes the engine and its declared capabilities.
	Info() ProviderInfo
}
