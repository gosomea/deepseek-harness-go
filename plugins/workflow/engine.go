// engine.go holds the fake workflow engine implementation.
package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/gosomea/deepseek-harness-go/capability/workflow"
)

// Options configures a fake engine.
type Options struct {
	// ID is the route key. Defaults to "fake".
	ID string
	// Name is the diagnostics name. Defaults to "Fake Workflow Engine".
	Name string
	// Capabilities lists the script capabilities the engine declares. A caller
	// that runs a script with another capability gets a refusal.
	Capabilities []string
}

// Engine is a deterministic fake workflow engine.
type Engine struct {
	info workflow.ProviderInfo
	// stepTrace, when set, is appended to for each step, so a test can observe
	// that each step ran once in order.
	onStep func(step string)
}

// New returns a fake engine.
func New(options Options) *Engine {
	id := options.ID
	if id == "" {
		id = "fake"
	}
	name := options.Name
	if name == "" {
		name = "Fake Workflow Engine"
	}
	caps := options.Capabilities
	if caps == nil {
		caps = []string{"two-step-delegation"}
	}
	return &Engine{info: workflow.ProviderInfo{ID: id, Name: name, Capabilities: caps}}
}

// OnStep installs a callback run after each step, for observation in tests.
func (e *Engine) OnStep(callback func(step string)) {
	if e == nil {
		return
	}
	e.onStep = callback
}

// Info returns the engine's declared route.
func (e *Engine) Info() workflow.ProviderInfo { return e.info }

// Run validates the request and executes its steps in order.
//
// Validation (parent required, capability declared, args match meta) happens
// before any step runs, so a rejected request never produces a partial trace.
func (e *Engine) Run(ctx context.Context, request workflow.Request) (workflow.Result, error) {
	if err := request.Validate(); err != nil {
		return workflow.Result{}, err
	}
	if !e.info.Supports(request.Script.Capability) {
		return workflow.Result{}, fmt.Errorf("%w: %q", workflow.ErrUnknownCapability, request.Script.Capability)
	}
	result := workflow.Result{ScriptID: request.Script.ID}
	for _, step := range request.Script.Steps {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("workflow: cancelled before step %q: %w", step, err)
		}
		result.Trace = append(result.Trace, "step:"+step)
		if e.onStep != nil {
			e.onStep(step)
		}
	}
	result.Completed = true
	return result, nil
}

// TraceString renders a result's trace as a comma-separated line.
func TraceString(result workflow.Result) string {
	return strings.Join(result.Trace, ",")
}

// compile-time assertion that Engine satisfies the seam.
var _ workflow.Provider = (*Engine)(nil)
