package systemprompt_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/systemprompt"
	"github.com/gosomea/deepseek-harness-go/tools"
)

func fragment(id string, order int, text string, scopes ...string) systemprompt.Fragment {
	return systemprompt.Fragment{ID: id, Source: systemprompt.SourceBase, Order: order, Text: text, Scopes: scopes}
}

func definition(name, description string) tools.Definition {
	return tools.Definition{
		Name:        tools.ToolName(name),
		Description: description,
		Schema: tools.InputSchema{
			Type:       tools.SchemaTypeObject,
			Properties: map[string]tools.PropertySchema{"path": {Type: tools.SchemaTypeString}},
			Required:   []string{"path"},
		},
		ReadOnly: true,
	}
}

func TestAssemblyRendersFragmentsInExplicitOrder(t *testing.T) {
	set := systemprompt.NewSet()
	// Registered out of order on purpose: rendering must follow Order, not
	// registration sequence.
	for _, f := range []systemprompt.Fragment{
		fragment("c", 30, "third"),
		fragment("a", 10, "first"),
		fragment("b", 20, "second"),
	} {
		if err := set.Add(f); err != nil {
			t.Fatalf("add %s: %v", f.ID, err)
		}
	}
	assembly, err := systemprompt.Assemble(systemprompt.AssemblyInput{Fragments: set})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if assembly.Text != "first\n\nsecond\n\nthird" {
		t.Fatalf("unexpected text %q", assembly.Text)
	}
	ids := []string{}
	for _, f := range assembly.Fragments {
		ids = append(ids, f.ID)
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("unexpected fragment order %v", ids)
	}
}

// Equal orders must be broken deterministically by ID, so the same input never
// renders differently between runs.
func TestAssemblyIsDeterministicForEqualOrders(t *testing.T) {
	build := func() *systemprompt.Set {
		set := systemprompt.NewSet()
		for _, id := range []string{"zulu", "alpha", "mike"} {
			if err := set.Add(fragment(id, 5, id)); err != nil {
				t.Fatalf("add %s: %v", id, err)
			}
		}
		return set
	}
	first, err := systemprompt.Assemble(systemprompt.AssemblyInput{Fragments: build()})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if first.Text != "alpha\n\nmike\n\nzulu" {
		t.Fatalf("equal orders must sort by id, got %q", first.Text)
	}
	second, err := systemprompt.Assemble(systemprompt.AssemblyInput{Fragments: build()})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if !first.Equal(second) {
		t.Fatal("two assemblies of the same input must be equal")
	}
}

// Scoped fragments are filtered during ordering, so a caller cannot render a
// fragment that does not belong to the scope.
func TestScopeFilteringSelectsOnlyVisibleFragments(t *testing.T) {
	set := systemprompt.NewSet()
	for _, f := range []systemprompt.Fragment{
		fragment("global", 10, "everyone"),
		fragment("agent-a", 20, "only a", "agent-a"),
		fragment("agent-b", 30, "only b", "agent-b"),
	} {
		if err := set.Add(f); err != nil {
			t.Fatalf("add %s: %v", f.ID, err)
		}
	}
	assembly, err := systemprompt.Assemble(systemprompt.AssemblyInput{Fragments: set, Scope: "agent-a"})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if assembly.Text != "everyone\n\nonly a" {
		t.Fatalf("unexpected scoped text %q", assembly.Text)
	}
	empty, err := systemprompt.Assemble(systemprompt.AssemblyInput{Fragments: set, Scope: "agent-c"})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if empty.Text != "everyone" {
		t.Fatalf("an unrelated scope must see only global fragments, got %q", empty.Text)
	}
}

func TestSetRefusesDuplicateAndInvalidFragments(t *testing.T) {
	set := systemprompt.NewSet()
	if err := set.Add(fragment("dup", 1, "text")); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := set.Add(fragment("dup", 2, "other")); !errors.Is(err, systemprompt.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a duplicate id, got %v", err)
	}
	if err := set.Add(systemprompt.Fragment{ID: "", Source: systemprompt.SourceBase, Text: "x"}); !errors.Is(err, systemprompt.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank id, got %v", err)
	}
	if err := set.Add(systemprompt.Fragment{ID: "x", Text: "x"}); !errors.Is(err, systemprompt.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a missing source, got %v", err)
	}
	if err := set.Add(systemprompt.Fragment{ID: "y", Source: systemprompt.SourceBase, Text: "  "}); !errors.Is(err, systemprompt.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for blank text, got %v", err)
	}
	if err := set.Add(systemprompt.Fragment{ID: "z", Source: systemprompt.SourceBase, Text: "x", Scopes: []string{" "}}); !errors.Is(err, systemprompt.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank scope, got %v", err)
	}
	if set.Len() != 1 {
		t.Fatalf("refused fragments must not be stored, got %d", set.Len())
	}
	if !set.Remove("dup") || set.Remove("dup") {
		t.Fatal("Remove must report presence then absence")
	}
	if set.Len() != 0 {
		t.Fatalf("want an empty set, got %d", set.Len())
	}
}

// The tool schema a model sees must be the schema the pipeline enforces, so the
// two cannot drift apart.
func TestAssemblyToolSchemasMatchTheEnforcedSchema(t *testing.T) {
	assembly, err := systemprompt.Assemble(systemprompt.AssemblyInput{
		Tools: []tools.Definition{definition("read_file", "Read a file."), definition("write_file", "Write a file.")},
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if names := assembly.ToolNames(); strings.Join(names, ",") != "read_file,write_file" {
		t.Fatalf("tools must be sorted by name, got %v", names)
	}
	for _, schema := range assembly.Tools {
		if !strings.Contains(schema.Schema, `"required":["path"]`) {
			t.Fatalf("tool %s schema lost the enforced constraint: %q", schema.Name, schema.Schema)
		}
		if !schema.ReadOnly {
			t.Fatalf("tool %s must carry its read-only declaration", schema.Name)
		}
	}
}

func TestAssemblyRefusesInvalidToolDefinition(t *testing.T) {
	_, err := systemprompt.Assemble(systemprompt.AssemblyInput{
		Tools: []tools.Definition{{Name: "broken", Description: "d", Schema: tools.InputSchema{Type: tools.SchemaTypeObject, Required: []string{"gap"}}}},
	})
	if !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want the tool validation error, got %v", err)
	}
}

func TestSystemMessageRefusesEmptyText(t *testing.T) {
	empty := systemprompt.Assembly{}
	if _, err := empty.SystemMessage(); !errors.Is(err, systemprompt.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an empty assembly, got %v", err)
	}
	set := systemprompt.NewSet()
	if err := set.Add(fragment("base", 1, "You are a harness.")); err != nil {
		t.Fatalf("add: %v", err)
	}
	assembly, err := systemprompt.Assemble(systemprompt.AssemblyInput{Fragments: set})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	message, err := assembly.SystemMessage()
	if err != nil {
		t.Fatalf("system message: %v", err)
	}
	if message.Role != "system" || message.Source.Kind != "system-prompt" {
		t.Fatalf("unexpected message %+v", message)
	}
}

func TestSeparatorAndBareInputs(t *testing.T) {
	set := systemprompt.NewSet()
	for _, f := range []systemprompt.Fragment{fragment("a", 1, "one"), fragment("b", 2, "two")} {
		if err := set.Add(f); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	joined, err := systemprompt.Assemble(systemprompt.AssemblyInput{Fragments: set, Separator: " | "})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if joined.Text != "one | two" {
		t.Fatalf("unexpected joined text %q", joined.Text)
	}
	bare, err := systemprompt.Assemble(systemprompt.AssemblyInput{})
	if err != nil {
		t.Fatalf("assemble with no input: %v", err)
	}
	if bare.Text != "" || len(bare.Tools) != 0 {
		t.Fatalf("an empty input must assemble to nothing, got %+v", bare)
	}
	if bare.Equal(joined) {
		t.Fatal("assemblies with different text must not be equal")
	}
	if len(set.Fragments()) != 2 {
		t.Fatalf("Fragments must return a copy of both, got %d", len(set.Fragments()))
	}
}
