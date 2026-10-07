package testkit

import (
	"encoding/json"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// runSteps executes scenario steps in order against the Go runtime.
func runSteps(scenario *Scenario, env replayEnv) error {
	for index, step := range scenario.Steps {
		if err := runStep(step, env); err != nil {
			return fmt.Errorf("%s step %d (%s): %w", scenario.ID, index+1, step.Op, err)
		}
	}
	return nil
}

func runStep(step Step, env replayEnv) error {
	switch step.Op {
	case "register":
		return registerNode(step, env)
	case "settle":
		if err := env.settle(); err != nil && !*env.reportedCleanupError {
			// Cleanup failures surface through Wait in Go; the reference only
			// logs them. Record the first report only, so a failure already
			// shown by an earlier close operation is not duplicated.
			*env.reportedCleanupError = true
			env.note("settle error=cleanup-failed")
		}
		return nil
	case "observe":
		return observe(step, env)
	case "setReady":
		env.ready[step.As] = derefBool(step.Ready)
		return nil
	case "notify":
		// Refresh re-evaluates conditional providers; the reference names the
		// same operation reflect.notify(names).
		env.root.Refresh()
		return env.settle()
	case "dispose":
		fiber := env.nodes[step.As]
		if fiber == nil {
			return fmt.Errorf("dispose unknown node %s", step.As)
		}
		if err := fiber.Dispose(); err != nil {
			return err
		}
		env.note("disposed %s", step.As)
		return env.settle()
	case "close":
		deadline, cancel := closeDeadline()
		defer cancel()
		if err := env.root.Close(deadline); err != nil {
			// Cleanup failures surface to the caller in Go and are reported as
			// an accumulated error. The reference only logs them. The scenario
			// must declare that divergence; it is never silently normalized.
			*env.reportedCleanupError = true
			env.note("close error=cleanup-failed")
			return nil
		}
		env.note("close ok")
		return nil
	case "emit", "serial", "bail", "waterfall":
		return dispatch(step, env)
	case "update":
		return updateNode(step, env)
	default:
		return fmt.Errorf("unknown op %q", step.Op)
	}
}

func registerNode(step Step, env replayEnv) error {
	var config any
	if len(step.Config) > 0 {
		if err := json.Unmarshal(step.Config, &config); err != nil {
			return fmt.Errorf("config for %s: %w", step.As, err)
		}
	}
	plugin := cordis.Plugin{
		Name:   step.Name,
		Inject: append([]string(nil), step.Inject...),
		Apply:  applyFor(step, env),
	}
	if step.Validate {
		plugin.Validate = validateParityConfig
	}
	fiber, err := env.root.Plugin(plugin, config)
	if err != nil {
		return err
	}
	env.nodes[step.As] = fiber
	env.register(step.As)
	return env.settle()
}

// applyFor builds the plugin body described by a register step.
func applyFor(step Step, env replayEnv) func(*cordis.Context, any) (cordis.Cleanup, error) {
	return func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		env.note("apply %s", step.As)
		for _, name := range step.Provides {
			if _, err := provideService(ctx, step, name, env); err != nil {
				return nil, err
			}
		}
		for _, label := range step.Cleanups {
			label := label
			if _, err := ctx.OnDispose(label, func() error {
				env.note("cleanup %s:%s", step.As, label)
				if label == "panic" {
					return fmt.Errorf("cleanup panic")
				}
				return nil
			}); err != nil {
				return nil, err
			}
		}
		for _, listener := range step.Listeners {
			if err := registerListener(ctx, step, listener, env); err != nil {
				return nil, err
			}
		}
		if step.SelfDispose {
			_ = ctx.Fiber().Dispose()
		}
		return func() error {
			env.note("cleanup %s:returned", step.As)
			return nil
		}, nil
	}
}

// provideService publishes one service. A scenario that omits ready declares an
// unconditionally available service, matching the reference runner; only a
// scenario that sets ready installs a conditional predicate.
func provideService(ctx *cordis.Context, step Step, name string, env replayEnv) (cordis.Cleanup, error) {
	if step.Ready == nil {
		return ctx.Provide(name, name)
	}
	node := step.As
	return ctx.ProvideWhen(name, name, func() bool { return env.ready[node] })
}

func registerListener(ctx *cordis.Context, step Step, listener Listener, env replayEnv) error {
	seq, kind, value := listener.Seq, listener.Kind, listener.Value
	_, err := ctx.On(listener.Event, func(cordis.Event, cordis.Next) (any, error) {
		if kind == "bail" {
			env.note("listener %s:%d:bail=%s", step.As, seq, value)
			return value, nil
		}
		env.note("listener %s:%d", step.As, seq)
		return nil, nil
	}, cordis.EventOptions{Prepend: listener.Prepend})
	return err
}

func observe(step Step, env replayEnv) error {
	env.note("observe %s", step.Note)
	// Node order is fixed by the scenario's registration order, which both
	// runners preserve, so the trace stays comparable.
	for _, name := range env.nodeOrder() {
		env.note("state %s=%s", name, nodeState(env.nodes[name]))
	}
	return nil
}

func dispatch(step Step, env replayEnv) error {
	dispatcher := env.root.Events()
	switch step.Op {
	case "emit":
		return dispatcher.Emit(step.Event, "x")
	case "serial":
		_, err := dispatcher.Serial(step.Event, "x")
		return err
	case "bail":
		_, err := dispatcher.Bail(step.Event, "x")
		return err
	default:
		_, err := dispatcher.Waterfall(step.Event, func() (any, error) { return "default", nil }, "x")
		return err
	}
}

func updateNode(step Step, env replayEnv) error {
	fiber := env.nodes[step.As]
	if fiber == nil {
		return fmt.Errorf("update unknown node %s", step.As)
	}
	var config any
	if err := json.Unmarshal(step.Config, &config); err != nil {
		return err
	}
	if err := fiber.Update(config); err != nil {
		env.note("update %s=rejected", step.As)
		return nil
	}
	env.note("update %s=ok", step.As)
	return env.settle()
}

// validateParityConfig mirrors the reference scenario's validator: a config
// whose n value starts with "good" is accepted.
func validateParityConfig(config any) (any, error) {
	object, ok := config.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("config must be an object")
	}
	value, _ := object["n"].(string)
	if !hasGoodPrefix(value) {
		return nil, fmt.Errorf("n must start with good")
	}
	return config, nil
}

func hasGoodPrefix(value string) bool {
	const prefix = "good"
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func derefBool(value *bool) bool { return value != nil && *value }
