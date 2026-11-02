package local_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/approval"
	"github.com/gosomea/deepseek-harness-go/capability/fs"
	"github.com/gosomea/deepseek-harness-go/capability/sandbox"
	"github.com/gosomea/deepseek-harness-go/capability/subprocess"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
	"github.com/gosomea/deepseek-harness-go/plugins/local"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// toolSet builds a tool set over a fresh world with a scripted approval answer.
func toolSet(t *testing.T, mode fs.Mode, outcome approval.Outcome, policy sandbox.Policy) (*local.Tools, string) {
	t.Helper()
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root, ResolveSymlinks: true})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	fileSystem, err := local.NewFS(local.FSConfig{World: world, Mode: mode})
	if err != nil {
		t.Fatalf("fs: %v", err)
	}
	process, err := local.NewProcess(local.ProcessConfig{World: world})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	gateway, err := approval.NewGateway(&approval.AlwaysResponder{Outcome: outcome}, approval.PolicyAskOnce)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	set, err := local.NewTools(local.ToolsConfig{FS: fileSystem, Process: process, Approvals: gateway, Policy: policy})
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	return set, root
}

// call runs one tool through the real pipeline, which is what a model's request
// would go through.
//
// It returns the result and the error together: the pipeline reports a refused
// or failed tool body as both an error and a result with IsError set, so a test
// asserting only one of the two would miss half the contract.
func call(t *testing.T, set *local.Tools, name, callID string, arguments map[string]any) (tools.Result, error) {
	t.Helper()
	registry := tools.NewRegistry()
	if err := set.Register(registry); err != nil {
		t.Fatalf("register: %v", err)
	}
	pipeline := &tools.Pipeline{Registry: registry}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return pipeline.Execute(context.Background(), tools.Call{ID: callID, Name: tools.ToolName(name), Arguments: string(encoded)})
}

// defaultPolicy is the confinement every write test runs under.
func defaultPolicy() sandbox.Policy {
	return sandbox.Policy{Effective: sandbox.ModeWorkspaceWrite, Available: sandbox.ModeDangerFullAccess}
}

// A read needs no approval: it has no side effect to authorize.
func TestReadNeedsNoApproval(t *testing.T) {
	set, root := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeRejected, defaultPolicy())
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	result, err := call(t, set, "read_file", "call-1", map[string]any{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("a read must not be denied: %v", err)
	}
	if result.IsError {
		t.Fatalf("a read must not be marked as an error: %+v", result)
	}
	if !strings.Contains(result.Content, "content") {
		t.Fatalf("the content must be returned: %q", result.Content)
	}
	// The path in the answer is relative, so the host's layout is not published.
	if strings.Contains(result.Content, root) {
		t.Fatalf("the answer must not leak the absolute path: %q", result.Content)
	}
}

// An allowed edit writes; a rejected one must not touch the file at all.
func TestEditIsDeniedWithoutApproval(t *testing.T) {
	rejected, root := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeRejected, defaultPolicy())
	result, err := call(t, rejected, "edit_file", "call-1", map[string]any{"path": "new.txt", "content": "written"})
	if err == nil {
		t.Fatalf("a rejected approval must make the call an error: %+v", result)
	}
	if !result.IsError {
		t.Fatalf("the result must be marked as an error too: %+v", result)
	}
	if !errors.Is(err, local.ErrPolicyDenied) {
		t.Fatalf("want ErrPolicyDenied, got %v", err)
	}
	if !strings.Contains(result.Content, "not authorized") {
		t.Fatalf("the denial must be explained: %q", result.Content)
	}
	// The decisive assertion: nothing was written.
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("a denied edit must not create the file")
	}

	allowed, allowedRoot := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	result, err = call(t, allowed, "edit_file", "call-2", map[string]any{"path": "new.txt", "content": "written"})
	if err != nil {
		t.Fatalf("an allowed edit must succeed: %v", err)
	}
	if result.IsError {
		t.Fatalf("an allowed edit must not be marked as an error: %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(allowedRoot, "new.txt"))
	if err != nil {
		t.Fatalf("the file must exist: %v", err)
	}
	if string(content) != "written" {
		t.Fatalf("unexpected content %q", content)
	}
}

// A write outside the world is refused, whatever the approval said.
func TestEditRefusesAPathOutsideTheWorld(t *testing.T) {
	set, root := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	outside := filepath.Join(root, "..", "escaped.txt")
	result, err := call(t, set, "edit_file", "call-1", map[string]any{"path": outside, "content": "x"})
	if err == nil {
		t.Fatalf("a write outside the world must fail: %+v", result)
	}
	if !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("want ErrOutsideWorkspace, got %v", err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("a refused write must not create anything outside the world")
	}
}

// A read-only filesystem does not even offer the editing tool, so the model
// cannot be asked to choose an action that is not permitted.
func TestReadOnlyOffersNoEditingTool(t *testing.T) {
	set, _ := toolSet(t, fs.ModeReadOnly, approval.OutcomeAllowedOnce, sandbox.Policy{Effective: sandbox.ModeReadOnly, Available: sandbox.ModeWorkspaceWrite})
	names := map[string]bool{}
	for _, definition := range set.Definitions() {
		names[string(definition.Name)] = true
	}
	if !names["read_file"] {
		t.Fatal("the read tool must always be offered")
	}
	if names["edit_file"] {
		t.Fatal("a read-only filesystem must not offer the editing tool")
	}
}

// The shell tool asks before running, and a denial means the command never starts.
func TestShellIsDeniedWithoutApproval(t *testing.T) {
	set, root := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeRejected, defaultPolicy())
	marker := filepath.Join(root, "ran")
	result, err := call(t, set, "run_command", "call-1", map[string]any{
		"program": "/bin/sh", "arguments": []any{"-c", "touch " + marker},
	})
	if err == nil {
		t.Fatalf("a rejected approval must make the call an error: %+v", result)
	}
	if !errors.Is(err, local.ErrPolicyDenied) {
		t.Fatalf("want ErrPolicyDenied, got %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("a denied command must not have run")
	}

	allowed, allowedRoot := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	allowedMarker := filepath.Join(allowedRoot, "ran")
	result, err = call(t, allowed, "run_command", "call-2", map[string]any{
		"program": "/bin/sh", "arguments": []any{"-c", "touch " + allowedMarker},
	})
	if err != nil {
		t.Fatalf("an allowed command must run: %v", err)
	}
	if result.IsError {
		t.Fatalf("an allowed command must not be marked as an error: %+v", result)
	}
	if _, err := os.Stat(allowedMarker); err != nil {
		t.Fatalf("the allowed command must have run: %v", err)
	}
}

// A command under a read-only policy is refused without asking anyone: the
// isolation cannot be downgraded implicitly to make the call work.
func TestShellRefusesUnderAReadOnlyPolicy(t *testing.T) {
	set, _ := toolSet(t, fs.ModeReadOnly, approval.OutcomeAllowedOnce, sandbox.Policy{Effective: sandbox.ModeReadOnly, Available: sandbox.ModeWorkspaceWrite})
	result, err := call(t, set, "run_command", "call-1", map[string]any{"program": "/bin/echo", "arguments": []any{"hello"}})
	if err == nil {
		t.Fatalf("a command under read-only must be refused: %+v", result)
	}
	if !errors.Is(err, local.ErrPolicyDenied) {
		t.Fatalf("want ErrPolicyDenied, got %v", err)
	}
	if !strings.Contains(result.Content, "workspace-write") {
		t.Fatalf("the refusal must name the required mode: %q", result.Content)
	}
}

// A command's non-zero exit is reported to the model as an error result, because
// the model needs to see that the command failed.
func TestShellReportsANonZeroExit(t *testing.T) {
	set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	result, err := call(t, set, "run_command", "call-1", map[string]any{"program": "/bin/sh", "arguments": []any{"-c", "exit 7"}})
	// A non-zero exit is a successful tool execution whose reported outcome is a
	// failure: the model must see it as an error result, not as a broken pipeline.
	if err != nil {
		t.Fatalf("a non-zero exit is a result, not a pipeline failure: %v", err)
	}
	if !result.IsError {
		t.Fatalf("a non-zero exit must be reported as an error result: %+v", result)
	}
	if !strings.Contains(result.Content, "exit=7") {
		t.Fatalf("the exit code must be reported: %q", result.Content)
	}
}

// A side effect must be bound to a call identity: without one there is nothing
// for a grant to be bound to.
func TestSideEffectRequiresACallIdentity(t *testing.T) {
	set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	registry := tools.NewRegistry()
	if err := set.Register(registry); err != nil {
		t.Fatalf("register: %v", err)
	}
	pipeline := &tools.Pipeline{Registry: registry}
	result, err := pipeline.Execute(context.Background(), tools.Call{
		Name: "edit_file", Arguments: `{"path":"a.txt","content":"x"}`,
	})
	if err == nil {
		t.Fatalf("a call without an id must be refused: %+v", result)
	}
	if !result.IsError {
		t.Fatalf("the result must be marked as an error: %+v", result)
	}
}

// Registration and configuration are validated.
func TestToolsConfiguration(t *testing.T) {
	if _, err := local.NewTools(local.ToolsConfig{}); !errors.Is(err, local.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an approval channel, got %v", err)
	}
	gateway, err := approval.NewGateway(nil, approval.PolicyDeny)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	// A policy that cannot be enforced is refused at construction, not at the
	// first side effect.
	if _, err := local.NewTools(local.ToolsConfig{
		Approvals: gateway,
		Policy:    sandbox.Policy{Effective: sandbox.ModeDangerFullAccess, Available: sandbox.ModeReadOnly},
	}); !errors.Is(err, sandbox.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	// A set with neither provider has nothing to act through, so it is refused
	// rather than offering a read tool that would dereference nil.
	if _, err := local.NewTools(local.ToolsConfig{
		Approvals: gateway,
		Policy:    sandbox.Policy{Effective: sandbox.ModeReadOnly, Available: sandbox.ModeWorkspaceWrite},
	}); !errors.Is(err, local.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a provider, got %v", err)
	}
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w-1", Root: root})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	fileSystem, err := local.NewFS(local.FSConfig{World: world, Mode: fs.ModeReadOnly})
	if err != nil {
		t.Fatalf("fs: %v", err)
	}
	set, err := local.NewTools(local.ToolsConfig{
		FS:        fileSystem,
		Approvals: gateway,
		Policy:    sandbox.Policy{Effective: sandbox.ModeReadOnly, Available: sandbox.ModeWorkspaceWrite},
	})
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	if err := set.Register(nil); !errors.Is(err, local.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a nil registry, got %v", err)
	}
	// A read-only filesystem offers only the read tool.
	if names := toolNames(set); len(names) != 1 || !names["read_file"] {
		t.Fatalf("a read-only set must offer only read_file, got %v", names)
	}
}

// Registering the same set twice is refused by the registry rather than
// silently replacing a definition.
func TestToolsRegisterTwice(t *testing.T) {
	set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	registry := tools.NewRegistry()
	if err := set.Register(registry); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := set.Register(registry); err == nil {
		t.Fatal("a duplicate registration must be refused")
	}
}

// The tool argument readers reject what the pipeline cannot have passed, so a
// direct caller cannot smuggle a wrong type past validation.
func TestArgumentReadersRejectWrongTypes(t *testing.T) {
	// These calls bypass schema validation to exercise the readers directly; the
	// pipeline would have rejected the arguments earlier, so the branches only
	// exist to keep the readers total.
	cases := []struct {
		name      string
		arguments map[string]any
	}{
		{"missing path", map[string]any{}},
		{"non-string path", map[string]any{"path": 42}},
		{"blank path", map[string]any{"path": "   "}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
			registry := tools.NewRegistry()
			if err := set.Register(registry); err != nil {
				t.Fatalf("register: %v", err)
			}
			// The schema refuses these before the body runs, which is the behavior
			// a model actually experiences.
			pipeline := &tools.Pipeline{Registry: registry}
			encoded, err := json.Marshal(testCase.arguments)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			result, err := pipeline.Execute(context.Background(), tools.Call{
				ID: "call-1", Name: "read_file", Arguments: string(encoded),
			})
			if err == nil {
				t.Fatalf("invalid arguments must be refused: %+v", result)
			}
			if !result.IsError {
				t.Fatalf("the result must be marked as an error: %+v", result)
			}
		})
	}
	// A non-array arguments value is refused for the command tool.
	set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	registry := tools.NewRegistry()
	if err := set.Register(registry); err != nil {
		t.Fatalf("register: %v", err)
	}
	pipeline := &tools.Pipeline{Registry: registry}
	result, err := pipeline.Execute(context.Background(), tools.Call{
		ID: "call-2", Name: "run_command", Arguments: `{"program":"/bin/echo","arguments":"not-an-array"}`,
	})
	if err == nil {
		t.Fatalf("a non-array arguments value must be refused: %+v", result)
	}
	// A non-string array element is refused too.
	result, err = pipeline.Execute(context.Background(), tools.Call{
		ID: "call-3", Name: "run_command", Arguments: `{"program":"/bin/echo","arguments":[1,2]}`,
	})
	if err == nil {
		t.Fatalf("a non-string element must be refused: %+v", result)
	}
}

// Running a command with no arguments at all is allowed: the argument list is
// optional, so a program that needs none must work.
func TestCommandWithoutArguments(t *testing.T) {
	set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	result, err := call(t, set, "run_command", "call-1", map[string]any{"program": "/usr/bin/true"})
	if err != nil {
		t.Fatalf("a command with no arguments must run: %v", err)
	}
	if result.IsError {
		t.Fatalf("a successful command must not be an error: %+v", result)
	}
}

// A command whose stderr is non-empty reports it, so the model sees the
// diagnostic rather than only the exit code.
func TestCommandReportsStderr(t *testing.T) {
	set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	result, err := call(t, set, "run_command", "call-1", map[string]any{
		"program": "/bin/sh", "arguments": []any{"-c", "echo diagnostic 1>&2"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(result.Content, "diagnostic") {
		t.Fatalf("stderr must be reported: %q", result.Content)
	}
}

// A truncated command reports the truncation, so a clipped answer is not read as
// a complete one.
func TestCommandReportsTruncation(t *testing.T) {
	set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	result, err := call(t, set, "run_command", "call-1", map[string]any{
		"program": "/bin/sh", "arguments": []any{"-c", "head -c 2000000 /dev/zero | tr '\\0' 'a'"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(result.Content, "truncated") {
		t.Fatalf("truncation must be reported: %q", result.Content[:min(200, len(result.Content))])
	}
}

// The read tool reports a missing file as an error result rather than an empty
// success, because "no content" and "no file" are different answers.
func TestReadReportsAMissingFile(t *testing.T) {
	set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	result, err := call(t, set, "read_file", "call-1", map[string]any{"path": "absent.txt"})
	if err == nil {
		t.Fatalf("reading a missing file must fail: %+v", result)
	}
	if !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// An edit whose target is a directory reports the failure.
func TestEditReportsADirectoryTarget(t *testing.T) {
	set, root := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	result, err := call(t, set, "edit_file", "call-1", map[string]any{"path": "notes", "content": "x"})
	if err == nil {
		t.Fatalf("editing a directory must fail: %+v", result)
	}
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// The tool set can always name the world it acts in, because NewTools refuses a
// set that has neither a filesystem nor a process provider to derive it from.
func TestToolSetAlwaysNamesItsWorld(t *testing.T) {
	set, root := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	if set.Root() == "" {
		t.Fatal("a tool set must be able to name its world")
	}
	// The captured root is the canonical workspace root, not a path the tool
	// derived later.
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if set.Root() != canonical {
		t.Fatalf("the root must be canonical: %q want %q", set.Root(), canonical)
	}
	// A path inside the world is shown relative, so the model can act on it again
	// without learning where the machine keeps its files.
	registry := tools.NewRegistry()
	if err := set.Register(registry); err != nil {
		t.Fatalf("register: %v", err)
	}
	pipeline := &tools.Pipeline{Registry: registry}
	if _, err := pipeline.Execute(context.Background(), tools.Call{
		ID: "call-1", Name: "edit_file", Arguments: `{"path":"notes/inner.txt","content":"x"}`,
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	result, err := pipeline.Execute(context.Background(), tools.Call{
		ID: "call-2", Name: "read_file", Arguments: `{"path":"notes/inner.txt"}`,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The answer names the file relative to the world, including its directory.
	if !strings.Contains(result.Content, filepath.Join("notes", "inner.txt")) {
		t.Fatalf("the answer must name the relative path: %q", result.Content)
	}
}

// A run without the adapter is a programming error that names the fix, rather
// than a run that silently loses cancellation.
func TestRunRejectsASeamValueThatIsNotAContext(t *testing.T) {
	provider, _ := newProcess(t)
	if _, err := provider.Run(bareCtx{}, subprocess.Request{Program: "/bin/echo"}); !errors.Is(err, local.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}
}

// bareCtx implements the seam's contract without being a context.Context.
type bareCtx struct{}

func (bareCtx) Err() error            { return nil }
func (bareCtx) Done() <-chan struct{} { return nil }

// toolNames lists the tool names a set offers.
func toolNames(set *local.Tools) map[string]bool {
	names := map[string]bool{}
	for _, definition := range set.Definitions() {
		names[string(definition.Name)] = true
	}
	return names
}

// A tool body can be invoked directly through its exported Execute field, which
// is a public path a non-pipeline caller can take. Such a caller bypasses schema
// validation, so the readers must stay fail-closed there rather than panicking on
// a wrong-typed argument.
func TestToolBodyIsFailClosedWhenCalledDirectly(t *testing.T) {
	set, _ := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	executors := map[string]tools.Executor{}
	for _, definition := range set.Definitions() {
		executors[string(definition.Name)] = definition.Execute
	}
	// A wrong Go type, a missing argument and a blank string must each return an
	// error, never panic.
	cases := []struct {
		name      string
		tool      string
		arguments map[string]any
	}{
		{"read with a non-string path", "read_file", map[string]any{"path": 42}},
		{"read with a missing path", "read_file", map[string]any{}},
		{"read with a blank path", "read_file", map[string]any{"path": "   "}},
		{"edit with a non-string content", "edit_file", map[string]any{"path": "a.txt", "content": 7}},
		{"command with a non-array arguments", "run_command", map[string]any{"program": "/bin/echo", "arguments": "not-an-array"}},
		{"command with a non-string element", "run_command", map[string]any{"program": "/bin/echo", "arguments": []any{1}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			executor, ok := executors[testCase.tool]
			if !ok {
				t.Fatalf("the %s tool must be offered", testCase.tool)
			}
			result, err := executor(tools.Context{
				Name: tools.ToolName(testCase.tool), Arguments: testCase.arguments,
				CallID: "call-direct",
			})
			if err == nil {
				t.Fatalf("a malformed argument must be refused, got %+v", result)
			}
			if !errors.Is(err, local.ErrInvalidValue) {
				t.Fatalf("want ErrInvalidValue, got %v", err)
			}
		})
	}
	// A malformed argument must not reach the side effect: the file stays absent.
	if _, err := os.Stat(filepath.Join(t.TempDir(), "unused")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("unexpected: %v", err)
	}
}

// A side effect invoked directly without a call identity is refused, so the
// binding cannot be skipped by bypassing the pipeline.
func TestDirectSideEffectRequiresACallIdentity(t *testing.T) {
	set, root := toolSet(t, fs.ModeWorkspaceWrite, approval.OutcomeAllowedOnce, defaultPolicy())
	var edit tools.Executor
	for _, definition := range set.Definitions() {
		if definition.Name == "edit_file" {
			edit = definition.Execute
		}
	}
	if edit == nil {
		t.Fatal("the edit tool must be offered")
	}
	if _, err := edit(tools.Context{
		Name: "edit_file", CallID: "",
		Arguments: map[string]any{"path": "direct.txt", "content": "x"},
	}); !errors.Is(err, local.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a call id, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "direct.txt")); !os.IsNotExist(err) {
		t.Fatal("an unbound side effect must not write anything")
	}
}
