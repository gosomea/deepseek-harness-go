package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// ErrHarnessConfig reports an Assembly that cannot start a Harness.
var ErrHarnessConfig = errors.New("app: invalid harness configuration")

// HarnessConfig describes the collaborators of one minimal Harness.
//
// It exists so that starting an Agent is an application-level decision: the
// application chooses the provider, the log and the tool pipeline, and the
// driver is assembled from them. Naming the parts here is what keeps the
// wiring visible instead of hidden in a package-level default.
type HarnessConfig struct {
	// Provider generates model responses. Required.
	Provider llm.Provider
	// Pipeline executes tool calls. Optional: an Agent with no tools is valid.
	Pipeline *tools.Pipeline
	// SystemPrompt renders the prompt for one scope. Optional.
	SystemPrompt func(scope string) (string, error)
	// Tools returns the tool definitions visible in one scope. Optional.
	Tools func(scope string) []tools.Definition
	// MaxSteps bounds one turn's steps. Zero uses the driver's default.
	MaxSteps int
}

// Harness is one assembled, ready-to-run Agent plus what it was assembled from.
//
// It owns the session log and the factory, and it is the object an application
// disposes when it shuts down: releasing the Harness releases every Agent it
// created, in reverse creation order.
type Harness struct {
	// Log is the authoritative session log this Harness records into.
	Log *session.Log
	// Factory owns the Agents this Harness created.
	Factory *agentloop.Factory
	// Registry resolves the installed driver.
	Registry *agent.Registry
	// Scope is the Agent scope every Agent of this Harness belongs to.
	Scope string
	// providerName is the installed provider's route, captured at assembly so
	// Start can name the route an Agent's model messages come from.
	providerName string
	// providerModel is the installed provider's default model, when it names one.
	providerModel string
}

// NewHarness assembles a minimal Harness from the configuration.
//
// It fails before creating anything when the configuration is incomplete, so a
// misconfigured application never reaches the point of owning half a Harness.
// The session log is created here with the real clock; a deterministic caller
// (a test or an example) supplies its own log through NewHarnessWithLog.
func NewHarness(config HarnessConfig) (*Harness, error) {
	log := session.NewLog(session.KnownTypes())
	return NewHarnessWithLog(config, log, "main")
}

// NewHarnessWithLog assembles a Harness over a caller-supplied log and scope.
//
// The log and the scope are parameters rather than fields of the configuration
// because they are the two things a deterministic caller must control: a test
// replaces the clock on the log, and a test names the scope whose tools it
// expects.
func NewHarnessWithLog(config HarnessConfig, log *session.Log, scope string) (*Harness, error) {
	if config.Provider == nil {
		return nil, fmt.Errorf("%w: a model provider is required", ErrHarnessConfig)
	}
	if log == nil {
		return nil, fmt.Errorf("%w: a session log is required", ErrHarnessConfig)
	}
	if scope == "" {
		return nil, fmt.Errorf("%w: a scope is required", ErrHarnessConfig)
	}
	factory, err := agentloop.NewFactory(agentloop.Config{
		Provider:        config.Provider,
		Log:             log,
		Pipeline:        config.Pipeline,
		SystemFragments: agentloop.NewFragmentSource(config.SystemPrompt, config.Tools),
		MaxSteps:        config.MaxSteps,
	})
	if err != nil {
		return nil, err
	}
	registry := agent.NewRegistry()
	if err := factory.Install(registry); err != nil {
		return nil, err
	}
	info := config.Provider.Info()
	return &Harness{
		Log: log, Factory: factory, Registry: registry, Scope: scope,
		providerName: info.ID, providerModel: info.Name,
	}, nil
}

// Start creates one Agent in this Harness's scope.
//
// The returned Owner is the only capability that can dispose the Agent; the
// Harness also owns it, so Close releases whatever the caller forgets.
func (h *Harness) Start(ctx context.Context, spec agent.Spec) (agent.Owner, error) {
	if h == nil || h.Registry == nil {
		return nil, fmt.Errorf("%w: harness is nil or not assembled", ErrHarnessConfig)
	}
	if spec.Scope == "" {
		spec.Scope = agent.ScopeKey(h.Scope)
	}
	if spec.SessionID == "" {
		spec.SessionID = h.Scope
	}
	// The route and model must be recorded on the assistant messages this Agent
	// produces, so they cannot be left empty in the spec: an assistant message
	// without a model source is refused by the message vocabulary.
	if spec.Provider == "" {
		spec.Provider = h.providerName
	}
	if spec.Model == "" {
		spec.Model = h.providerModel
	}
	return h.Registry.Create(ctx, spec)
}

// RunTurn drives one turn of an Agent created by this Harness.
//
// It exists so a caller does not have to reach for the driver's runner
// interface: the Harness already knows which driver it installed.
func (h *Harness) RunTurn(ctx context.Context, handle agent.Handle) error {
	runner, ok := agentloop.AsRunner(handle)
	if !ok {
		return fmt.Errorf("%w: handle is not runnable by this harness", ErrHarnessConfig)
	}
	return runner.RunTurn(ctx)
}

// Close disposes every Agent this Harness created and closes the registry.
//
// It is idempotent, and it releases the Agents before the registry so a
// concurrent Start cannot slip into a registry that is already being torn down.
func (h *Harness) Close(ctx context.Context) error {
	if h == nil || h.Registry == nil {
		return nil
	}
	return h.Registry.Close(ctx)
}
