package agentloop

import "github.com/gosomea/deepseek-harness-go/tools"

// fragmentSource is a minimal view of a prompt fragment set.
//
// It exists so this package depends on the shape it needs (an ordered render)
// rather than on the whole systemprompt builder. That keeps the driver free to
// be driven by a different prompt source later without changing this package's
// import list, and it is why the field is a function pair rather than a concrete
// type.
type fragmentSource struct {
	// render returns the prompt text for one scope.
	render func(scope string) (string, error)
	// toolDefinitions returns the tools visible in one scope.
	toolDefinitions func(scope string) []tools.Definition
}

// NewFragmentSource builds a prompt source from the two functions the driver
// needs. Either may be nil, in which case that part contributes nothing rather
// than failing: a driver with no system prompt is a legitimate minimal Agent.
func NewFragmentSource(
	render func(scope string) (string, error),
	toolDefinitions func(scope string) []tools.Definition,
) *fragmentSource {
	return &fragmentSource{render: render, toolDefinitions: toolDefinitions}
}

// prompt returns the rendered system prompt for one scope.
func (s *fragmentSource) prompt(scope string) (string, error) {
	if s == nil || s.render == nil {
		return "", nil
	}
	return s.render(scope)
}

// toolsFor returns the tool definitions visible in one scope.
func (s *fragmentSource) toolsFor(scope string) []tools.Definition {
	if s == nil || s.toolDefinitions == nil {
		return nil
	}
	return s.toolDefinitions(scope)
}
