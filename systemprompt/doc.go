// Package systemprompt assembles the ordered prompt text and tool schemas a
// model request carries.
//
// The package exists so that "what the model saw" is a value that can be built
// twice and compared, rather than the incidental result of map iteration order
// or plugin registration timing. Fragments are contributed with an explicit
// order, assembly is a pure function of those fragments, and the same
// fragments always assemble to the same bytes.
package systemprompt
