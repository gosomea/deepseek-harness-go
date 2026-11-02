package systemprompt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/tools"
)

// ToolSchema is one tool as the model sees it.
//
// It is built from a validated tools.Definition rather than accepted as free
// text, so the schema a model plans against is the same schema the pipeline
// enforces. A prompt that described a different schema than the executor
// checks would make a model's correct plan fail for reasons it cannot see.
type ToolSchema struct {
	// Name is the tool name the model calls.
	Name string `json:"name"`
	// Description explains the tool.
	Description string `json:"description"`
	// Schema is the argument schema, rendered as JSON.
	Schema string `json:"schema"`
	// ReadOnly declares that the tool has no side effects.
	ReadOnly bool `json:"readOnly,omitempty"`
}

// Assembly is the rendered result: one system prompt and one tool list.
//
// It is a value, not a builder, because the point of assembly is that it can be
// produced twice from the same inputs and compared. Two Assemblies with the
// same fields must serialize identically.
type Assembly struct {
	// Text is the rendered system prompt.
	Text string `json:"text"`
	// Tools is the tool list in deterministic order.
	Tools []ToolSchema `json:"tools,omitempty"`
	// Fragments records which fragments were rendered, in order, so a reader
	// can attribute a line of the prompt to its contributor.
	Fragments []Fragment `json:"-"`
}

// AssemblyInput is everything assembly needs.
//
// Tools arrive as definitions rather than as a pre-rendered list so that the
// assembly step is the single place where a tool becomes model-visible text;
// a caller cannot inject a tool description the pipeline would not enforce.
type AssemblyInput struct {
	// Fragments is the fragment set to render.
	Fragments *Set
	// Scope selects the Agent scope whose fragments are visible. An empty scope
	// renders only global fragments.
	Scope string
	// Tools are the definitions visible in this scope, in unspecified order;
	// assembly sorts them.
	Tools []tools.Definition
	// Separator joins fragments. An empty separator means two newlines.
	Separator string
}

// Assemble renders the prompt text and tool list.
//
// It is a pure function of its input: iterating the same input twice produces
// byte-identical output, which is what makes the request comparable between two
// runs and what a prompt-regression test asserts.
func Assemble(input AssemblyInput) (Assembly, error) {
	separator := input.Separator
	if separator == "" {
		separator = "\n\n"
	}
	assembly := Assembly{}
	if input.Fragments != nil {
		fragments := input.Fragments.Ordered(input.Scope)
		texts := make([]string, 0, len(fragments))
		for _, fragment := range fragments {
			texts = append(texts, strings.TrimSpace(fragment.Text))
		}
		assembly.Text = strings.Join(texts, separator)
		assembly.Fragments = fragments
	}
	toolSchemas := make([]ToolSchema, 0, len(input.Tools))
	for _, definition := range input.Tools {
		if err := definition.Validate(); err != nil {
			return Assembly{}, fmt.Errorf("tool %q: %w", definition.Name, err)
		}
		schema, err := tools.SchemaText(definition.Schema)
		if err != nil {
			return Assembly{}, fmt.Errorf("tool %q: %w", definition.Name, err)
		}
		toolSchemas = append(toolSchemas, ToolSchema{
			Name:        definition.Name.String(),
			Description: definition.Description,
			Schema:      schema,
			ReadOnly:    definition.ReadOnly,
		})
	}
	sort.Slice(toolSchemas, func(i, j int) bool { return toolSchemas[i].Name < toolSchemas[j].Name })
	assembly.Tools = toolSchemas
	return assembly, nil
}

// SystemMessage renders the assembled prompt as a system message.
//
// An assembly with no text produces no message at all rather than an empty one:
// an empty system message records "no instructions", which is a different fact
// from "instructions that happen to be empty".
func (a Assembly) SystemMessage() (llm.Message, error) {
	if strings.TrimSpace(a.Text) == "" {
		return llm.Message{}, fmt.Errorf("%w: assembly has no prompt text", ErrInvalidValue)
	}
	return llm.NewSystemMessage(a.Text)
}

// ToolNames returns the assembled tool names in order.
func (a Assembly) ToolNames() []string {
	names := make([]string, 0, len(a.Tools))
	for _, tool := range a.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// Equal reports whether two assemblies are identical.
//
// It exists so that "the same inputs produce the same request" is a single
// callable assertion instead of a diff a test has to write by hand.
func (a Assembly) Equal(other Assembly) bool {
	if a.Text != other.Text || len(a.Tools) != len(other.Tools) {
		return false
	}
	for index := range a.Tools {
		if a.Tools[index] != other.Tools[index] {
			return false
		}
	}
	return true
}
