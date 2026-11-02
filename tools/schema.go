package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ValidateArguments checks a raw argument string against a schema and returns
// the decoded object.
//
// It refuses malformed JSON, a value of the wrong type, a missing required
// member and a member the schema does not describe. Refusing here is the point
// of the pipeline: the body of a tool must never see an argument value that
// does not satisfy the schema it advertises, because by then a real side effect
// is one call away.
func ValidateArguments(schema InputSchema, raw string) (map[string]any, error) {
	if err := schema.Validate(); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: arguments must be a JSON value", ErrToolInputInvalid)
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrToolInputInvalid, err)
	}
	if err := validateValue(schema, decoded, "arguments"); err != nil {
		return nil, err
	}
	if schema.Type == SchemaTypeObject {
		object, _ := decoded.(map[string]any)
		if object == nil {
			// json.Unmarshal yields nil for a JSON null, which satisfies no
			// object schema.
			return nil, fmt.Errorf("%w: arguments must be a JSON object, got null", ErrToolInputInvalid)
		}
		return object, nil
	}
	// A non-object schema is accepted by the vocabulary but has no member map
	// to hand a body, so it is returned as an empty argument set rather than a
	// nil map a body might dereference.
	return map[string]any{}, nil
}

// validateValue applies one schema node to one decoded JSON value.
func validateValue(schema InputSchema, value any, path string) error {
	if err := validateType(schema.Type, value, path); err != nil {
		return err
	}
	if schema.Type != SchemaTypeObject {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, name := range schema.Required {
		if _, present := object[name]; !present {
			return fmt.Errorf("%w: %s is missing required member %q", ErrToolInputInvalid, path, name)
		}
	}
	for name, member := range object {
		property, described := schema.Properties[name]
		if !described {
			if schema.AdditionalProperties {
				continue
			}
			return fmt.Errorf("%w: %s has unknown member %q", ErrToolInputInvalid, path, name)
		}
		if err := validateMember(property, member, path+"."+name); err != nil {
			return err
		}
	}
	return nil
}

// validateMember applies one property schema to one member value.
func validateMember(property PropertySchema, value any, path string) error {
	if len(property.Enum) > 0 {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: %s must be a string to match an enum", ErrToolInputInvalid, path)
		}
		found := false
		for _, allowed := range property.Enum {
			if text == allowed {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: %s must be one of %s, got %q", ErrToolInputInvalid, path, strings.Join(property.Enum, ", "), text)
		}
	}
	if err := validateType(property.Type, value, path); err != nil {
		return err
	}
	if property.Type == SchemaTypeArray && property.Items != nil {
		items, ok := value.([]any)
		if !ok {
			return nil
		}
		for index, item := range items {
			if err := validateMember(*property.Items, item, path+"["+strconv.Itoa(index)+"]"); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateType reports whether a decoded JSON value has the declared type.
//
// JSON numbers decode to float64, so an integer schema is checked for an exact
// integral value rather than for a distinct Go type: rejecting 1 as an integer
// because it arrived as float64 would refuse a value every JSON writer produces.
func validateType(want JSONSchemaType, value any, path string) error {
	switch want {
	case SchemaTypeObject:
		if _, ok := value.(map[string]any); !ok {
			return typeError(path, want, value)
		}
	case SchemaTypeArray:
		if _, ok := value.([]any); !ok {
			return typeError(path, want, value)
		}
	case SchemaTypeString:
		if _, ok := value.(string); !ok {
			return typeError(path, want, value)
		}
	case SchemaTypeBoolean:
		if _, ok := value.(bool); !ok {
			return typeError(path, want, value)
		}
	case SchemaTypeNumber:
		if _, ok := value.(float64); !ok {
			return typeError(path, want, value)
		}
	case SchemaTypeInteger:
		number, ok := value.(float64)
		if !ok || number != math.Trunc(number) || math.IsInf(number, 0) {
			return typeError(path, want, value)
		}
	case "":
		return nil
	default:
		// A schema type this build does not know cannot be enforced. Refusing
		// is the only honest answer: accepting would let an unchecked value
		// reach a tool body.
		return fmt.Errorf("%w: %s uses unsupported schema type %q", ErrInvalidValue, path, want)
	}
	return nil
}

// typeError builds the message for a type mismatch, naming what arrived so a
// model can correct itself.
func typeError(path string, want JSONSchemaType, value any) error {
	return fmt.Errorf("%w: %s must be %s, got %s", ErrToolInputInvalid, path, want, jsonKind(value))
}

// jsonKind names a decoded JSON value's kind for diagnostics.
func jsonKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}

// SchemaText renders the schema as compact JSON for a prompt or a diagnostic.
//
// It is a presentation helper only: the pipeline validates against the struct,
// never by re-parsing this text, so the two can never disagree.
func SchemaText(schema InputSchema) (string, error) {
	encoded, err := json.Marshal(struct {
		Type       JSONSchemaType            `json:"type"`
		Properties map[string]PropertySchema `json:"properties,omitempty"`
		Required   []string                  `json:"required,omitempty"`
	}{Type: schema.Type, Properties: schema.Properties, Required: schema.Required})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
