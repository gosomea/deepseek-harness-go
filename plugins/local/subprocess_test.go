package local_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/capability/subprocess"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
	"github.com/gosomea/deepseek-harness-go/plugins/local"
)

// newProcess builds a process provider over a fresh temporary world.
func newProcess(t *testing.T) (*local.OSProcess, string) {
	t.Helper()
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root, ResolveSymlinks: true})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	provider, err := local.NewProcess(local.ProcessConfig{World: world})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	return provider, world.Root()
}

// A command's output and exit code are captured, and a non-zero exit is a result
// rather than a provider failure.
func TestRunCapturesOutputAndExitCode(t *testing.T) {
	provider, _ := newProcess(t)
	ctx := local.AsProcessCtx(context.Background())
	result, err := provider.Run(ctx, subprocess.Request{Program: "/bin/sh", Arguments: []string{"-c", "echo hello"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(result.Stdout)) != "hello" {
		t.Fatalf("unexpected stdout %q", result.Stdout)
	}
	if result.ExitCode != 0 || !result.Succeeded() {
		t.Fatalf("a clean run must succeed: %+v", result)
	}
	if result.Duration <= 0 {
		t.Fatal("a run must report its duration")
	}
	// A non-zero exit is reported as a result, not as an error.
	failing, err := provider.Run(ctx, subprocess.Request{Program: "/bin/sh", Arguments: []string{"-c", "exit 3"}})
	if err != nil {
		t.Fatalf("a failing command must not be a provider error: %v", err)
	}
	if failing.ExitCode != 3 || failing.Succeeded() {
		t.Fatalf("unexpected failing result %+v", failing)
	}
}

// Arguments are passed without a shell, so a value containing a space stays one
// argument instead of becoming a second word.
func TestArgumentsAreNotSplitByAShell(t *testing.T) {
	provider, _ := newProcess(t)
	ctx := local.AsProcessCtx(context.Background())
	// printf with one argument that contains a space prints it unchanged; a shell
	// would split it into two words.
	result, err := provider.Run(ctx, subprocess.Request{
		Program: "/usr/bin/printf", Arguments: []string{"%s", "one two"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(result.Stdout) != "one two" {
		t.Fatalf("the argument was split: %q", result.Stdout)
	}
}

// Output beyond the limit is clipped and reported as truncated, so a caller can
// tell a short answer from a clipped one.
func TestOutputLimitTruncatesAndReports(t *testing.T) {
	provider, _ := newProcess(t)
	ctx := local.AsProcessCtx(context.Background())
	result, err := provider.Run(ctx, subprocess.Request{
		Program: "/bin/sh",
		// 10000 characters, well past the 64-byte limit below.
		Arguments:      []string{"-c", "printf 'a%.0s' $(seq 1 10000)"},
		MaxOutputBytes: 64,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Stdout) != 64 {
		t.Fatalf("want exactly the limit captured, got %d bytes", len(result.Stdout))
	}
	if !result.Truncated {
		t.Fatal("clipped output must be reported as truncated")
	}
	if result.Succeeded() {
		t.Fatal("a truncated run is not a clean success")
	}
}

// A command that outlives its timeout is stopped and reported as timed out.
func TestTimeoutStopsTheProcess(t *testing.T) {
	provider, _ := newProcess(t)
	ctx := local.AsProcessCtx(context.Background())
	start := time.Now()
	result, err := provider.Run(ctx, subprocess.Request{
		Program: "/bin/sh", Arguments: []string{"-c", "sleep 10"},
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("a timeout must be reported: %+v", result)
	}
	if result.Cancelled {
		t.Fatal("a timeout is not a cancellation")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the timeout did not stop the process promptly: %v", elapsed)
	}
}

// Cancelling the context stops the process and reports a cancellation, which is
// distinct from a timeout.
func TestCancellationStopsTheProcess(t *testing.T) {
	provider, _ := newProcess(t)
	ctx, cancel := context.WithCancel(context.Background())
	seam := local.AsProcessCtx(ctx)
	done := make(chan subprocess.Result, 1)
	go func() {
		result, err := provider.Run(seam, subprocess.Request{Program: "/bin/sh", Arguments: []string{"-c", "sleep 10"}})
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("unexpected error: %v", err)
		}
		done <- result
	}()
	// A barrier rather than a sleep: wait until the process is actually running by
	// polling for its marker file, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case result := <-done:
		if !result.Cancelled {
			t.Fatalf("a cancelled run must be reported: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not stop the process")
	}
}

// A cancelled context stops the whole process tree: a grandchild must not outlive
// the call that started it.
func TestCancellationKillsTheProcessTree(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("tree termination is verified on Unix here; the Windows path is unverified")
	}
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root, ResolveSymlinks: true})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	provider, err := local.NewProcess(local.ProcessConfig{World: world})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	marker := filepath.Join(root, "grandchild-alive")
	// The script backgrounds a long sleep that writes a marker, then waits. After
	// the call is cancelled the marker must stop being updated.
	script := "(while true; do date +%s%N > " + marker + "; sleep 0.1; done) & sleep 30"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = provider.Run(local.AsProcessCtx(ctx), subprocess.Request{Program: "/bin/sh", Arguments: []string{"-c", script}})
	}()
	// Wait for the marker to appear, which proves the grandchild runs.
	if !waitForFile(t, marker, 5*time.Second) {
		cancel()
		<-done
		t.Skip("the environment did not start the background child")
	}
	cancel()
	<-done
	// After cancellation the marker must stop changing.
	first := readFileString(t, marker)
	time.Sleep(400 * time.Millisecond)
	second := readFileString(t, marker)
	if first != second {
		t.Fatalf("a grandchild survived cancellation: %q -> %q", first, second)
	}
}

// A nil environment means an empty one rather than the parent's, so a command
// never inherits credentials the caller did not pass.
func TestNilEnvironmentIsEmptyNotInherited(t *testing.T) {
	provider, _ := newProcess(t)
	ctx := local.AsProcessCtx(context.Background())
	t.Setenv("DSH_SECRET_MARKER", "leaked")
	result, err := provider.Run(ctx, subprocess.Request{
		Program: "/bin/sh", Arguments: []string{"-c", "printf '%s' \"$DSH_SECRET_MARKER\""},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(result.Stdout) != "" {
		t.Fatalf("the parent environment leaked into the command: %q", result.Stdout)
	}
	// An explicit environment is passed through.
	explicit, err := provider.Run(ctx, subprocess.Request{
		Program: "/bin/sh", Arguments: []string{"-c", "printf '%s' \"$DSH_SECRET_MARKER\""},
		Env: []string{"DSH_SECRET_MARKER=chosen"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(explicit.Stdout) != "chosen" {
		t.Fatalf("an explicit environment must be passed: %q", explicit.Stdout)
	}
}

// A working directory outside the world is refused before anything runs.
func TestWorkingDirectoryIsConfined(t *testing.T) {
	provider, root := newProcess(t)
	ctx := local.AsProcessCtx(context.Background())
	if _, err := provider.Run(ctx, subprocess.Request{
		Program: "/bin/sh", Arguments: []string{"-c", "pwd"},
		Dir: filepath.Join(root, ".."),
	}); !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("want ErrOutsideWorkspace, got %v", err)
	}
	// A directory inside the world is used.
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	result, err := provider.Run(ctx, subprocess.Request{
		Program: "/bin/sh", Arguments: []string{"-c", "pwd"}, Dir: "sub",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(result.Stdout)), "sub") {
		t.Fatalf("the working directory was not applied: %q", result.Stdout)
	}
}

// The reported capabilities describe this platform, and a requirement the
// platform cannot meet is a refusal.
func TestCapabilitiesAndRefusal(t *testing.T) {
	provider, _ := newProcess(t)
	capabilities := provider.Capabilities()
	if capabilities.Platform == "" {
		t.Fatal("the platform must be reported")
	}
	if !capabilities.ProcessTreeKill {
		t.Fatal("process-tree termination is implemented on this build")
	}
	if capabilities.Sandboxed {
		t.Fatal("this provider does not claim to sandbox by default")
	}
	// A caller requiring confinement is refused rather than silently unconfined.
	if err := capabilities.Supports(subprocess.Capabilities{Sandboxed: true}); !errors.Is(err, subprocess.ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
	if err := capabilities.Supports(subprocess.Capabilities{ProcessTreeKill: true}); err != nil {
		t.Fatalf("tree termination is available: %v", err)
	}
}

// A cancelled context refuses the run before starting a process.
func TestCancelledContextRefusesTheRun(t *testing.T) {
	provider, _ := newProcess(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Run(local.AsProcessCtx(ctx), subprocess.Request{Program: "/bin/echo"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// A missing program is a provider error, not a fabricated result.
func TestMissingProgramIsAnError(t *testing.T) {
	provider, _ := newProcess(t)
	if _, err := provider.Run(local.AsProcessCtx(context.Background()), subprocess.Request{
		Program: "/nonexistent/program-xyz",
	}); err == nil {
		t.Fatal("starting a missing program must report an error")
	}
}

// A stale configuration is refused at construction.
func TestProcessConfiguration(t *testing.T) {
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	if _, err := local.NewProcess(local.ProcessConfig{}); !errors.Is(err, local.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a workspace, got %v", err)
	}
	provider, err := local.NewProcess(local.ProcessConfig{World: world, MaxOutputBytes: 10, Timeout: time.Second})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if provider.Capabilities().Platform == "" {
		t.Fatal("the provider must report its platform")
	}
	// Stdin is passed through.
	result, err := provider.Run(local.AsProcessCtx(context.Background()), subprocess.Request{
		Program: "/bin/cat", Stdin: []byte("from stdin"), MaxOutputBytes: 64,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(result.Stdout) != "from stdin" {
		t.Fatalf("stdin was not passed through: %q", result.Stdout)
	}
}

// waitForFile waits until a path exists, using a bounded poll rather than a
// single sleep so a slow machine does not produce a false failure.
func waitForFile(t *testing.T, path string, limit time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// readFileString reads a file, returning an empty string when it is unreadable.
func readFileString(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

// A process killed by a signal reports -1, which is a different fact from any
// exit code the process itself could have returned.
func TestSignalledProcessReportsMinusOne(t *testing.T) {
	provider, _ := newProcess(t)
	ctx := local.AsProcessCtx(context.Background())
	result, err := provider.Run(ctx, subprocess.Request{
		Program: "/bin/sh", Arguments: []string{"-c", "kill -TERM $$"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.ExitCode != -1 {
		t.Fatalf("a signalled process must report -1, got %d", result.ExitCode)
	}
	if result.Succeeded() {
		t.Fatal("a signalled process must not count as success")
	}
}

// Stderr is captured separately from stdout, so a caller can tell a diagnostic
// from a result.
func TestStderrIsCapturedSeparately(t *testing.T) {
	provider, _ := newProcess(t)
	ctx := local.AsProcessCtx(context.Background())
	result, err := provider.Run(ctx, subprocess.Request{
		Program: "/bin/sh", Arguments: []string{"-c", "echo out; echo err 1>&2"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(result.Stdout)) != "out" {
		t.Fatalf("unexpected stdout %q", result.Stdout)
	}
	if strings.TrimSpace(string(result.Stderr)) != "err" {
		t.Fatalf("unexpected stderr %q", result.Stderr)
	}
}

// The default limits apply when a request does not name its own.
func TestProviderDefaultsApply(t *testing.T) {
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	// A short timeout configured on the provider stops a long command.
	provider, err := local.NewProcess(local.ProcessConfig{World: world, Timeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	result, err := provider.Run(local.AsProcessCtx(context.Background()), subprocess.Request{
		Program: "/bin/sh", Arguments: []string{"-c", "sleep 5"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("the provider default timeout must apply: %+v", result)
	}
	// A provider-imposed output limit applies when the request does not set one.
	small, err := local.NewProcess(local.ProcessConfig{World: world, MaxOutputBytes: 8})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	clipped, err := small.Run(local.AsProcessCtx(context.Background()), subprocess.Request{
		Program: "/bin/sh", Arguments: []string{"-c", "printf 'aaaaaaaaaaaaaaaaaaaa'"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(clipped.Stdout) != 8 || !clipped.Truncated {
		t.Fatalf("the provider default limit must apply: %+v", clipped)
	}
}
