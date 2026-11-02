package tools

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds the tools one scope can address.
//
// It is safe for concurrent use and it owns its own state rather than sharing a
// map with the runtime: a tool definition registered by one component must not
// be visible to a scope that never received it, which is why lookup goes
// through this type and not through a package-level variable.
type Registry struct {
	mu    sync.RWMutex
	tools map[ToolName]Definition
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[ToolName]Definition)}
}

// Register adds one definition.
//
// A nil executor, an empty name, a missing description, an invalid schema and a
// duplicate name are all refused: a duplicate would let the last registration
// win silently, which makes "which tool ran" untraceable.
func (r *Registry) Register(definition Definition) error {
	if err := definition.Validate(); err != nil {
		return err
	}
	if definition.Execute == nil {
		return fmt.Errorf("%w: tool %q requires an executor", ErrInvalidValue, definition.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[definition.Name]; exists {
		return fmt.Errorf("%w: tool %q is already registered", ErrInvalidValue, definition.Name)
	}
	r.tools[definition.Name] = definition
	return nil
}

// RegisterDefinition adds a definition that has no body. It exists for schema,
// prompt and policy tests that must not be able to execute anything.
func (r *Registry) RegisterDefinition(definition Definition) error {
	if err := definition.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[definition.Name]; exists {
		return fmt.Errorf("%w: tool %q is already registered", ErrInvalidValue, definition.Name)
	}
	r.tools[definition.Name] = definition
	return nil
}

// Unregister removes one tool, reporting whether it was present.
func (r *Registry) Unregister(name ToolName) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; !exists {
		return false
	}
	delete(r.tools, name)
	return true
}

// Lookup returns one definition.
func (r *Registry) Lookup(name ToolName) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definition, exists := r.tools[name]
	return definition, exists
}

// List returns the definitions sorted by name, so a prompt assembly and a
// diagnostic listing are deterministic rather than map-order dependent.
func (r *Registry) List() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]Definition, 0, len(r.tools))
	for _, definition := range r.tools {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions
}

// Names returns the registered tool names in sorted order.
func (r *Registry) Names() []ToolName {
	definitions := r.List()
	names := make([]ToolName, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

// Len returns how many tools are registered.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Clone returns an independent copy.
//
// Scope visibility is built by copying and then removing entries, so a child
// scope's removals must not be able to affect its parent.
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := &Registry{tools: make(map[ToolName]Definition, len(r.tools))}
	for name, definition := range r.tools {
		clone.tools[name] = definition
	}
	return clone
}
