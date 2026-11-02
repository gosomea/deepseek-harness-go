package tools_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/tools"
)

func objectSchema() tools.InputSchema {
	return tools.InputSchema{
		Type: tools.SchemaTypeObject,
		Properties: map[string]tools.PropertySchema{
			"path": {Type: tools.SchemaTypeString},
			"mode": {Type: tools.SchemaTypeString, Enum: []string{"text", "bytes"}},
			"tags": {Type: tools.SchemaTypeArray, Items: &tools.PropertySchema{Type: tools.SchemaTypeString}},
			"flag": {Type: tools.SchemaTypeBoolean},
			"size": {Type: tools.SchemaTypeInteger},
		},
		Required: []string{"path"},
	}
}

// The validator is the boundary that keeps a malformed argument value away from
// a tool body, so every type must be checked, not sampled.
func TestValidateArgumentsAcceptsEveryDeclaredType(t *testing.T) {
	decoded, err := tools.ValidateArguments(objectSchema(), `{"path":"a.txt","mode":"text","tags":["x"],"flag":true,"size":3}`)
	if err != nil {
		t.Fatalf("valid arguments refused: %v", err)
	}
	if decoded["path"] != "a.txt" || decoded["size"] != float64(3) {
		t.Fatalf("unexpected decoded value %+v", decoded)
	}
}

func TestValidateArgumentsRejectsEachViolation(t *testing.T) {
	cases := []struct {
		name      string
		arguments string
	}{
		{"malformed json", `{"path":`},
		{"not an object", `"text"`},
		{"null", `null`},
		{"missing required", `{}`},
		{"unknown member", `{"path":"a","nope":1}`},
		{"wrong type", `{"path":7}`},
		{"boolean as string", `{"path":"a","flag":"yes"}`},
		{"integer as fraction", `{"path":"a","size":1.5}`},
		{"enum violation", `{"path":"a","mode":"nope"}`},
		{"element type", `{"path":"a","tags":[1]}`},
		{"array as scalar", `{"path":"a","tags":"x"}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := tools.ValidateArguments(objectSchema(), testCase.arguments); !errors.Is(err, tools.ErrToolInputInvalid) {
				t.Fatalf("want ErrToolInputInvalid, got %v", err)
			}
		})
	}
}

// An object schema that allows extra members must accept them, and a scalar
// schema must validate without pretending to have a member map.
func TestValidateArgumentsHonoursAdditionalPropertiesAndScalars(t *testing.T) {
	open := objectSchema()
	open.AdditionalProperties = true
	if _, err := tools.ValidateArguments(open, `{"path":"a","extra":{"nested":1}}`); err != nil {
		t.Fatalf("additional properties must be accepted: %v", err)
	}
	if _, err := tools.ValidateArguments(tools.InputSchema{Type: tools.SchemaTypeString}, `"x"`); err != nil {
		t.Fatalf("a scalar schema must validate: %v", err)
	}
	if _, err := tools.ValidateArguments(tools.InputSchema{Type: tools.SchemaTypeString}, `1`); !errors.Is(err, tools.ErrToolInputInvalid) {
		t.Fatalf("want ErrToolInputInvalid for a wrong scalar type, got %v", err)
	}
}

// A schema whose type this build cannot enforce must fail loudly: silently
// accepting an unchecked value is the failure mode this package exists to
// prevent.
func TestValidateArgumentsRefusesUnsupportedSchemaTypes(t *testing.T) {
	schema := tools.InputSchema{Type: tools.JSONSchemaType("money")}
	if _, err := tools.ValidateArguments(schema, `1`); !errors.Is(err, tools.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for an unsupported type, got %v", err)
	}
}

func TestSchemaTextRendersTheEnforcedConstraints(t *testing.T) {
	rendered, err := tools.SchemaText(objectSchema())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rendered == "" {
		t.Fatal("the rendering must not be empty")
	}
	for _, want := range []string{`"type":"object"`, `"required":["path"]`} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendering missing %q: %s", want, rendered)
		}
	}
}
