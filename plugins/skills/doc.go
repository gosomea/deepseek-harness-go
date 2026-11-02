// Package skills discovers skills and loads only the ones an agent enabled.
//
// The package exists because "what is available" and "what the model actually
// received" are different facts that are easy to collapse into one. A directory
// of skills is a catalogue; a loaded skill is content that entered a request.
// Treating the catalogue as if it were the request is how an agent ends up with
// instructions it never enabled, or pays token cost for skills it never uses.
//
// Discovery and loading are therefore separate steps with separate results, and a
// loaded skill can be unloaded again — an unloaded skill must leave nothing
// behind in the request.
package skills
