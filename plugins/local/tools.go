package local

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gosomea/deepseek-harness-go/capability/approval"
	"github.com/gosomea/deepseek-harness-go/capability/fs"
	"github.com/gosomea/deepseek-harness-go/capability/sandbox"
	"github.com/gosomea/deepseek-harness-go/capability/subprocess"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// ErrPolicyDenied reports that a side effect was refused before it happened.
//
// It is the error a tool returns when policy said no. The pipeline turns it into
// a normalized result for the model, so a denied call is a visible fact rather
// than a silent no-op.
var ErrPolicyDenied = errors.New("local: the action was not authorized")

// Tools registers read, edit and shell tools over a workspace world.
//
// Every tool that can change something goes through the approval channel before
// it touches the filesystem or starts a process. The read-only tool does not ask,
// because a read has no side effect to authorize.
type Tools struct {
	// fs reads and writes inside the world.
	fs *OSFS
	// process runs commands.
	process *OSProcess
	// approvals asks about side effects.
	approvals *approval.Gateway
	// policy is the confinement in force.
	policy sandbox.Policy
	// root is the workspace root, captured at construction.
	//
	// It is captured rather than looked up per call because the constructor
	// refuses a set with no provider to derive it from: a tool set that cannot
	// name its world could not safely act in one.
	root string
}

// ToolsConfig configures the tool set.
type ToolsConfig struct {
	// FS is the filesystem provider. Required for the file tools.
	FS *OSFS
	// Process is the process provider. Required for the shell tool.
	Process *OSProcess
	// Approvals asks about side effects. Required.
	Approvals *approval.Gateway
	// Policy is the confinement in force.
	Policy sandbox.Policy
}

// NewTools builds the tool set.
//
// It validates the confinement policy first: a policy that cannot be enforced is
// a configuration error, and discovering that on the first write would be
// discovering it too late.
func NewTools(config ToolsConfig) (*Tools, error) {
	if config.Approvals == nil {
		return nil, fmt.Errorf("%w: an approval channel is required", ErrInvalidValue)
	}
	if err := config.Policy.Validate(); err != nil {
		return nil, err
	}
	if config.FS == nil && config.Process == nil {
		// A set with neither provider would offer the read tool with nothing to
		// read through, so the failure is refused here rather than surfacing as a
		// nil dereference on the first call.
		return nil, fmt.Errorf("%w: a tool set needs a filesystem or a process provider", ErrInvalidValue)
	}
	root := ""
	if config.FS != nil && config.FS.Workspace() != nil {
		root = config.FS.Workspace().Root()
	} else if config.Process != nil {
		root = config.Process.worldRoot()
	}
	return &Tools{
		fs: config.FS, process: config.Process,
		approvals: config.Approvals, policy: config.Policy, root: root,
	}, nil
}

// Definitions returns the tool definitions to register.
func (t *Tools) Definitions() []tools.Definition {
	var definitions []tools.Definition
	if t.fs != nil {
		definitions = append(definitions, t.readFileDefinition())
	}
	if t.fs != nil && t.fs.Mode().CanWrite() {
		definitions = append(definitions, t.editFileDefinition())
	}
	if t.process != nil {
		definitions = append(definitions, t.shellDefinition())
	}
	return definitions
}

// Register adds every definition to a registry.
func (t *Tools) Register(registry *tools.Registry) error {
	if registry == nil {
		return fmt.Errorf("%w: a registry is required", ErrInvalidValue)
	}
	for _, definition := range t.Definitions() {
		if err := registry.Register(definition); err != nil {
			return err
		}
	}
	return nil
}

// readFileDefinition describes the read-only tool.
func (t *Tools) readFileDefinition() tools.Definition {
	return tools.Definition{
		Name: "read_file", Description: "Read a file from the workspace.",
		Schema:   pathSchema("path", "Path of the file, relative to the workspace root."),
		ReadOnly: true, Owner: "plugins/local",
		Execute: func(execution tools.Context) (tools.Result, error) {
			return t.readFile(execution)
		},
	}
}

// editFileDefinition describes the writing tool.
func (t *Tools) editFileDefinition() tools.Definition {
	schema := pathSchema("path", "Path of the file, relative to the workspace root.")
	schema.Properties["content"] = tools.PropertySchema{Type: tools.SchemaTypeString, Description: "The complete new content of the file."}
	schema.Required = []string{"path", "content"}
	return tools.Definition{
		Name: "edit_file", Description: "Replace the contents of a file in the workspace.",
		Schema: schema, ReadOnly: false, Owner: "plugins/local",
		Execute: func(execution tools.Context) (tools.Result, error) {
			return t.editFile(execution)
		},
	}
}

// shellDefinition describes the command tool.
func (t *Tools) shellDefinition() tools.Definition {
	schema := tools.InputSchema{
		Type: tools.SchemaTypeObject,
		Properties: map[string]tools.PropertySchema{
			"program": {Type: tools.SchemaTypeString, Description: "The executable to run, without a shell."},
			"arguments": {
				Type: tools.SchemaTypeArray, Description: "Arguments, each passed as one argv element.",
				Items: &tools.PropertySchema{Type: tools.SchemaTypeString},
			},
		},
		Required: []string{"program"},
	}
	return tools.Definition{
		Name: "run_command", Description: "Run one command in the workspace.",
		Schema: schema, ReadOnly: false, Owner: "plugins/local",
		Execute: func(execution tools.Context) (tools.Result, error) {
			return t.runCommand(context.Background(), execution)
		},
	}
}

// pathSchema builds a one-path argument schema.
func pathSchema(name, description string) tools.InputSchema {
	return tools.InputSchema{
		Type: tools.SchemaTypeObject,
		Properties: map[string]tools.PropertySchema{
			name: {Type: tools.SchemaTypeString, Description: description},
		},
		Required: []string{name},
	}
}

// readFile performs the read-only tool's work.
func (t *Tools) readFile(execution tools.Context) (tools.Result, error) {
	filePath, err := argumentString(execution, "path")
	if err != nil {
		return tools.Result{}, err
	}
	ctx := AsCtx(context.Background())
	result, err := t.fs.Read(ctx, filePath)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Content: fmt.Sprintf("%s\n%s", t.displayPath(result.Path), string(result.Content)),
	}, nil
}

// editFile asks first, then writes.
//
// The order is the whole point of this tool: the approval request is bound to the
// call id, and only an allowed-once answer reaches the filesystem. A denied,
// cancelled or unanswered request returns ErrPolicyDenied and writes nothing.
func (t *Tools) editFile(execution tools.Context) (tools.Result, error) {
	filePath, err := argumentString(execution, "path")
	if err != nil {
		return tools.Result{}, err
	}
	content, err := argumentString(execution, "content")
	if err != nil {
		return tools.Result{}, err
	}
	if err := t.authorize(execution, "edit_file", "write "+filePath); err != nil {
		return tools.Result{}, err
	}
	ctx := AsCtx(context.Background())
	stat, err := t.fs.Write(ctx, fs.WriteRequest{Path: filePath, Content: []byte(content)})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Content: fmt.Sprintf("wrote %d bytes to %s", stat.Size, t.displayPath(stat.Path)),
	}, nil
}

// runCommand asks first, then runs.
//
// The confinement is checked before the request: a call that would need a wider
// mode than the policy allows is refused without bothering a human about it.
func (t *Tools) runCommand(ctx context.Context, execution tools.Context) (tools.Result, error) {
	program, err := argumentString(execution, "program")
	if err != nil {
		return tools.Result{}, err
	}
	arguments, err := argumentStrings(execution, "arguments")
	if err != nil {
		return tools.Result{}, err
	}
	if err := t.policy.Validate(); err != nil {
		return tools.Result{}, err
	}
	// A command needs to write somewhere, so the policy must permit it. This is
	// the "cannot implicitly downgrade isolation" rule in practice: rather than
	// running unconfined, the call is refused.
	if t.policy.Effective == sandbox.ModeReadOnly {
		return tools.Result{}, fmt.Errorf("%w: running a command needs at least %s, the policy is %s",
			ErrPolicyDenied, sandbox.ModeWorkspaceWrite, t.policy.Effective)
	}
	if err := t.authorize(execution, "run_command", "run "+program); err != nil {
		return tools.Result{}, err
	}
	result, err := t.process.Run(AsProcessCtx(ctx), subprocess.Request{Program: program, Arguments: arguments})
	if err != nil {
		return tools.Result{}, err
	}
	summary := fmt.Sprintf("exit=%d\n%s", result.ExitCode, string(result.Stdout))
	if len(result.Stderr) > 0 {
		summary += "\nstderr:\n" + string(result.Stderr)
	}
	if result.Truncated {
		summary += "\n(output was truncated)"
	}
	return tools.Result{Content: summary, IsError: !result.Succeeded()}, nil
}

// authorize asks the approval channel about one specific side effect.
//
// Every request is bound to the call id, so a grant cannot authorize a different
// call, and the outcome is turned into an error here rather than returned as a
// value: a caller that forgot to check the outcome would otherwise proceed.
func (t *Tools) authorize(execution tools.Context, tool, action string) error {
	callID := execution.CallID
	if callID == "" {
		return fmt.Errorf("%w: a side effect must be bound to a call", ErrInvalidValue)
	}
	request := approval.Request{
		ID:     tool + ":" + callID,
		CallID: callID,
		Tool:   tool,
		Action: action,
	}
	decision, err := t.approvals.Request(context.Background(), request)
	if err != nil {
		return err
	}
	if !decision.Granted() {
		reason := decision.Reason
		if reason == "" {
			reason = string(decision.Outcome)
		}
		return fmt.Errorf("%w: %s (%s)", ErrPolicyDenied, action, reason)
	}
	return nil
}

// argumentString reads one required string argument.
//
// The value has already passed schema validation, so the only checks that remain
// are the ones validation cannot express: presence and non-blankness. A wrong Go
// type here would mean the pipeline validated a different schema than this tool
// declared, which is a programming error rather than a caller mistake — so it is
// reported as such instead of being silently coerced.
func argumentString(execution tools.Context, name string) (string, error) {
	text, ok := execution.Arguments[name].(string)
	if !ok {
		return "", fmt.Errorf("%w: the %s argument must be a string", ErrInvalidValue, name)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%w: the %s argument must not be blank", ErrInvalidValue, name)
	}
	return text, nil
}

// argumentStrings reads an optional array-of-strings argument.
func argumentStrings(execution tools.Context, name string) ([]string, error) {
	value, ok := execution.Arguments[name]
	if !ok || value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: the %s argument must be an array", ErrInvalidValue, name)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%w: every %s element must be a string", ErrInvalidValue, name)
		}
		out = append(out, text)
	}
	return out, nil
}

// displayPath shows a path relative to the workspace root.
//
// It exists so a tool's output does not leak the absolute location of the machine
// it ran on, while still naming the file precisely enough for the model to act on
// again. Returning only the base name would lose the directory, and returning the
// absolute path would publish the host's layout.
func (t *Tools) displayPath(absolute string) string {
	relative, err := filepath.Rel(t.root, absolute)
	// A path that cannot be made relative, or that climbs out of the world, is
	// shown by base name: the tool must never publish the host's layout.
	if err != nil || strings.HasPrefix(relative, "..") {
		return filepath.Base(absolute)
	}
	return relative
}

// Root reports the workspace root this tool set acts in.
func (t *Tools) Root() string { return t.root }
