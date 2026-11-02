package workflow_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/workflow"
)

func TestScriptValidate(t *testing.T) {
	ok := workflow.Script{ID: "two-step", Capability: "two-step-delegation", Args: []string{"topic"}, Steps: []string{"a", "b"}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid script rejected: %v", err)
	}
	if err := (workflow.Script{}).Validate(); !errors.Is(err, workflow.ErrInvalidScript) {
		t.Fatalf("empty err = %v", err)
	}
	if err := (workflow.Script{ID: "x"}).Validate(); !errors.Is(err, workflow.ErrInvalidScript) {
		t.Fatalf("no capability err = %v", err)
	}
	if err := (workflow.Script{ID: "x", Capability: "c", Steps: []string{""}}).Validate(); !errors.Is(err, workflow.ErrInvalidScript) {
		t.Fatalf("empty step err = %v", err)
	}
}

func TestValidateArgs(t *testing.T) {
	s := workflow.Script{ID: "s", Capability: "c", Args: []string{"topic"}}
	if err := s.ValidateArgs(map[string]string{"topic": "x"}); err != nil {
		t.Fatalf("valid args rejected: %v", err)
	}
	if err := s.ValidateArgs(map[string]string{}); !errors.Is(err, workflow.ErrInvalidArgs) {
		t.Fatalf("missing arg err = %v", err)
	}
	if err := s.ValidateArgs(map[string]string{"topic": "x", "extra": "y"}); !errors.Is(err, workflow.ErrInvalidArgs) {
		t.Fatalf("extra arg err = %v", err)
	}
}

func TestRequestRequiresParent(t *testing.T) {
	r := workflow.Request{Script: workflow.Script{ID: "s", Capability: "c"}}
	if !errors.Is(r.Validate(), workflow.ErrParentRequired) {
		t.Fatalf("missing parent err = %v", r.Validate())
	}
}

func TestProviderInfoSupports(t *testing.T) {
	info := workflow.ProviderInfo{Capabilities: []string{"a"}}
	if !info.Supports("a") || info.Supports("b") {
		t.Fatal("Supports wrong")
	}
}
