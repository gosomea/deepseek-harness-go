package systemprompt

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// These tests live in the package so the coverage gate counts the fragment and
// assembly branches an external test cannot reach directly.

func TestFragmentAppliesToHandlesGlobalAndScoped(t *testing.T) {
	global := Fragment{ID: "g", Source: SourceBase, Text: "x"}
	if !global.AppliesTo("anything") {
		t.Fatal("a fragment with no scopes must be global")
	}
	scoped := Fragment{ID: "s", Source: SourceBase, Text: "x", Scopes: []string{"a", "b"}}
	if !scoped.AppliesTo("b") {
		t.Fatal("a listed scope must match")
	}
	if scoped.AppliesTo("c") {
		t.Fatal("an unlisted scope must not match")
	}
}

func TestFragmentValidateCoversEveryRejection(t *testing.T) {
	cases := []Fragment{
		{Source: SourceBase, Text: "x"},
		{ID: "a", Text: "x"},
		{ID: "a", Source: SourceBase},
		{ID: "a", Source: SourceBase, Text: "x", Scopes: []string{" "}},
	}
	for index, fragment := range cases {
		if err := fragment.Validate(); !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("case %d: want ErrInvalidValue, got %v", index, err)
		}
	}
}

func TestSetRemoveKeepsRemainingFragmentsInOrder(t *testing.T) {
	set := NewSet()
	for _, id := range []string{"a", "b", "c"} {
		if err := set.Add(Fragment{ID: id, Source: SourceBase, Order: 1, Text: id}); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	if !set.Remove("b") {
		t.Fatal("Remove must report a present fragment")
	}
	ordered := set.Ordered("")
	if len(ordered) != 2 || ordered[0].ID != "a" || ordered[1].ID != "c" {
		t.Fatalf("unexpected remaining order %+v", ordered)
	}
	if set.Remove("b") {
		t.Fatal("Remove must report an absent fragment")
	}
}

func TestAssembleRejectsAnInvalidToolDefinition(t *testing.T) {
	_, err := Assemble(AssemblyInput{Tools: []tools.Definition{{
		Name: "broken", Description: "d",
		Schema: tools.InputSchema{Type: tools.SchemaTypeObject, Required: []string{"gap"}},
	}}})
	if !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want the definition error, got %v", err)
	}
}

func TestAssemblySystemMessageAndToolNames(t *testing.T) {
	set := NewSet()
	if err := set.Add(Fragment{ID: "base", Source: SourceBase, Text: "instructions"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	assembly, err := Assemble(AssemblyInput{Fragments: set})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	message, err := assembly.SystemMessage()
	if err != nil {
		t.Fatalf("system message: %v", err)
	}
	if message.Role != llm.RoleSystem || message.Source.Kind != llm.SourceSystemPrompt {
		t.Fatalf("unexpected message %+v", message)
	}
	if len(assembly.ToolNames()) != 0 {
		t.Fatalf("an assembly without tools must name none, got %v", assembly.ToolNames())
	}
}

func TestAssemblyEqualComparesTextAndTools(t *testing.T) {
	first := Assembly{Text: "a", Tools: []ToolSchema{{Name: "t"}}}
	if !first.Equal(Assembly{Text: "a", Tools: []ToolSchema{{Name: "t"}}}) {
		t.Fatal("identical assemblies must be equal")
	}
	if first.Equal(Assembly{Text: "b", Tools: []ToolSchema{{Name: "t"}}}) {
		t.Fatal("differing text must not be equal")
	}
	if first.Equal(Assembly{Text: "a", Tools: []ToolSchema{{Name: "t"}, {Name: "u"}}}) {
		t.Fatal("differing tool counts must not be equal")
	}
	if first.Equal(Assembly{Text: "a", Tools: []ToolSchema{{Name: "u"}}}) {
		t.Fatal("differing tool names must not be equal")
	}
}
