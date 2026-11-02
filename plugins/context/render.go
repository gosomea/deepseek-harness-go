package context

import (
	"fmt"
	"sort"
	"strings"
)

// EventType is the session-log event type this package contributes.
//
// It is exported because the log registry must be told about it, and a string
// literal repeated at two call sites is how a rename silently stops matching.
const EventType = "context.contribution"

// Request is the rendered form of a recorded contribution set.
//
// It is the request-side value: text plus the citations that produced it. The
// citations are kept so a reader can go from a line in the request back to the
// log fact that put it there.
type Request struct {
	// Text is the assembled instruction text, in render order.
	Text string
	// CitedIDs are the contribution ids that produced Text, in the same order.
	// The renderer never emits text without a citation.
	CitedIDs []string
	// References are the request-time access paths the request cites, sorted by
	// path. They are paths, not contents: resolving them is the consumer's job.
	References []Reference
}

// Render turns a recorded contribution set into request text.
//
// The invariant this function enforces is the package's reason to exist: every
// line of output is attributable to a recorded contribution. It therefore
// returns ErrNotRecorded rather than rendering from anything the set does not
// own, and it never invents placeholder text for a missing contribution.
//
// Output is a pure function of (set, scope): the same pair renders the same
// bytes, so a test can compare two renders directly.
func Render(set *Set, scope string) (Request, error) {
	if set == nil {
		return Request{}, fmt.Errorf("%w: no contribution set", ErrNotRecorded)
	}
	ordered := set.Ordered(scope)
	request := Request{}
	var builder strings.Builder
	for index, contribution := range ordered {
		if index > 0 {
			builder.WriteString("\n\n")
		}
		switch contribution.Kind {
		case KindInstruction, KindTime:
			builder.WriteString(contribution.Text)
		case KindReference:
			// A reference renders as a citation line. The content behind the
			// path is not read here: reading it would make the render depend on
			// the filesystem, and the log would no longer determine the request.
			builder.WriteString("reference: ")
			builder.WriteString(contribution.Reference.Path)
			if note := strings.TrimSpace(contribution.Reference.Note); note != "" {
				builder.WriteString(" (")
				builder.WriteString(note)
				builder.WriteString(")")
			}
			request.References = append(request.References, *contribution.Reference)
		default:
			// Ordered only returns validated contributions, so this is
			// unreachable through the public API. It stays because a future kind
			// added without a render rule must fail loudly instead of vanishing
			// from the request.
			return Request{}, fmt.Errorf("%w: no render rule for kind %q", ErrNotRecorded, contribution.Kind)
		}
		request.CitedIDs = append(request.CitedIDs, contribution.ID)
	}
	request.Text = builder.String()
	sort.SliceStable(request.References, func(i, j int) bool {
		return request.References[i].Path < request.References[j].Path
	})
	return request, nil
}

// Rebuild renders the same set twice and reports whether the two agree.
//
// It exists so a caller can assert the rebuildable property directly instead of
// comparing strings by hand, and so the property has one implementation that a
// test and a runtime check share.
func Rebuild(set *Set, scope string) (Request, error) {
	first, err := Render(set, scope)
	if err != nil {
		return Request{}, err
	}
	second, err := Render(set, scope)
	if err != nil {
		return Request{}, err
	}
	if first.Text != second.Text {
		return Request{}, fmt.Errorf("%w: two renders of one set differ", ErrNotRecorded)
	}
	if len(first.CitedIDs) != len(second.CitedIDs) {
		return Request{}, fmt.Errorf("%w: two renders cite different counts", ErrNotRecorded)
	}
	for index := range first.CitedIDs {
		if first.CitedIDs[index] != second.CitedIDs[index] {
			return Request{}, fmt.Errorf("%w: two renders cite different ids at %d", ErrNotRecorded, index)
		}
	}
	return first, nil
}

// MemoryLog is the append surface this package writes contributions to.
//
// It is a small interface rather than a concrete *session.Log so the package
// depends on "a place facts are recorded" and not on the session log's full
// shape. The session log satisfies it.
type MemoryLog interface {
	// AppendContribution records one contribution and returns its identity.
	AppendContribution(contribution Contribution) (string, error)
}

// Recorder writes contributions to the log before anything renders them.
//
// It is the enforcement point for the package invariant: a caller that has a
// Recorder cannot reach Render with an unrecorded contribution, because Render
// takes a Set and a Set is only populated through Add, which the recorder calls
// after the append succeeded.
type Recorder struct {
	log MemoryLog
}

// NewRecorder returns a recorder writing to log.
func NewRecorder(log MemoryLog) *Recorder { return &Recorder{log: log} }

// Record appends contributions in order, stopping at the first failure.
//
// It records before rendering by construction: the returned set contains exactly
// the contributions the log accepted. A rejected contribution is not added, so a
// later render cannot cite something the log never saw.
func (r *Recorder) Record(contributions ...Contribution) (*Set, error) {
	if r == nil || r.log == nil {
		return nil, fmt.Errorf("%w: no log to record to", ErrNotRecorded)
	}
	set := NewSet()
	for _, contribution := range contributions {
		if err := contribution.Validate(); err != nil {
			return nil, err
		}
		if _, err := r.log.AppendContribution(contribution); err != nil {
			return nil, err
		}
		if err := set.Add(contribution); err != nil {
			return nil, err
		}
	}
	return set, nil
}
