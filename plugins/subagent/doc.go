// Package subagent provides an in-process subagent Provider.
//
// The provider runs a child Agent through an agent.Factory in the same process,
// so a parent can spawn, fork and control a child without any transport. It
// exists as a separate package because "how a child runs" must not leak into the
// agent package: the seam in capability/subagent names what a caller needs, and
// this package is one way to satisfy it.
//
// A fork is expressed as an inheritance fact, never as a replay that fabricates
// new child facts. The provider records that a fork inherited through a given
// parent event and hands the child a lineage entry; it does not copy the parent's
// session so that the child "is" those events. That distinction is what makes the
// two logs independently explainable.
package subagent
