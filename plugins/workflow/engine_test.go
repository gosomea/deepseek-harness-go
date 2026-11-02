package workflow_test

import (
	"context"
	"errors"
	"testing"

	seam "github.com/gosomea/deepseek-harness-go/capability/workflow"
	engine "github.com/gosomea/deepseek-harness-go/plugins/workflow"
)

func TestEngineRun(t *testing.T) {
	eng := engine.New(engine.Options{})
	var seen []string
	eng.OnStep(func(step string) { seen = append(seen, step) })
	request := seam.Request{
		Parent: "parent-1",
		Script: seam.Script{ID: "two-step", Capability: "two-step-delegation", Steps: []string{"plan", "exec"}},
	}
	result, err := eng.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Completed || len(result.Trace) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if len(seen) != 2 || seen[0] != "plan" {
		t.Fatalf("steps = %v", seen)
	}
	if engine.TraceString(result) != "step:plan,step:exec" {
		t.Fatalf("trace string = %q", engine.TraceString(result))
	}
}

func TestEngineRefusesUnknownCapability(t *testing.T) {
	eng := engine.New(engine.Options{Capabilities: []string{"known"}})
	request := seam.Request{
		Parent: "p",
		Script: seam.Script{ID: "s", Capability: "unknown", Steps: []string{"a"}},
	}
	if _, err := eng.Run(context.Background(), request); !errors.Is(err, seam.ErrUnknownCapability) {
		t.Fatalf("err = %v, want ErrUnknownCapability", err)
	}
}

func TestEngineRefusesBadArgsBeforeRunning(t *testing.T) {
	eng := engine.New(engine.Options{})
	ran := false
	eng.OnStep(func(string) { ran = true })
	request := seam.Request{
		Parent: "p",
		Script: seam.Script{ID: "s", Capability: "two-step-delegation", Args: []string{"topic"}, Steps: []string{"a"}},
		Args:   map[string]string{},
	}
	if _, err := eng.Run(context.Background(), request); err == nil {
		t.Fatal("expected arg validation error")
	}
	if ran {
		t.Fatal("steps ran despite invalid args")
	}
}

func TestEngineInfo(t *testing.T) {
	eng := engine.New(engine.Options{ID: "fake", Name: "Fake"})
	if eng.Info().ID != "fake" || eng.Info().Name != "Fake" {
		t.Fatalf("info = %+v", eng.Info())
	}
}
