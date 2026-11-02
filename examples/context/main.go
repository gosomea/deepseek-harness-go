// Command context demonstrates the rebuildable-request property without a
// session log: contributions are recorded to a small in-memory log, rendered
// twice, and shown to agree, then a refused append is shown to leave nothing
// citable.
package main

import (
	"fmt"
	"io"
	"os"

	capcompaction "github.com/gosomea/deepseek-harness-go/capability/compaction"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/plugins/compaction"
	"github.com/gosomea/deepseek-harness-go/plugins/context"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// memoryLog stands in for the session log: a place contributions are recorded
// before anything renders them. It refuses one id so the example can show that a
// refused contribution never becomes citable.
type memoryLog struct {
	recorded []string
	refuseID string
}

// AppendContribution records one contribution and returns its identity.
func (l *memoryLog) AppendContribution(c context.Contribution) (string, error) {
	if c.ID == l.refuseID {
		return "", fmt.Errorf("log refused to record %q", c.ID)
	}
	l.recorded = append(l.recorded, c.ID)
	return c.ID, nil
}

func run(out io.Writer) error {
	log := &memoryLog{}
	recorder := context.NewRecorder(log)

	set, err := recorder.Record(
		context.Contribution{
			ID: "base", Kind: context.KindInstruction, Source: context.SourceBase,
			Text: "You are a harness.",
		},
		context.Contribution{
			ID: "spec", Kind: context.KindReference, Source: context.SourceEnvironment,
			Reference: &context.Reference{Path: "docs/context/contributions.md"},
		},
		context.Contribution{
			ID: "clock", Kind: context.KindTime, Source: context.SourceProfile,
			Text: "now: fixed for this experiment",
		},
	)
	if err != nil {
		return err
	}

	// Rendering twice must agree: that is what makes a request comparable.
	first, err := context.Render(set, "")
	if err != nil {
		return err
	}
	second, err := context.Rebuild(set, "")
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "recorded: %v\n", log.recorded)
	fmt.Fprintf(out, "cited: %v\n", first.CitedIDs)
	fmt.Fprintf(out, "two renders agree: %t\n", first.Text == second.Text)
	fmt.Fprintf(out, "text:\n%s\n", first.Text)

	// A refused append must not become citable: the recorder returns no set, so
	// there is nothing for a later render to read.
	refusing := &memoryLog{refuseID: "blocked"}
	refuser := context.NewRecorder(refusing)
	refused, err := refuser.Record(
		context.Contribution{
			ID: "kept", Kind: context.KindInstruction, Source: context.SourceBase,
			Text: "recorded before the failure",
		},
		context.Contribution{
			ID: "blocked", Kind: context.KindInstruction, Source: context.SourceBase,
			Text: "the log will refuse this one",
		},
	)
	fmt.Fprintf(out, "refused append error: %v\n", err != nil)
	fmt.Fprintf(out, "set after refusal is nil: %t\n", refused == nil)
	fmt.Fprintf(out, "log recorded before refusal: %v\n", refusing.recorded)

	// Compaction through a real route: a summary replaces a region, the tool
	// call/result pair survives, and a failed summary never claims the end phase.
	summaryRoute, err := compaction.NewRoute(compaction.RouteOptions{
		Provider: testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"the user asked to read notes.txt"})),
		Model:    "fake-model",
	})
	if err != nil {
		return err
	}
	compactor := compaction.NewCompactor(summaryRoute)
	history := []capcompaction.Entry{
		{ID: "m1", Kind: capcompaction.KindMessage, Text: "please read the file"},
		{ID: "c1", Kind: capcompaction.KindToolCall, Text: "read_file(notes.txt)", CallID: "call-1"},
		{ID: "r1", Kind: capcompaction.KindToolResult, Text: "hello", CallID: "call-1"},
		{ID: "m2", Kind: capcompaction.KindMessage, Text: "thanks"},
	}
	result, err := compactor.Compact(history, capcompaction.Plan{Start: 0, End: 1, Summary: "requested"})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "compaction applied: %t\n", result.Applied)
	fmt.Fprintf(out, "compaction phases: %v\n", compactor.Phases())

	// A failing provider must never report the end phase.
	failing, err := compaction.NewRoute(compaction.RouteOptions{
		Provider: testkit.NewFakeProviderWithOptions(testkit.FakeProviderFailure(io.ErrUnexpectedEOF)),
		Model:    "fake-model",
	})
	if err != nil {
		return err
	}
	failingCompactor := compaction.NewCompactor(failing)
	_, compactErr := failingCompactor.Compact(history, capcompaction.Plan{Start: 0, End: 1, Summary: "requested"})
	fmt.Fprintf(out, "summary failure reported: %t\n", compactErr != nil)
	fmt.Fprintf(out, "failed compaction phases: %v\n", failingCompactor.Phases())
	return nil
}
