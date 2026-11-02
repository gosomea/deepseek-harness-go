package tools

import (
	"context"
	"errors"
	"testing"
)

// These tests live in the package so that the coverage gate counts the schema
// and registry branches that an external test cannot reach directly.

func TestInputSchemaValidateRejectsUnenforceableShapes(t *testing.T) {
	if err := (InputSchema{}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a type-less schema, got %v", err)
	}
	bad := InputSchema{Type: SchemaTypeObject, Required: []string{"missing"}}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an undescribed required member, got %v", err)
	}
	blank := InputSchema{Type: SchemaTypeObject, Required: []string{" "}, Properties: map[string]PropertySchema{" ": {Type: SchemaTypeString}}}
	if err := blank.Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank required name, got %v", err)
	}
	if err := (InputSchema{Type: SchemaTypeArray}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an array without items, got %v", err)
	}
	if err := (InputSchema{Type: SchemaTypeString}).Validate(); err != nil {
		t.Fatalf("a scalar schema must be valid: %v", err)
	}
}

func TestValidateTypeRejectsUnsupportedAndMismatchedTypes(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  JSONSchemaType
	}{
		{"object as string", "x", SchemaTypeObject},
		{"array as string", "x", SchemaTypeArray},
		{"string as object", map[string]any{}, SchemaTypeString},
		{"boolean as string", "x", SchemaTypeBoolean},
		{"number as string", "x", SchemaTypeNumber},
		{"string as integer", "x", SchemaTypeInteger},
		{"fractional as integer", 1.5, SchemaTypeInteger},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateType(testCase.want, testCase.value, "v"); !errors.Is(err, ErrToolInputInvalid) {
				t.Fatalf("want ErrToolInputInvalid, got %v", err)
			}
		})
	}
	if err := validateType(JSONSchemaType("money"), 1.0, "v"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unsupported schema type, got %v", err)
	}
	if err := validateType("", nil, "v"); err != nil {
		t.Fatalf("an absent type must be tolerated: %v", err)
	}
	if err := validateType(SchemaTypeInteger, 2.0, "v"); err != nil {
		t.Fatalf("an integral float must satisfy an integer schema: %v", err)
	}
}

func TestValidateArgumentsAcceptsEnumAndTypedArrays(t *testing.T) {
	schema := InputSchema{
		Type: SchemaTypeObject,
		Properties: map[string]PropertySchema{
			"mode": {Type: SchemaTypeString, Enum: []string{"text"}},
			"tags": {Type: SchemaTypeArray, Items: &PropertySchema{Type: SchemaTypeString}},
		},
	}
	if _, err := ValidateArguments(schema, `{"mode":"text","tags":["a","b"]}`); err != nil {
		t.Fatalf("valid arguments refused: %v", err)
	}
	if _, err := ValidateArguments(schema, `{"mode":"text","tags":["a",1]}`); !errors.Is(err, ErrToolInputInvalid) {
		t.Fatalf("want ErrToolInputInvalid for a mistyped element, got %v", err)
	}
	if _, err := ValidateArguments(schema, `{"mode":7}`); !errors.Is(err, ErrToolInputInvalid) {
		t.Fatalf("want ErrToolInputInvalid for a non-string enum value, got %v", err)
	}
	if _, err := ValidateArguments(schema, `null`); !errors.Is(err, ErrToolInputInvalid) {
		t.Fatalf("want ErrToolInputInvalid for null arguments, got %v", err)
	}
}

func TestValidateArgumentsAcceptsNonObjectSchemaAndExtraMembers(t *testing.T) {
	object := InputSchema{
		Type:                 SchemaTypeObject,
		AdditionalProperties: true,
		Properties:           map[string]PropertySchema{"known": {Type: SchemaTypeString}},
	}
	decoded, err := ValidateArguments(object, `{"known":"a","extra":1}`)
	if err != nil {
		t.Fatalf("additional properties are allowed: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("want both members decoded, got %v", decoded)
	}
	scalar := InputSchema{Type: SchemaTypeString}
	decoded, err = ValidateArguments(scalar, `"x"`)
	if err != nil {
		t.Fatalf("a scalar schema must validate: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("a scalar schema has no member map, got %v", decoded)
	}
	if _, err := ValidateArguments(InputSchema{}, `{}`); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an invalid schema, got %v", err)
	}
}

func TestSchemaTextRendersEnforceableMembers(t *testing.T) {
	rendered, err := SchemaText(InputSchema{
		Type:       SchemaTypeObject,
		Properties: map[string]PropertySchema{"path": {Type: SchemaTypeString, Description: "file"}},
		Required:   []string{"path"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rendered != `{"type":"object","properties":{"path":{"type":"string","description":"file"}},"required":["path"]}` {
		t.Fatalf("unexpected rendering %q", rendered)
	}
}

func TestJSONKindNamesEveryDecodedShape(t *testing.T) {
	cases := map[string]any{
		"null":    nil,
		"boolean": true,
		"number":  1.0,
		"string":  "x",
		"array":   []any{},
		"object":  map[string]any{},
	}
	for want, value := range cases {
		if got := jsonKind(value); got != want {
			t.Fatalf("want %q, got %q", want, got)
		}
	}
	if got := jsonKind(struct{}{}); got == "" {
		t.Fatal("an unexpected Go type must still be named")
	}
}

func TestRegistryRefusesDuplicateAndExecutorlessDefinitions(t *testing.T) {
	registry := NewRegistry()
	definition := Definition{
		Name:        "read_file",
		Description: "Read a file.",
		Schema:      InputSchema{Type: SchemaTypeObject},
		Execute:     func(Context) (Result, error) { return Result{}, nil },
	}
	if err := registry.Register(definition); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.Register(definition); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a duplicate, got %v", err)
	}
	if err := registry.Register(Definition{Name: "x", Description: "d", Schema: InputSchema{Type: SchemaTypeObject}}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a missing executor, got %v", err)
	}
	// A blank name is refused; note that the previous case already used "x" and
	// was refused for the missing executor, so "x" itself is still free.
	if err := registry.Register(Definition{Description: "d", Schema: InputSchema{Type: SchemaTypeObject}, Execute: definition.Execute}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank name, got %v", err)
	}
	if err := registry.RegisterDefinition(Definition{Name: "schema-only", Description: "d", Schema: InputSchema{Type: SchemaTypeObject}}); err != nil {
		t.Fatalf("a definition-only entry must register: %v", err)
	}
	if err := registry.RegisterDefinition(Definition{Name: "schema-only", Description: "d", Schema: InputSchema{Type: SchemaTypeObject}}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a duplicate definition-only entry, got %v", err)
	}
	if !registry.Unregister("schema-only") || registry.Unregister("schema-only") {
		t.Fatal("Unregister must report presence then absence")
	}
	if registry.Len() != 1 {
		t.Fatalf("want one tool left, got %d", registry.Len())
	}
	if names := registry.Names(); len(names) != 1 || names[0] != "read_file" {
		t.Fatalf("unexpected names %v", names)
	}
	clone := registry.Clone()
	if _, ok := clone.Lookup("read_file"); !ok {
		t.Fatal("Clone must carry the tools")
	}
	clone.Unregister("read_file")
	if _, ok := registry.Lookup("read_file"); !ok {
		t.Fatal("mutating a clone must not affect the original")
	}
}

func TestPipelineRefusesExecutorlessDefinitionAndValidation(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterDefinition(Definition{
		Name:        "schema-only",
		Description: "Only a schema.",
		Schema:      InputSchema{Type: SchemaTypeObject},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	pipeline := &Pipeline{Registry: registry}
	result, err := pipeline.Execute(context.Background(), Call{ID: "c1", Name: "schema-only", Arguments: `{}`})
	if !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a definition without a body, got %v", err)
	}
	if result.Outcome != OutcomeFailed || result.Attempts != 0 {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestResultAndDefinitionValidation(t *testing.T) {
	if err := (Result{Name: "t", Outcome: OutcomeSuccess}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a result without a call id, got %v", err)
	}
	if err := (Result{CallID: "c", Outcome: OutcomeSuccess}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a result without a name, got %v", err)
	}
	if err := (Result{CallID: "c", Name: "t"}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a result without an outcome, got %v", err)
	}
	if err := (Result{CallID: "c", Name: "t", Outcome: OutcomeSuccess}).Validate(); err != nil {
		t.Fatalf("a complete result must validate: %v", err)
	}
	if err := (Definition{Description: "d", Schema: InputSchema{Type: SchemaTypeObject}}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a nameless tool, got %v", err)
	}
	if err := (Definition{Name: "t", Schema: InputSchema{Type: SchemaTypeObject}}).Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a tool without a description, got %v", err)
	}
}

func TestValidateNamesRejectsBlanksAndDuplicates(t *testing.T) {
	if err := ValidateNames([]ToolName{"a", "b"}); err != nil {
		t.Fatalf("valid names refused: %v", err)
	}
	if err := ValidateNames([]ToolName{"a", "a"}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a duplicate, got %v", err)
	}
	if err := ValidateNames([]ToolName{""}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank name, got %v", err)
	}
}

func TestToolNameHelpersAndErrorText(t *testing.T) {
	name := ToolName("read_file")
	if name.String() != "read_file" || name.IsZero() {
		t.Fatalf("unexpected name helpers for %q", name)
	}
	if !ToolName("").IsZero() {
		t.Fatal("the empty name must report zero")
	}
	denied := DenyTools("read_file")
	decision, reason := denied.Check(Definition{Name: "other"}, Call{})
	if decision != GuardAbstain || reason != "" {
		t.Fatalf("a non-matching name must abstain, got %s/%q", decision, reason)
	}
	_, reason = denied.Check(Definition{Name: "read_file"}, Call{})
	if reason == "" {
		t.Fatal("a denial must carry a reason")
	}
}
