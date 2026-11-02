// Command tools demonstrates the tool pipeline without side effects: one call is
// admitted and runs, and three others are refused at different stages, showing
// that a refusal never reaches the tool body.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gosomea/deepseek-harness-go/systemprompt"
	"github.com/gosomea/deepseek-harness-go/tools"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run executes four calls and prints each stage observation plus the normalized
// result, so the difference between "refused" and "ran then failed" is visible
// in the output rather than asserted in prose.
func run(out io.Writer) error {
	var bodyRuns int
	registry := tools.NewRegistry()
	if err := registry.Register(tools.Definition{
		Name:        "read_file",
		Description: "Read a file from the workspace.",
		Schema: tools.InputSchema{
			Type: tools.SchemaTypeObject,
			Properties: map[string]tools.PropertySchema{
				"path": {Type: tools.SchemaTypeString, Description: "file to read"},
			},
			Required: []string{"path"},
		},
		ReadOnly: true,
		Owner:    "example",
		Execute: func(execution tools.Context) (tools.Result, error) {
			bodyRuns++
			return tools.Result{Content: "contents of " + execution.Arguments["path"].(string)}, nil
		},
	}); err != nil {
		return err
	}

	pipeline := &tools.Pipeline{
		Registry: registry,
		Guards:   tools.NewGuardChain(tools.DenyPrefix("mcp.")),
		Observer: tools.ObserverFunc(func(observation tools.Observation) {
			fmt.Fprintf(out, "observe call=%s stage=%s outcome=%s detail=%q\n",
				observation.Call.ID, observation.Stage, observation.Outcome, observation.Detail)
		}),
	}

	calls := []tools.Call{
		{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.txt"}`},
		{ID: "call-2", Name: "read_file", Arguments: `{}`},
		{ID: "call-3", Name: "missing_tool", Arguments: `{}`},
		{ID: "call-4", Name: "read_file", Arguments: `{"path":7}`},
	}

	fmt.Fprintln(out, "--- pipeline ---")
	for _, call := range calls {
		fmt.Fprintf(out, "execute call=%s name=%s arguments=%s\n", call.ID, call.Name, call.Arguments)
		result, err := pipeline.Execute(context.Background(), call)
		fmt.Fprintf(out, "  outcome=%s attempts=%d isError=%v content=%q error=%v\n",
			result.Outcome, result.Attempts, result.IsError, result.Content, err != nil)
	}
	fmt.Fprintf(out, "bodyRuns=%d\n", bodyRuns)

	fmt.Fprintln(out, "--- guard monotonicity ---")
	// Appending a permissive guard cannot re-authorize a call an earlier guard
	// denied, because the chain stops at the first refusal.
	chain := tools.NewGuardChain(tools.DenyTools("read_file"))
	chain.Append(tools.GuardFunc(func(definition tools.Definition, call tools.Call) (tools.GuardDecision, string) {
		return tools.GuardAllow, ""
	}))
	fmt.Fprintf(out, "guards=%d\n", chain.Len())
	strict := &tools.Pipeline{Registry: registry, Guards: chain}
	denied, err := strict.Execute(context.Background(), calls[0])
	fmt.Fprintf(out, "denied outcome=%s attempts=%d error=%v\n", denied.Outcome, denied.Attempts, err != nil)
	fmt.Fprintf(out, "denial names the tool=%v\n", strings.Contains(denied.Content, "read_file"))

	// The schema a model plans against must be the schema the pipeline enforces,
	// so the assembly is built from the registered definitions rather than from
	// hand-written text.
	fmt.Fprintln(out, "--- prompt assembly ---")
	fragments := systemprompt.NewSet()
	if err := fragments.Add(systemprompt.Fragment{
		ID: "base", Source: systemprompt.SourceBase, Order: 10,
		Text: "You read files on request.",
	}); err != nil {
		return err
	}
	assembly, err := systemprompt.Assemble(systemprompt.AssemblyInput{
		Fragments: fragments,
		Tools:     registry.List(),
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "prompt=%q\n", assembly.Text)
	for _, schema := range assembly.Tools {
		fmt.Fprintf(out, "tool=%s schema=%s\n", schema.Name, schema.Schema)
	}
	fmt.Fprintf(out, "schema carries the enforced constraint=%v\n", strings.Contains(assembly.Tools[0].Schema, `"required":["path"]`))
	return nil
}
