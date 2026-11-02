// Package tools defines what a tool is, who may see it, and what happens
// between a model requesting a call and a result being recorded.
//
// The package is deliberately dumb about where execution happens: a tool's
// body comes from a capability provider in some other package, and this package
// owns the ordered pipeline that decides whether the body runs at all. That
// split is what makes "deny before side effects" checkable, and it is why the
// pipeline reports a normalized result for every path, including the paths that
// never executed anything.
package tools
