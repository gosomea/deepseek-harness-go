// Package llm defines the conversation vocabulary shared by every later
// subsystem: message roles, content blocks, tool calls and their results.
//
// The types here describe what a model can see. They deliberately know nothing
// about sessions, transports or providers: a message is a value, not a record
// and not a request. Package session stores these values and rebuilds history
// from them; a provider adapter converts them to a wire format.
//
// Vocabulary growth is explicit rather than open-ended. Known content blocks are
// concrete Go types; an unrecognized block is preserved verbatim as an
// UnknownBlock so a durable record survives a round trip without being silently
// rewritten into text. Nothing here parses tool arguments or calls a model.
package llm
