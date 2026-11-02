// Package attachment maps stored attachments into what a request may reach.
//
// The package exists because stored content and reachable content are different
// facts. An attachment is stored once under an identity; what a request may read
// is a per-request mapping over that store. A request that could read anything in
// the store would make "what this request was allowed to see" unanswerable.
//
// The mapping is therefore explicit and resolves by identity, and every way it
// can fail — a missing id, an unsupported modality, a different execution world,
// corrupted content — produces a diagnostic rather than an empty result that
// looks like an absent attachment.
package attachment
