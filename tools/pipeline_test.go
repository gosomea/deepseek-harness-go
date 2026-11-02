package tools_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gosomea/deepseek-harness-go/tools"
)

// readFileSchema is the schema every test in this file exercises: one required
// string member plus an optional integer, which is enough to cover the
// validation paths that matter without inventing a grammar.
func readFileSchema() tools.InputSchema {
	return tools.InputSchema{
		Type: tools.SchemaTypeObject,
		Properties: map[string]tools.PropertySchema{
			"path":  {Type: tools.SchemaTypeString, Description: "file to read"},
			"lines": {Type: tools.SchemaTypeInteger, Description: "line limit"},
			"mode":  {Type: tools.SchemaTypeString, Description: "read mode", Enum: []string{"text", "bytes"}},
		},
		Required: []string{"path"},
	}
}

// countingExecutor records how many times the body ran, which is the fact that
// distinguishes "refused before execution" from "executed and failed".
func countingExecutor(t *testing.T, counter *int32) tools.Executor {
	t.Helper()
	return func(execution tools.Context) (tools.Result, error) {
		atomic.AddInt32(counter, 1)
		if execution.Arguments["path"] == "boom" {
			return tools.Result{}, errors.New("read failed")
		}
		return tools.Result{Content: "read " + execution.Arguments["path"].(string)}, nil
	}
}

func newPipeline(t *testing.T, counter *int32, guards ...tools.Guard) *tools.Pipeline {
	t.Helper()
	registry := tools.NewRegistry()
	err := registry.Register(tools.Definition{
		Name:        "read_file",
		Description: "Read a file.",
		Schema:      readFileSchema(),
		ReadOnly:    true,
		Owner:       "test",
		Execute:     countingExecutor(t, counter),
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return &tools.Pipeline{Registry: registry, Guards: tools.NewGuardChain(guards...)}
}

func call(id, arguments string) tools.Call {
	return tools.Call{ID: id, Name: "read_file", Arguments: arguments}
}

func TestPipelineRunsTheBodyOnlyForValidAdmittedArguments(t *testing.T) {
	var counter int32
	pipeline := newPipeline(t, &counter)
	result, err := pipeline.Execute(context.Background(), call("c1", `{"path":"a.txt","lines":3}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Outcome != tools.OutcomeSuccess || result.Content != "read a.txt" {
		t.Fatalf("unexpected result %+v", result)
	}
	if result.Attempts != 1 {
		t.Fatalf("want exactly one attempt, got %d", result.Attempts)
	}
	if got := atomic.LoadInt32(&counter); got != 1 {
		t.Fatalf("body must run once, ran %d times", got)
	}
}

// The central claim of the pipeline: a rejection or an argument error must not
// reach the tool body, and it must still produce exactly one result.
func TestPipelineNeverReachesTheBodyForRejectionsAndBadArguments(t *testing.T) {
	cases := []struct {
		name        string
		arguments   string
		guards      []tools.Guard
		wantOutcome tools.ResultOutcome
		wantErr     error
	}{
		{"denied by policy", `{"path":"a.txt"}`, []tools.Guard{tools.DenyTools("read_file")}, tools.OutcomeDenied, tools.ErrToolDenied},
		{"denied by prefix", `{"path":"a.txt"}`, []tools.Guard{tools.DenyPrefix("read_")}, tools.OutcomeDenied, tools.ErrToolDenied},
		{"malformed json", `{"path":`, nil, tools.OutcomeInputInvalid, tools.ErrToolInputInvalid},
		{"missing required member", `{}`, nil, tools.OutcomeInputInvalid, tools.ErrToolInputInvalid},
		{"unknown member", `{"path":"a.txt","extra":1}`, nil, tools.OutcomeInputInvalid, tools.ErrToolInputInvalid},
		{"wrong member type", `{"path":7}`, nil, tools.OutcomeInputInvalid, tools.ErrToolInputInvalid},
		{"enum violation", `{"path":"a.txt","mode":"nope"}`, nil, tools.OutcomeInputInvalid, tools.ErrToolInputInvalid},
		{"float where integer required", `{"path":"a.txt","lines":1.5}`, nil, tools.OutcomeInputInvalid, tools.ErrToolInputInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var counter int32
			pipeline := newPipeline(t, &counter, testCase.guards...)
			result, err := pipeline.Execute(context.Background(), call("c1", testCase.arguments))
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("want %v, got %v", testCase.wantErr, err)
			}
			if result.Outcome != testCase.wantOutcome {
				t.Fatalf("want outcome %s, got %s", testCase.wantOutcome, result.Outcome)
			}
			if result.Attempts != 0 {
				t.Fatalf("a refused call must report zero attempts, got %d", result.Attempts)
			}
			if got := atomic.LoadInt32(&counter); got != 0 {
				t.Fatalf("the body must never run, ran %d times", got)
			}
			if !result.IsError || result.Content == "" {
				t.Fatalf("a refusal must be visible to the model: %+v", result)
			}
		})
	}
}

// The guard chain is monotonic: a guard appended later cannot re-authorize a
// call an earlier guard denied.
func TestGuardChainIsMonotonicAndUnknownDecisionsDeny(t *testing.T) {
	var counter int32
	calls := make([]string, 0, 2)
	recording := tools.GuardFunc(func(definition tools.Definition, invocation tools.Call) (tools.GuardDecision, string) {
		calls = append(calls, "second")
		return tools.GuardAllow, ""
	})
	pipeline := newPipeline(t, &counter, tools.DenyTools("read_file"), recording)
	result, err := pipeline.Execute(context.Background(), call("c1", `{"path":"a.txt"}`))
	if !errors.Is(err, tools.ErrToolDenied) {
		t.Fatalf("want denial, got %v", err)
	}
	if result.Outcome != tools.OutcomeDenied {
		t.Fatalf("want denied, got %s", result.Outcome)
	}
	if len(calls) != 0 {
		t.Fatal("a later guard must not even be consulted after a denial")
	}

	unknown := tools.GuardFunc(func(definition tools.Definition, invocation tools.Call) (tools.GuardDecision, string) {
		return tools.GuardDecision("maybe"), ""
	})
	chain := tools.NewGuardChain(unknown)
	decision, reason := chain.Check(tools.Definition{Name: "read_file"}, call("c1", `{}`))
	if decision != tools.GuardDeny || reason == "" {
		t.Fatalf("an unknown decision must deny with a reason, got %s/%q", decision, reason)
	}
	if chain.Len() != 1 {
		t.Fatalf("want one guard, got %d", chain.Len())
	}
	chain.Append(nil)
	if chain.Len() != 1 {
		t.Fatal("appending nil must not add a guard")
	}
}

func TestPipelineRefusesUnknownToolAndMissingRegistry(t *testing.T) {
	var counter int32
	pipeline := newPipeline(t, &counter)
	unknown := tools.Call{ID: "c1", Name: "nope", Arguments: `{}`}
	result, err := pipeline.Execute(context.Background(), unknown)
	if !errors.Is(err, tools.ErrToolNotFound) {
		t.Fatalf("want ErrToolNotFound, got %v", err)
	}
	if result.Outcome != tools.OutcomeFailed || result.Attempts != 0 {
		t.Fatalf("unexpected result %+v", result)
	}
	bare := &tools.Pipeline{}
	if _, err := bare.Execute(context.Background(), call("c1", `{"path":"a.txt"}`)); !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a registry-less pipeline, got %v", err)
	}
}

func TestPipelineRefusesInvalidCallAndEmptyArguments(t *testing.T) {
	var counter int32
	pipeline := newPipeline(t, &counter)
	if _, err := pipeline.Execute(context.Background(), tools.Call{Name: "read_file", Arguments: `{}`}); !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a call without an id, got %v", err)
	}
	if _, err := pipeline.Execute(context.Background(), tools.Call{ID: "c1", Arguments: `{}`}); !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a call without a name, got %v", err)
	}
	if _, err := pipeline.Execute(context.Background(), call("c1", "  ")); !errors.Is(err, tools.ErrToolInputInvalid) {
		t.Fatalf("want ErrToolInputInvalid for empty arguments, got %v", err)
	}
}

// A tool body that fails must still produce a normalized result, and
// post-execute normalization must run on that path too.
func TestPipelineNormalizesFailedExecutionAndRunsPostExecute(t *testing.T) {
	var counter int32
	pipeline := newPipeline(t, &counter)
	pipeline.PostExecute = func(result tools.Result) tools.Result {
		result.Content = strings.TrimSpace(result.Content) + " [normalized]"
		return result
	}
	result, err := pipeline.Execute(context.Background(), call("c1", `{"path":"boom"}`))
	if err == nil || err.Error() != "read failed" {
		t.Fatalf("want the body error, got %v", err)
	}
	if result.Outcome != tools.OutcomeFailed || !result.IsError {
		t.Fatalf("unexpected result %+v", result)
	}
	if result.Content != "read failed [normalized]" {
		t.Fatalf("post-execute must run for a failed body, got %q", result.Content)
	}
	if result.Attempts != 1 {
		t.Fatalf("want one attempt, got %d", result.Attempts)
	}
}

// Post-execute may reshape content but must not rewrite how the call ended:
// otherwise a failed tool could be presented as a success.
func TestPostExecuteCannotRewriteTheOutcome(t *testing.T) {
	var counter int32
	pipeline := newPipeline(t, &counter)
	pipeline.PostExecute = func(result tools.Result) tools.Result {
		return tools.Result{CallID: "forged", Name: "other", Outcome: tools.OutcomeSuccess, Content: "all good"}
	}
	result, err := pipeline.Execute(context.Background(), call("c1", `{"path":"boom"}`))
	if err == nil {
		t.Fatal("a failed body must still report an error")
	}
	if result.Outcome != tools.OutcomeFailed || result.CallID != "c1" || result.Name != "read_file" || !result.IsError {
		t.Fatalf("post-execute rewrote the outcome: %+v", result)
	}
	if result.Content != "all good" {
		t.Fatalf("post-execute may replace the content, got %q", result.Content)
	}
}

func TestPipelineChecksCancellationBeforeRunningTheBody(t *testing.T) {
	var counter int32
	pipeline := newPipeline(t, &counter)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := pipeline.Execute(ctx, call("c1", `{"path":"a.txt"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if result.Outcome != tools.OutcomeCanceled || result.Attempts != 0 {
		t.Fatalf("unexpected result %+v", result)
	}
	if got := atomic.LoadInt32(&counter); got != 0 {
		t.Fatalf("a canceled call must not run the body, ran %d times", got)
	}
}

// An observer sees the ordered stages, and a panicking observer cannot break
// execution.
func TestObservationOrderAndPanicContainment(t *testing.T) {
	var counter int32
	pipeline := newPipeline(t, &counter)
	var stages []tools.Stage
	pipeline.Observer = tools.ObserverFunc(func(observation tools.Observation) {
		stages = append(stages, observation.Stage)
		if observation.Stage == tools.StageExecute {
			panic("broken tracer")
		}
	})
	if _, err := pipeline.Execute(context.Background(), call("c1", `{"path":"a.txt"}`)); err != nil {
		t.Fatalf("a panicking observer must not fail the call: %v", err)
	}
	want := []tools.Stage{tools.StageLookup, tools.StageValidate, tools.StagePreExecute, tools.StageExecute, tools.StagePostExecute, tools.StageFinalize}
	if len(stages) != len(want) {
		t.Fatalf("want stages %v, got %v", want, stages)
	}
	for index := range want {
		if stages[index] != want[index] {
			t.Fatalf("stage order wrong at %d: want %s, got %s", index, want[index], stages[index])
		}
	}
}

// A denied call must not emit StageExecute at all: the absence is the evidence,
// not a message claiming so.
func TestDeniedCallEmitsNoExecuteStage(t *testing.T) {
	var counter int32
	pipeline := newPipeline(t, &counter, tools.DenyTools("read_file"))
	var stages []tools.Stage
	pipeline.Observer = tools.ObserverFunc(func(observation tools.Observation) {
		stages = append(stages, observation.Stage)
	})
	if _, err := pipeline.Execute(context.Background(), call("c1", `{"path":"a.txt"}`)); err == nil {
		t.Fatal("a denied call must report an error")
	}
	for _, stage := range stages {
		if stage == tools.StageExecute {
			t.Fatalf("a denied call must not reach execute, stages=%v", stages)
		}
	}
}

func TestExecuteAllReturnsOneResultPerCallAndJoinsErrors(t *testing.T) {
	var counter int32
	pipeline := newPipeline(t, &counter)
	calls := []tools.Call{
		call("c1", `{"path":"a.txt"}`),
		call("c2", `{"path":"boom"}`),
		call("c3", `{"path":"c.txt"}`),
	}
	results, err := pipeline.ExecuteAll(context.Background(), calls, false)
	if err == nil {
		t.Fatal("want the joined body error")
	}
	if err := tools.ValidateResults(calls, results); err != nil {
		t.Fatalf("results must answer every call in order: %v", err)
	}
	if results[0].Outcome != tools.OutcomeSuccess || results[1].Outcome != tools.OutcomeFailed || results[2].Outcome != tools.OutcomeSuccess {
		t.Fatalf("unexpected outcomes %+v", results)
	}
	stopped, _ := pipeline.ExecuteAll(context.Background(), calls, true)
	if len(stopped) != 2 {
		t.Fatalf("stopOnError must stop after the failing call, got %d results", len(stopped))
	}
}

func TestValidateResultsRejectsMismatchedAnswers(t *testing.T) {
	calls := []tools.Call{call("c1", `{}`), call("c2", `{}`)}
	if err := tools.ValidateResults(calls, []tools.Result{{CallID: "c1", Name: "read_file", Outcome: tools.OutcomeSuccess}}); !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a missing result, got %v", err)
	}
	wrong := []tools.Result{
		{CallID: "c2", Name: "read_file", Outcome: tools.OutcomeSuccess},
		{CallID: "c1", Name: "read_file", Outcome: tools.OutcomeSuccess},
	}
	if err := tools.ValidateResults(calls, wrong); !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a mismatched call id, got %v", err)
	}
}
