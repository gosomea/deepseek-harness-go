package subagent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/capability/subagent"
)

// Errors this package returns.
var (
	// ErrNoFactory reports a provider without a factory to create children.
	ErrNoFactory = errors.New("subagent: provider has no factory")
	// ErrParentMissing reports a spawn whose parent id the provider does not
	// know, so lineage cannot be recorded.
	ErrParentMissing = errors.New("subagent: parent not known to the provider")
)

// Lineage records one child's origin.
//
// It exists so the parent/child relationship is a recorded fact rather than an
// inference from timing: a reader can answer "who created this child, in what
// mode, inheriting through where" from the record alone.
type Lineage struct {
	// Child is the child Agent's id.
	Child agent.ID
	// Parent is the parent Agent's id.
	Parent agent.ID
	// Mode is spawn or fork.
	Mode subagent.Mode
	// InheritThrough is the parent event count inherited, for a fork. Zero for a
	// spawn.
	InheritThrough int
	// Depth is the child's depth below the root.
	Depth int
}

// Options configures an in-process provider.
type Options struct {
	// Factory creates children. Required.
	Factory agent.Factory
	// ID is the route key. Defaults to "in-process".
	ID string
	// Name is the diagnostics name. Defaults to "In-Process Subagents".
	Name string
}

// Provider is an in-process subagent Provider.
type Provider struct {
	factory agent.Factory
	info    subagent.ProviderInfo

	mu       sync.Mutex
	lineages map[agent.ID]Lineage
	children map[agent.ID]agent.Owner
	order    []agent.ID
}

// New returns an in-process provider.
func New(options Options) (*Provider, error) {
	if options.Factory == nil {
		return nil, fmt.Errorf("%w", ErrNoFactory)
	}
	id := options.ID
	if id == "" {
		id = "in-process"
	}
	name := options.Name
	if name == "" {
		name = "In-Process Subagents"
	}
	return &Provider{
		factory: options.Factory,
		info: subagent.ProviderInfo{
			ID:   id,
			Name: name,
			Capabilities: []subagent.Capability{
				subagent.CapabilitySpawn, subagent.CapabilityFork, subagent.CapabilityContinue,
			},
		},
		lineages: map[agent.ID]Lineage{},
		children: map[agent.ID]agent.Owner{},
	}, nil
}

// Info returns the provider's declared route.
func (p *Provider) Info() subagent.ProviderInfo { return p.info }

// Spawn creates a child through the factory and records its lineage.
//
// The parent must be known (it was created by this provider or registered as a
// root), because a fork that cannot name its parent cannot record what it
// inherited. A spawn still records its parent so "who created this" is always
// answerable.
func (p *Provider) Spawn(ctx context.Context, spec subagent.Spec) (agent.Owner, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	if err := p.requireParentLocked(spec.Parent); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	p.mu.Unlock()

	childSpec := agent.Spec{
		Scope:     spec.ToolScope,
		SessionID: fmt.Sprintf("child:%s:%d", spec.Parent, spec.Depth),
	}
	if childSpec.Scope == "" {
		childSpec.Scope = agent.ScopeKey("subagent")
	}
	owner, err := p.factory.Create(ctx, childSpec)
	if err != nil {
		return nil, err
	}
	lineage := Lineage{
		Child:          owner.Handle().Info().ID,
		Parent:         spec.Parent,
		Mode:           spec.Mode,
		InheritThrough: spec.InheritThrough,
		Depth:          spec.Depth,
	}
	p.mu.Lock()
	p.lineages[lineage.Child] = lineage
	p.children[lineage.Child] = owner
	p.order = append(p.order, lineage.Child)
	p.mu.Unlock()
	return owner, nil
}

// RegisterRoot records a root Agent so children can name it as parent.
//
// A root is created outside the provider (the top-level Agent), so the provider
// must be told it exists before a spawn can attribute a parent to it.
func (p *Provider) RegisterRoot(id agent.ID) error {
	if id.IsZero() {
		return fmt.Errorf("%w: root id is empty", subagent.ErrInvalidSpec)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lineages[id] = Lineage{Child: id, Mode: subagent.ModeSpawn}
	if _, ok := p.children[id]; !ok {
		p.children[id] = nil
	}
	return nil
}

// LineageOf returns the recorded lineage for a child, reporting whether it is
// known.
func (p *Provider) LineageOf(child agent.ID) (Lineage, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	lineage, ok := p.lineages[child]
	return lineage, ok
}

// Children returns the ids of the children this provider created, in order.
func (p *Provider) Children() []agent.ID {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]agent.ID, len(p.order))
	copy(out, p.order)
	return out
}

// DisposeChild disposes one child the provider created.
func (p *Provider) DisposeChild(ctx context.Context, child agent.ID) error {
	p.mu.Lock()
	owner, ok := p.children[child]
	p.mu.Unlock()
	if !ok || owner == nil {
		return fmt.Errorf("%w: %q", ErrParentMissing, child)
	}
	return owner.Dispose(ctx)
}

// requireParentLocked reports whether a parent is known. The caller holds the lock.
func (p *Provider) requireParentLocked(parent agent.ID) error {
	if _, ok := p.lineages[parent]; !ok {
		return fmt.Errorf("%w: %q", ErrParentMissing, parent)
	}
	return nil
}

// compile-time assertion that Provider satisfies the seam.
var _ subagent.Provider = (*Provider)(nil)
