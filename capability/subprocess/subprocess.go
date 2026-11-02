// Package subprocess defines the process seam: running a command, bounding its
// output, and making sure its process tree dies when it is cancelled.
//
// The seam exists because a command is a side effect, and side effects are what
// policy exists to decide about. A tool reaches the process world only through
// this interface, and a Provider may refuse to implement it at all on a platform
// where it cannot provide the promised isolation.
package subprocess

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidValue reports an unusable request.
var ErrInvalidValue = errors.New("subprocess: invalid value")

// ErrOutputLimit reports that a process exceeded its output bound.
var ErrOutputLimit = errors.New("subprocess: output limit exceeded")

// ErrUnsupported reports a capability the platform cannot provide.
//
// It exists so a missing isolation feature is a refusal rather than a silent
// downgrade: a caller that asked for a confined process must not receive an
// unconfined one.
var ErrUnsupported = errors.New("subprocess: unsupported on this platform")

// Request describes one command to run.
type Request struct {
	// Program is the executable name or path. Required.
	Program string
	// Arguments are passed without a shell, so a value containing a space is one
	// argument rather than two words.
	Arguments []string
	// Dir is the working directory, resolved inside the workspace.
	Dir string
	// Env is the environment. A nil slice means an empty environment rather than
	// the parent's: inheriting the parent's environment would hand a command
	// credentials the caller never chose to pass.
	Env []string
	// Stdin is optional input.
	Stdin []byte
	// Timeout bounds the run. Zero uses the provider default.
	Timeout time.Duration
	// MaxOutputBytes bounds stdout and stderr each. Zero uses the default.
	MaxOutputBytes int64
}

// Validate reports whether the request is usable.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Program) == "" {
		return fmt.Errorf("%w: a program is required", ErrInvalidValue)
	}
	if r.Timeout < 0 {
		return fmt.Errorf("%w: timeout must not be negative", ErrInvalidValue)
	}
	if r.MaxOutputBytes < 0 {
		return fmt.Errorf("%w: output limit must not be negative", ErrInvalidValue)
	}
	return nil
}

// Result is the outcome of one command.
type Result struct {
	// ExitCode is the process exit status; -1 when the process was signalled.
	ExitCode int
	// Stdout is the captured output, truncated at the limit.
	Stdout []byte
	// Stderr is the captured error output, truncated at the limit.
	Stderr []byte
	// Truncated reports that a stream hit its bound, so a caller can tell a
	// short answer from a clipped one.
	Truncated bool
	// TimedOut reports that the timeout stopped the process.
	TimedOut bool
	// Cancelled reports that the context stopped the process.
	Cancelled bool
	// Duration is the wall-clock runtime.
	Duration time.Duration
}

// Succeeded reports whether the command exited zero without being stopped.
func (r Result) Succeeded() bool {
	return r.ExitCode == 0 && !r.TimedOut && !r.Cancelled && !r.Truncated
}

// Runner is the process seam.
type Runner interface {
	// Run executes one command and waits for it.
	//
	// Cancelling the context must terminate the whole process tree, not only the
	// direct child: a shell script's children would otherwise outlive the call
	// that started them.
	Run(ctx Ctx, request Request) (Result, error)
	// Capabilities reports what this platform can promise.
	Capabilities() Capabilities
}

// Capabilities describes what a platform can promise.
//
// It is reported rather than assumed, so a caller that requires a feature can
// refuse to start instead of discovering the gap when a side effect has already
// happened.
type Capabilities struct {
	// ProcessTreeKill reports that cancelling terminates descendants.
	ProcessTreeKill bool
	// Sandboxed reports that commands can be confined to the workspace.
	Sandboxed bool
	// Platform is the operating system name.
	Platform string
}

// Supports reports whether these capabilities cover a requirement.
func (c Capabilities) Supports(required Capabilities) error {
	if required.ProcessTreeKill && !c.ProcessTreeKill {
		return fmt.Errorf("%w: process-tree termination is required but unavailable on %s", ErrUnsupported, c.Platform)
	}
	if required.Sandboxed && !c.Sandboxed {
		return fmt.Errorf("%w: sandboxing is required but unavailable on %s", ErrUnsupported, c.Platform)
	}
	return nil
}

// Ctx is the cancellation contract this package needs.
type Ctx interface {
	// Err reports whether the work was cancelled.
	Err() error
	// Done reports a channel closed when the work should stop.
	Done() <-chan struct{}
}

// DefaultOutputLimit bounds one stream's capture when a request does not say.
const DefaultOutputLimit = 1 << 20

// DefaultTimeout bounds one command when a request does not say.
const DefaultTimeout = 60 * time.Second
