package tools

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidValue reports a value that cannot enter the tool vocabulary.
var ErrInvalidValue = errors.New("tools: invalid value")

// ErrToolNotFound reports a call naming a tool the registry does not hold.
var ErrToolNotFound = errors.New("tools: tool not found")

// ErrToolDenied reports a call refused by policy before its body ran.
var ErrToolDenied = errors.New("tools: tool denied by policy")

// ErrToolInputInvalid reports arguments that do not satisfy the tool's schema.
// It is separate from ErrToolDenied on purpose: "your arguments are wrong" and
// "you are not allowed" are different facts, and a caller retries them
// differently.
var ErrToolInputInvalid = errors.New("tools: tool input invalid")

// ToolName is a tool's session-local identifier.
type ToolName string

// String returns the raw name.
func (n ToolName) String() string { return string(n) }

// IsZero reports whether the name is unset.
func (n ToolName) IsZero() bool { return n == "" }

// JSONSchemaType is the small subset of JSON Schema this package validates.
//
// The subset is deliberately closed: a tool's arguments are validated at the
// boundary where a malformed value would otherwise reach a real side effect, so
// the rules must be few enough to reason about and to test exhaustively.
type JSONSchemaType string

// The JSON schema types this package understands.
const (
	// SchemaTypeObject is a JSON object.
	SchemaTypeObject JSONSchemaType = "object"
	// SchemaTypeString is a JSON string.
	SchemaTypeString JSONSchemaType = "string"
	// SchemaTypeNumber is a JSON number.
	SchemaTypeNumber JSONSchemaType = "number"
	// SchemaTypeInteger is an integral JSON number.
	SchemaTypeInteger JSONSchemaType = "integer"
	// SchemaTypeBoolean is a JSON boolean.
	SchemaTypeBoolean JSONSchemaType = "boolean"
	// SchemaTypeArray is a JSON array.
	SchemaTypeArray JSONSchemaType = "array"
)

// InputSchema describes a tool's accepted arguments.
//
// Only the constraints this project actually enforces are represented. Adding a
// keyword means adding its check; a keyword that were merely stored and ignored
// would make the schema a lie a model could rely on.
type InputSchema struct {
	// Type is the accepted JSON type of the whole value.
	Type JSONSchemaType `json:"type"`
	// Properties names the accepted object members.
	Properties map[string]PropertySchema `json:"properties,omitempty"`
	// Required lists the object members that must be present.
	Required []string `json:"required,omitempty"`
	// AdditionalProperties allows object members not listed in Properties.
	AdditionalProperties bool `json:"additionalProperties,omitempty"`
	// Items describes the element type of an array.
	Items *PropertySchema `json:"items,omitempty"`
}

// PropertySchema describes one accepted value.
type PropertySchema struct {
	// Type is the accepted JSON type.
	Type JSONSchemaType `json:"type"`
	// Description explains the value to the model.
	Description string `json:"description,omitempty"`
	// Enum restricts a value to a closed set.
	Enum []string `json:"enum,omitempty"`
	// Items describes array elements when Type is array.
	Items *PropertySchema `json:"items,omitempty"`
}

// Validate reports whether the schema is usable.
//
// A schema with no type cannot be enforced, and a required member that is not
// described cannot be checked, so both are refused at definition time rather
// than silently accepting everything at call time.
func (s InputSchema) Validate() error {
	if s.Type == "" {
		return fmt.Errorf("%w: schema requires a type", ErrInvalidValue)
	}
	for _, name := range s.Required {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%w: required member name must not be empty", ErrInvalidValue)
		}
		if s.Type == SchemaTypeObject {
			if _, ok := s.Properties[name]; !ok {
				return fmt.Errorf("%w: required member %q must be described in properties", ErrInvalidValue, name)
			}
		}
	}
	if s.Type == SchemaTypeArray && s.Items == nil {
		return fmt.Errorf("%w: array schema requires items", ErrInvalidValue)
	}
	return nil
}

// Definition is one registered tool.
type Definition struct {
	// Name is the session-local identifier a model calls.
	Name ToolName `json:"name"`
	// Description explains the tool to the model.
	Description string `json:"description"`
	// Schema describes the accepted arguments.
	Schema InputSchema `json:"schema"`
	// ReadOnly declares that execution has no side effects, which policy may
	// use to admit the call without approval.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Execute is the tool's body. A nil body is a definition-only entry used
	// for schema and policy tests; the pipeline refuses to execute it.
	Execute Executor `json:"-"`
	// Owner records which component contributed the definition, so visibility
	// and release can be attributed.
	Owner string `json:"owner,omitempty"`
}

// Validate reports whether the definition is complete.
func (d Definition) Validate() error {
	if d.Name.IsZero() {
		return fmt.Errorf("%w: tool requires a name", ErrInvalidValue)
	}
	if strings.TrimSpace(d.Description) == "" {
		return fmt.Errorf("%w: tool %q requires a description", ErrInvalidValue, d.Name)
	}
	return d.Schema.Validate()
}

// Executor runs one admitted call.
//
// It takes one Context parameter, which already carries the call id, the tool
// name, the raw argument string and the decoded value: passing the Call again
// would give a body two places to read the same fact and two chances to
// disagree. The body never repeats schema work, because it only runs after
// validation succeeded.
type Executor func(execution Context) (Result, error)

// Context carries the facts a tool body needs.
//
// It is a struct rather than context.Context because it is data about the call,
// not cancellation: cancellation travels in the embedded standard context.
// Tools that need neither still receive it unchanged.
type Context struct {
	// Name is the tool being executed.
	Name ToolName
	// Arguments is the decoded, schema-validated argument value.
	Arguments map[string]any
	// Raw is the raw argument string exactly as the model produced it.
	Raw string
	// CallID correlates this execution with the model's request.
	CallID string
}

// Call is one model-requested tool invocation.
type Call struct {
	// ID is the provider-issued call id, which the result must answer.
	ID string `json:"id"`
	// Name is the requested tool.
	Name ToolName `json:"name"`
	// Arguments is the raw JSON string exactly as the model produced it.
	Arguments string `json:"arguments"`
}

// Validate reports whether the call is structurally usable.
func (c Call) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("%w: tool call requires an id", ErrInvalidValue)
	}
	if c.Name.IsZero() {
		return fmt.Errorf("%w: tool call requires a name", ErrInvalidValue)
	}
	return nil
}

// ResultOutcome classifies how a call ended.
//
// The outcome is explicit rather than derived from the presence of an error,
// because a denial, an argument error and an execution failure are all
// "not a successful result" yet must be presented and retried differently.
type ResultOutcome string

// The outcomes this package produces.
const (
	// OutcomeSuccess means the tool body ran and returned a value.
	OutcomeSuccess ResultOutcome = "success"
	// OutcomeInputInvalid means the arguments failed schema validation and the
	// body never ran.
	OutcomeInputInvalid ResultOutcome = "input-invalid"
	// OutcomeDenied means policy refused the call and the body never ran.
	OutcomeDenied ResultOutcome = "denied"
	// OutcomeFailed means the body ran and reported an error.
	OutcomeFailed ResultOutcome = "failed"
	// OutcomeCanceled means the call was abandoned because the caller canceled.
	OutcomeCanceled ResultOutcome = "canceled"
)

// Result is the normalized outcome of one call.
//
// Every pipeline path produces exactly one Result, which is what makes "the
// final result is unique" checkable: there is no path that silently produces
// nothing, and no path that produces two.
type Result struct {
	// CallID answers the model's call.
	CallID string `json:"callId"`
	// Name is the tool that was requested.
	Name ToolName `json:"name"`
	// Outcome classifies how the call ended.
	Outcome ResultOutcome `json:"outcome"`
	// Content is the model-visible result text. It is set for every outcome,
	// including failures, so a model always learns what happened.
	Content string `json:"content"`
	// IsError marks a result the model should treat as a failure.
	IsError bool `json:"isError,omitempty"`
	// Attempts records how many times the body actually ran. It is zero for
	// input errors and denials, which is precisely the fact a denial test
	// asserts.
	Attempts int `json:"attempts"`
	// Err carries the underlying error, when there was one. It is not
	// serialized: the durable form is Content and IsError.
	Err error `json:"-"`
}

// Validate reports whether the result is usable.
func (r Result) Validate() error {
	if strings.TrimSpace(r.CallID) == "" {
		return fmt.Errorf("%w: result requires a callId", ErrInvalidValue)
	}
	if r.Name.IsZero() {
		return fmt.Errorf("%w: result requires a name", ErrInvalidValue)
	}
	if r.Outcome == "" {
		return fmt.Errorf("%w: result requires an outcome", ErrInvalidValue)
	}
	return nil
}
