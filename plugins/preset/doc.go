// Package preset registers named configuration bundles an agent can be created
// from.
//
// The package exists because the same three questions come up whenever an agent
// starts: which named bundle was asked for, what did it actually contribute, and
// what does an agent do when the bundle it named is not registered. Answering the
// last one by silently starting with an empty bundle turns a configuration
// mistake into a mysteriously quiet agent.
//
// A registry therefore refuses unknown names, records each bundle's contributions
// in a stated order, and reports which bundle supplied each value.
package preset
