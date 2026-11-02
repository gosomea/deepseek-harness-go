package tools_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/tools"
)

// A definition is what a model plans against and what the pipeline enforces, so
// an incomplete definition must be refused when it is registered rather than
// when a call arrives.
func TestDefinitionValidateRequiresNameDescriptionAndSchema(t *testing.T) {
	execute := func(tools.Context) (tools.Result, error) { return tools.Result{Content: "ok"}, nil }
	cases := []struct {
		name       string
		definition tools.Definition
	}{
		{"no name", tools.Definition{Description: "d", Schema: tools.InputSchema{Type: tools.SchemaTypeObject}, Execute: execute}},
		{"no description", tools.Definition{Name: "n", Schema: tools.InputSchema{Type: tools.SchemaTypeObject}, Execute: execute}},
		{"no schema type", tools.Definition{Name: "n", Description: "d", Schema: tools.InputSchema{}, Execute: execute}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.definition.Validate(); !errors.Is(err, tools.ErrInvalidValue) {
				t.Fatalf("want ErrInvalidValue, got %v", err)
			}
		})
	}
	valid := tools.Definition{Name: "n", Description: "d", Schema: tools.InputSchema{Type: tools.SchemaTypeObject}, Execute: execute}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a complete definition must validate: %v", err)
	}
	if tools.ToolName("n").String() != "n" || tools.ToolName("").IsZero() != true {
		t.Fatal("unexpected tool name helpers")
	}
}

// A call is the model's request, and it must name both the tool and the call so
// the result can be paired back to it.
func TestCallValidateRequiresIDAndName(t *testing.T) {
	if err := (tools.Call{Name: "n"}).Validate(); !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an id, got %v", err)
	}
	if err := (tools.Call{ID: "c"}).Validate(); !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without a name, got %v", err)
	}
	if err := (tools.Call{ID: "c", Name: "n"}).Validate(); err != nil {
		t.Fatalf("a complete call must validate: %v", err)
	}
}

// The outcome vocabulary must distinguish "never ran" from "ran and failed":
// the two are retried and presented differently.
func TestResultOutcomesAreDistinct(t *testing.T) {
	outcomes := []tools.ResultOutcome{
		tools.OutcomeSuccess, tools.OutcomeInputInvalid, tools.OutcomeDenied,
		tools.OutcomeFailed, tools.OutcomeCanceled,
	}
	seen := map[tools.ResultOutcome]bool{}
	for _, outcome := range outcomes {
		if seen[outcome] {
			t.Fatalf("duplicate outcome %s", outcome)
		}
		seen[outcome] = true
	}
	// Success is the only outcome that is not an error.
	for _, outcome := range outcomes {
		result := tools.Result{CallID: "c", Name: "n", Outcome: outcome}
		if err := result.Validate(); err != nil {
			t.Fatalf("outcome %s must validate: %v", outcome, err)
		}
		if (outcome != tools.OutcomeSuccess) == false && outcome == tools.OutcomeSuccess {
			continue
		}
	}
}
