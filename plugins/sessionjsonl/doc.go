// Package sessionjsonl stores a Session log as one JSON Lines file per session
// under a provider-owned data directory.
//
// The format is this project's own. It deliberately does not reuse DSH's
// generation names or layout: reading a foreign implementation's session file
// is a compatibility question with its own fixtures, and borrowing the name
// without the behaviour would be the worst of both. The artifact therefore
// carries its own format id and version, and writes only under the directory it
// was configured with.
//
// One line is one complete record. A partially written final line is a torn
// tail, which recovery drops; a damaged line in the middle is a corrupt log,
// which is refused rather than silently shortened.
package sessionjsonl
