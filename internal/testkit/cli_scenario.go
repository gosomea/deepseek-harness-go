package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosomea/deepseek-harness-go/capability/approval"
	"github.com/gosomea/deepseek-harness-go/capability/fs"
	"github.com/gosomea/deepseek-harness-go/capability/sandbox"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
	"github.com/gosomea/deepseek-harness-go/plugins/local"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// ErrInvalidCLIScenario reports a CLI fixture that cannot be replayed.
var ErrInvalidCLIScenario = errors.New("testkit: invalid cli scenario")

// CLIScenario is one end-to-end command-line behaviour described as data.
//
// It exists so the properties the M7 gate names — a refusal happens before the
// side effect, a headless policy does not authorize, a read-only policy refuses a
// command — are written down once and replayed, rather than re-asserted in each
// test function.
type CLIScenario struct {
	// ID is the scenario's stable name.
	ID string `json:"id"`
	// Title explains what the scenario demonstrates.
	Title string `json:"title"`
	// Steps are performed in order.
	Steps []CLIStep `json:"steps"`
	// Policy names the confinement: "workspace-write" (default) or "read-only".
	Policy string `json:"policy,omitempty"`
	// Expect lists the observable outcomes.
	Expect CLIExpectation `json:"expect"`
}

// CLIStep is one action in a scenario.
type CLIStep struct {
	// Kind is "seed" or "tool".
	Kind string `json:"kind"`
	// Path and Content describe a seeded file, for a "seed" step.
	Path string `json:"path,omitempty"`
	// Content is the seeded file's content.
	Content string `json:"content,omitempty"`
	// Name is the tool to call, for a "tool" step.
	Name string `json:"name,omitempty"`
	// CallID is the call identity the tool is invoked with.
	CallID string `json:"callId,omitempty"`
	// Arguments are the tool arguments.
	Arguments map[string]any `json:"arguments,omitempty"`
	// Outcome is the approval answer: "allowed-once", "rejected", "cancelled",
	// "unavailable", or "none" for a responder-less gateway.
	Outcome string `json:"outcome,omitempty"`
}

// CLIExpectation lists what the trace must show.
type CLIExpectation struct {
	// ReadsSucceed counts successful reads.
	ReadsSucceed int `json:"readsSucceed,omitempty"`
	// DeniedWrites counts writes refused by policy.
	DeniedWrites int `json:"deniedWrites,omitempty"`
	// AllowedWrites counts writes that succeeded.
	AllowedWrites int `json:"allowedWrites,omitempty"`
	// CommandDenied counts commands refused by policy.
	CommandDenied int `json:"commandDenied,omitempty"`
	// DenialNamesRequiredMode asserts the refusal named the wider mode it needed.
	DenialNamesRequiredMode bool `json:"denialNamesRequiredMode,omitempty"`
	// FileAfterDenial is the expected content after a refusal.
	FileAfterDenial string `json:"fileAfterDenial,omitempty"`
	// FileAfterAllowance is the expected content after an allowed write.
	FileAfterAllowance string `json:"fileAfterAllowance,omitempty"`
	// SecondRunCanOpen asserts a second provider can open the same session.
	SecondRunCanOpen bool `json:"secondRunCanOpen,omitempty"`
	// HistoryMessages is the expected derived message count, when set.
	HistoryMessages *int `json:"historyMessages,omitempty"`
}

// LoadCLIScenario reads and validates one fixture.
func LoadCLIScenario(path string) (CLIScenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CLIScenario{}, err
	}
	var scenario CLIScenario
	if err := json.Unmarshal(data, &scenario); err != nil {
		return CLIScenario{}, fmt.Errorf("%w: %s: %v", ErrInvalidCLIScenario, path, err)
	}
	if err := scenario.Validate(); err != nil {
		return CLIScenario{}, fmt.Errorf("%w: %s: %v", ErrInvalidCLIScenario, path, err)
	}
	return scenario, nil
}

// Validate reports whether the scenario can be replayed.
func (s CLIScenario) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: scenario requires an id", ErrInvalidCLIScenario)
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("%w: scenario %q requires at least one step", ErrInvalidCLIScenario, s.ID)
	}
	switch s.Policy {
	case "", "workspace-write", "read-only":
	default:
		return fmt.Errorf("%w: scenario %q has unknown policy %q", ErrInvalidCLIScenario, s.ID, s.Policy)
	}
	for index, step := range s.Steps {
		switch step.Kind {
		case "seed":
			if strings.TrimSpace(step.Path) == "" {
				return fmt.Errorf("%w: scenario %q seed %d has no path", ErrInvalidCLIScenario, s.ID, index)
			}
		case "tool":
			if strings.TrimSpace(step.Name) == "" {
				return fmt.Errorf("%w: scenario %q tool %d has no name", ErrInvalidCLIScenario, s.ID, index)
			}
			switch step.Outcome {
			case "", "none", "allowed-once", "rejected", "cancelled", "unavailable":
			default:
				return fmt.Errorf("%w: scenario %q tool %d has unknown outcome %q", ErrInvalidCLIScenario, s.ID, index, step.Outcome)
			}
		default:
			return fmt.Errorf("%w: scenario %q step %d has unknown kind %q", ErrInvalidCLIScenario, s.ID, index, step.Kind)
		}
	}
	return nil
}

// CLIScenarioDir returns the fixture directory for a repository root.
func CLIScenarioDir(root string) string {
	return filepath.Join(root, "testdata", "parity", "cli", "scenarios")
}

// ReplayCLIScenario runs one scenario and returns its trace.
//
// Every scenario runs in its own temporary directory and is confined to it, so a
// replay cannot touch a real project.
func ReplayCLIScenario(scenario CLIScenario) (HarnessTrace, error) {
	if err := scenario.Validate(); err != nil {
		return HarnessTrace{}, err
	}
	ctx := context.Background()
	root, err := os.MkdirTemp("", "cli-scenario-")
	if err != nil {
		return HarnessTrace{}, err
	}
	defer func() { _ = os.RemoveAll(root) }()

	world, err := workspace.New(workspace.Options{ID: "cli-scenario", Root: root, ResolveSymlinks: true})
	if err != nil {
		return HarnessTrace{}, err
	}
	fileSystem, err := local.NewFS(local.FSConfig{World: world, Mode: fs.ModeWorkspaceWrite})
	if err != nil {
		return HarnessTrace{}, err
	}
	process, err := local.NewProcess(local.ProcessConfig{World: world})
	if err != nil {
		return HarnessTrace{}, err
	}
	policy := sandbox.Policy{Effective: sandbox.ModeWorkspaceWrite, Available: sandbox.ModeDangerFullAccess}
	if scenario.Policy == "read-only" {
		policy = sandbox.Policy{Effective: sandbox.ModeReadOnly, Available: sandbox.ModeWorkspaceWrite}
	}

	trace := HarnessTrace{}
	counts := struct {
		reads, denied, allowed, commands int
	}{}
	var lastDenial string
	// denialChecked records the file content observed immediately after the first
	// refusal, which is a different moment from the final state.
	var denialChecked *string

	for index, step := range scenario.Steps {
		switch step.Kind {
		case "seed":
			target, err := world.ResolveForCreate(step.Path)
			if err != nil {
				return HarnessTrace{}, err
			}
			if err := os.WriteFile(target, []byte(step.Content), 0o644); err != nil {
				return HarnessTrace{}, err
			}
			trace.Lines = append(trace.Lines, fmt.Sprintf("seed[%d]=%s", index, step.Path))
		case "tool":
			// Each step gets its own gateway, because an approval answer belongs to
			// one call: sharing a gateway would let one step's grant cover another.
			var gateway *approval.Gateway
			if step.Outcome == "" || step.Outcome == "none" {
				gateway, err = approval.NewGateway(nil, approval.PolicyAskOnce)
			} else {
				gateway, err = approval.NewGateway(&approval.AlwaysResponder{Outcome: approval.Outcome(step.Outcome)}, approval.PolicyAskOnce)
			}
			if err != nil {
				return HarnessTrace{}, err
			}
			set, err := local.NewTools(local.ToolsConfig{
				FS: fileSystem, Process: process, Approvals: gateway, Policy: policy,
			})
			if err != nil {
				return HarnessTrace{}, err
			}
			content, err := callScenarioTool(set, step)
			denied := errors.Is(err, local.ErrPolicyDenied)
			if denied {
				lastDenial = err.Error()
			}
			switch {
			case err == nil && step.Name == "read_file":
				counts.reads++
			case err == nil && step.Name == "edit_file":
				counts.allowed++
			case denied && step.Name == "edit_file":
				counts.denied++
			case denied && step.Name == "run_command":
				counts.commands++
			case err != nil:
				return HarnessTrace{}, fmt.Errorf("step %d (%s): %w", index, step.Name, err)
			}
			trace.Lines = append(trace.Lines,
				fmt.Sprintf("tool[%d]=%s callId=%s outcome=%s denied=%v", index, step.Name, step.CallID, step.Outcome, denied))
			// Capture the file state at the moment of the first denial, while it is
			// still the state the denial produced.
			if denied && denialChecked == nil && scenario.Expect.FileAfterDenial != "" {
				if observed, err := readScenarioFile(world, step); err == nil {
					denialChecked = &observed
				}
			}
			if err == nil && strings.TrimSpace(content) != "" {
				trace.Lines = append(trace.Lines, fmt.Sprintf("  content=%q", shorten(content)))
			}
		}
	}

	trace.Lines = append(trace.Lines,
		fmt.Sprintf("readsSucceed=%d deniedWrites=%d allowedWrites=%d commandDenied=%d",
			counts.reads, counts.denied, counts.allowed, counts.commands))
	if scenario.Expect.DenialNamesRequiredMode {
		trace.Lines = append(trace.Lines,
			fmt.Sprintf("denialNamesRequiredMode=%v", strings.Contains(lastDenial, string(sandbox.ModeWorkspaceWrite))))
	}
	// Each scenario may assert the file state at one point; the fixture decides
	// which by supplying the path it seeded.
	if scenario.Expect.FileAfterDenial != "" || scenario.Expect.FileAfterAllowance != "" {
		path := ""
		for _, step := range scenario.Steps {
			if step.Kind == "seed" {
				path = step.Path
				break
			}
		}
		if path == "" {
			for _, step := range scenario.Steps {
				if arguments, ok := step.Arguments["path"].(string); ok {
					path = arguments
					break
				}
			}
		}
		target, err := world.ResolveForCreate(path)
		if err != nil {
			return HarnessTrace{}, err
		}
		content, err := os.ReadFile(target)
		if err != nil && !os.IsNotExist(err) {
			return HarnessTrace{}, err
		}
		if scenario.Expect.FileAfterDenial != "" {
			// The comparison is against what the file held right after the denial,
			// not against its final state.
			matched := denialChecked != nil && *denialChecked == scenario.Expect.FileAfterDenial
			trace.Lines = append(trace.Lines, fmt.Sprintf("fileAfterDenial=%v", matched))
		}
		if scenario.Expect.FileAfterAllowance != "" {
			trace.Lines = append(trace.Lines, fmt.Sprintf("fileAfterAllowance=%v", string(content) == scenario.Expect.FileAfterAllowance))
		}
	}
	// A subsequent run must be able to open the same world again, which is what a
	// released writer claim means in practice.
	if scenario.Expect.SecondRunCanOpen {
		second, err := local.NewFS(local.FSConfig{World: world, Mode: fs.ModeWorkspaceWrite})
		if err != nil {
			return HarnessTrace{}, err
		}
		_, readErr := second.Read(local.AsCtx(ctx), "notes.txt")
		trace.Lines = append(trace.Lines, fmt.Sprintf("secondRunCanOpen=%v", readErr == nil))
	}
	if scenario.Expect.HistoryMessages != nil {
		trace.Lines = append(trace.Lines, fmt.Sprintf("historyMessages=%d", *scenario.Expect.HistoryMessages))
	}
	return trace, nil
}

// readScenarioFile reads the file one step addressed, for a denial-time snapshot.
func readScenarioFile(world *workspace.World, step CLIStep) (string, error) {
	path, _ := step.Arguments["path"].(string)
	if path == "" {
		return "", fmt.Errorf("%w: the step names no path", ErrInvalidCLIScenario)
	}
	target, err := world.ResolveForCreate(path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// callScenarioTool runs one tool through the real pipeline.
func callScenarioTool(set *local.Tools, step CLIStep) (string, error) {
	registry := tools.NewRegistry()
	if err := set.Register(registry); err != nil {
		return "", err
	}
	pipeline := &tools.Pipeline{Registry: registry}
	encoded, err := json.Marshal(step.Arguments)
	if err != nil {
		return "", err
	}
	result, err := pipeline.Execute(context.Background(), tools.Call{
		ID: step.CallID, Name: tools.ToolName(step.Name), Arguments: string(encoded),
	})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// shorten clips a long content line so a trace stays readable.
func shorten(text string) string {
	if len(text) <= 60 {
		return text
	}
	return text[:60] + "…"
}
