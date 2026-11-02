package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/subprocess"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
)

// OSProcess runs commands with bounded output and process-tree cancellation.
//
// It never uses a shell. A shell would turn a value containing a space into two
// arguments, which is how an argument becomes a second command.
type OSProcess struct {
	// world confines the working directory.
	world *workspace.World
	// sandboxed records whether commands can be confined; it is reported rather
	// than assumed so a caller that requires confinement can refuse to start.
	sandboxed bool
	// maxOutput bounds one stream when a request does not say.
	maxOutput int64
	// timeout bounds a run when a request does not say.
	timeout time.Duration
}

// ProcessConfig configures the process provider.
type ProcessConfig struct {
	// World confines the working directory. Required.
	World *workspace.World
	// Sandboxed reports whether this build can confine commands. It is a
	// constructor input because the truth depends on the platform and on how the
	// program was started, not on the caller of Run.
	Sandboxed bool
	// MaxOutputBytes bounds one stream. Zero uses the package default.
	MaxOutputBytes int64
	// Timeout bounds one run. Zero uses the package default.
	Timeout time.Duration
}

// NewProcess builds a process provider over one workspace.
func NewProcess(config ProcessConfig) (*OSProcess, error) {
	if config.World == nil {
		return nil, fmt.Errorf("%w: a workspace is required", ErrInvalidValue)
	}
	maxOutput := config.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = subprocess.DefaultOutputLimit
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = subprocess.DefaultTimeout
	}
	return &OSProcess{world: config.World, sandboxed: config.Sandboxed, maxOutput: maxOutput, timeout: timeout}, nil
}

// worldRoot reports the process provider's workspace root.
func (p *OSProcess) worldRoot() string {
	if p.world == nil {
		return ""
	}
	return p.world.Root()
}

// Capabilities reports what this platform can promise.
func (p *OSProcess) Capabilities() subprocess.Capabilities {
	return subprocess.Capabilities{
		// Process-tree termination is implemented below through a process group,
		// and it works on every platform this build targets.
		ProcessTreeKill: true,
		Sandboxed:       p.sandboxed,
		Platform:        runtime.GOOS,
	}
}

// Run executes one command and waits for it.
//
// Cancellation and timeout both go through the same path: the whole process
// group is signalled, so a shell script's children cannot outlive the call that
// started them. The result reports which of the two stopped it, because
// "cancelled" and "timed out" need different responses from a caller.
func (p *OSProcess) Run(seam subprocess.Ctx, request subprocess.Request) (subprocess.Result, error) {
	// The seam declares its own minimal cancellation shape; AsProcessCtx is the
	// adapter that supplies one. A seam value that is not a context.Context means
	// the caller bypassed the adapter, which is a programming error rather than a
	// state to guess at.
	ctx, ok := seam.(context.Context)
	if !ok {
		return subprocess.Result{}, fmt.Errorf("%w: a process run needs a context.Context; use AsProcessCtx", ErrInvalidValue)
	}
	if err := request.Validate(); err != nil {
		return subprocess.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return subprocess.Result{}, err
	}
	dir := p.world.Root()
	if request.Dir != "" {
		resolved, err := p.world.Resolve(request.Dir)
		if err != nil {
			return subprocess.Result{}, err
		}
		dir = resolved
	}
	limit := request.MaxOutputBytes
	if limit <= 0 {
		limit = p.maxOutput
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = p.timeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.Command(request.Program, request.Arguments...)
	command.Dir = dir
	// A nil Env means an empty environment rather than the parent's. The
	// distinction is not cosmetic: exec.Cmd treats a nil Env as "inherit the
	// parent's", so passing nil through would hand the command every credential
	// this process holds. An empty non-nil slice is what actually means "none".
	command.Env = request.Env
	if command.Env == nil {
		command.Env = []string{}
	}
	if request.Stdin != nil {
		command.Stdin = bytes.NewReader(request.Stdin)
	}
	// A dedicated process group is what makes tree termination possible: without
	// it, signalling the group would reach this process too.
	command.SysProcAttr = processGroupAttr()

	stdout := &boundedBuffer{limit: limit}
	stderr := &boundedBuffer{limit: limit}
	command.Stdout = stdout
	command.Stderr = stderr

	start := time.Now()
	if err := command.Start(); err != nil {
		return subprocess.Result{}, err
	}
	// Wait in the background so the cancellation path can act on the group while
	// the process is still alive.
	waitErr := make(chan error, 1)
	go func() { waitErr <- command.Wait() }()

	var err error
	select {
	case err = <-waitErr:
	case <-runCtx.Done():
		// The context ended: kill the whole group, then collect the wait result so
		// no child is left running when this function returns.
		_ = killProcessTree(command.Process)
		err = <-waitErr
	}
	duration := time.Since(start)
	// Stop the output readers before reading the buffers: a grandchild that
	// inherited the pipe would otherwise keep them open.
	_ = killProcessTree(command.Process)

	result := subprocess.Result{
		Stdout:    stdout.Bytes(),
		Stderr:    stderr.Bytes(),
		Truncated: stdout.Truncated() || stderr.Truncated(),
		Duration:  duration,
		ExitCode:  exitCodeOf(command, err),
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		result.TimedOut = true
	}
	if ctx.Err() != nil {
		result.Cancelled = true
	}
	// A non-zero exit is a result, not a failure of the provider: the caller asked
	// for a command to run, and it ran. Only a start failure or a cancelled
	// context is reported as an error.
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, nil
}

// exitCodeOf extracts the exit status, reporting a signalled process as -1.
func exitCodeOf(command *exec.Cmd, err error) int {
	if command.ProcessState == nil {
		return -1
	}
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return -1
			}
			return status.ExitStatus()
		}
		return exitErr.ExitCode()
	}
	return -1
}

// boundedBuffer collects output up to a limit and records that it clipped.
//
// It is a buffer rather than a pipe reader because the limit has to hold whether
// or not anyone drains the stream: an unbounded pipe would let a chatty command
// consume memory until the limit is meaningless.
type boundedBuffer struct {
	// mu guards the buffer.
	mu sync.Mutex
	// data is what was captured.
	data bytes.Buffer
	// limit is the maximum captured size.
	limit int64
	// truncated records that bytes were dropped.
	truncated bool
}

// Write appends up to the limit and reports how much was accepted.
func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - int64(b.data.Len())
	if remaining <= 0 {
		b.truncated = true
		// Report the full length as accepted: returning a short count would make
		// the child see a write error and exit, which is a different fact from
		// "your output was too long".
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		b.truncated = true
		_, _ = b.data.Write(p[:remaining])
		return len(p), nil
	}
	return b.data.Write(p)
}

// Bytes returns the captured output.
func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data.Bytes()...)
}

// Truncated reports whether output was dropped.
func (b *boundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// Compile-time check that the provider satisfies the seam.
var _ subprocess.Runner = (*OSProcess)(nil)
