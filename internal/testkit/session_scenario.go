// Session surface parity: shared scenarios that both the Go session package and
// the fixed TypeScript reference replay, so their observable surface and derived
// history can be compared line by line.
//
// The scenario format is deliberately small: a list of events, optional fold
// options, and the observations to emit. It carries no Go type names, so the
// reference runner reads the same JSON without translation.
package testkit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
)

// SessionScenario is one shared session-surface case.
type SessionScenario struct {
	// ID uniquely identifies the scenario; it matches the file name stem.
	ID string `json:"id"`
	// Title states the single observable behaviour the scenario pins down.
	Title string `json:"title"`
	// Events are applied in order, exactly as written.
	Events []SessionScenarioEvent `json:"events"`
	// Step is the session step the scenario describes, for the trace header.
	Step int `json:"step,omitempty"`
	// ExpectFoldError declares that the fold must fail. When set, the Go runner
	// emits a trace of the rejection instead of a surface, and the reference must
	// reject too.
	ExpectFoldError bool `json:"expectFoldError,omitempty"`
	// Divergences declares trace lines where the two implementations intentionally
	// differ. Every entry must justify itself and must actually occur.
	Divergences []Divergence `json:"divergences,omitempty"`
}

// SessionScenarioEvent is one event in a session scenario.
type SessionScenarioEvent struct {
	// Type is the event discriminator.
	Type string `json:"type"`
	// Seq is the event's absolute position.
	Seq uint64 `json:"seq"`
	// Time is the recorded wall-clock milliseconds.
	Time int64 `json:"time,omitempty"`
	// Data is the payload, kept raw so the scenario is not tied to Go types.
	Data json.RawMessage `json:"data,omitempty"`
	// Ignorable marks an informational record a reader may skip.
	Ignorable bool `json:"ignorable,omitempty"`
	// SurfaceOp is the surface placement: "append" or an object. It is kept raw
	// for the same reason as Data.
	SurfaceOp json.RawMessage `json:"surfaceOp,omitempty"`
	// SourceEventSeqs cites earlier events.
	SourceEventSeqs []uint64 `json:"sourceEventSeqs,omitempty"`
}

// LoadSessionScenario reads and validates one scenario file.
func LoadSessionScenario(path string) (*SessionScenario, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var scenario SessionScenario
	if err := json.Unmarshal(body, &scenario); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if scenario.ID == "" || len(scenario.Events) == 0 {
		return nil, fmt.Errorf("%s: scenario needs an id and events", path)
	}
	// A foldable scenario must be dense from zero. Catching that here turns a
	// mis-numbered fixture into a clear load error instead of a confusing fold
	// failure with the wrong subject.
	if !scenario.ExpectFoldError {
		seen := map[uint64]bool{}
		for index, event := range scenario.Events {
			if seen[event.Seq] {
				return nil, fmt.Errorf("%s: duplicate seq %d in a foldable scenario", path, event.Seq)
			}
			seen[event.Seq] = true
			if event.Seq != uint64(index) {
				return nil, fmt.Errorf("%s: event %d has seq %d; a foldable scenario must be dense from 0",
					path, index, event.Seq)
			}
		}
	}
	return &scenario, nil
}

// SessionScenarioFiles returns the scenario files of a directory in name order.
func SessionScenarioFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// toSessionEvent converts one scenario event into a session event, reproducing the
// exact envelope the payload declares instead of going through the append path.
//
// The append path would reject some of these on purpose — that is what the
// rejection scenarios exercise — so the fold is driven by the literal events.
func toSessionEvent(scenario SessionScenarioEvent) (session.Event, error) {
	event := session.Event{
		Type:            scenario.Type,
		Seq:             session.Seq(scenario.Seq),
		Time:            session.TimeMS(scenario.Time),
		Data:            append(json.RawMessage(nil), scenario.Data...),
		Ignorable:       session.Ignorable(scenario.Ignorable),
		HasIgnorable:    scenario.Ignorable,
		SourceEventSeqs: make([]session.Seq, 0, len(scenario.SourceEventSeqs)),
	}
	for _, seq := range scenario.SourceEventSeqs {
		event.SourceEventSeqs = append(event.SourceEventSeqs, session.Seq(seq))
	}
	if len(event.SourceEventSeqs) == 0 {
		event.SourceEventSeqs = nil
	}
	if len(scenario.SurfaceOp) > 0 {
		op, err := decodeScenarioSurfaceOp(scenario.SurfaceOp)
		if err != nil {
			return session.Event{}, err
		}
		event.SurfaceOp = op
	}
	return event, nil
}

// decodeScenarioSurfaceOp reads "append" or a replace object from the scenario.
func decodeScenarioSurfaceOp(raw json.RawMessage) (*session.SurfaceOp, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == `"append"` {
		return &session.SurfaceOp{Op: session.SurfaceAppend}, nil
	}
	var fields struct {
		// Op is the operation name.
		Op string `json:"op"`
		// StartSeq is the first shadowed node of a replacement.
		StartSeq uint64 `json:"startSeq"`
		// EndSeq is the last shadowed node of a replacement.
		EndSeq uint64 `json:"endSeq"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return &session.SurfaceOp{Op: fields.Op, StartSeq: session.Seq(fields.StartSeq), EndSeq: session.Seq(fields.EndSeq)}, nil
}

// ReplaySessionScenario executes one scenario against the Go session package and
// returns the canonical trace.
//
// The trace records only what both implementations can observe: whether the fold
// succeeded, the surface positions, the replacement report, and the derived
// history's roles and text. Message identifiers are deliberately excluded: the
// reference mints them and this project preserves whatever a payload carries, so
// comparing them would compare a generator rather than a behaviour. The reference
// runner emits the same fields in the same order.
func ReplaySessionScenario(scenario *SessionScenario) (string, error) {
	events := make([]session.Event, 0, len(scenario.Events))
	for index, raw := range scenario.Events {
		event, err := toSessionEvent(raw)
		if err != nil {
			return "", fmt.Errorf("scenario %s: event %d: %w", scenario.ID, index, err)
		}
		events = append(events, event)
	}

	var lines []string
	surface, err := session.FoldSurface(events, sessionProjections(scenario)...)
	if err != nil {
		if !scenario.ExpectFoldError {
			return "", fmt.Errorf("scenario %s: fold failed: %w", scenario.ID, err)
		}
		lines = append(lines, "fold=rejected")
		return strings.Join(lines, "\n") + "\n", nil
	}
	if scenario.ExpectFoldError {
		return "", fmt.Errorf("scenario %s: fold succeeded but the scenario declares a rejection", scenario.ID)
	}
	lines = append(lines, "fold=ok")
	lines = append(lines, "nodes="+formatSeqs(surface.Nodes))
	lines = append(lines, "replaceGeneration="+formatUint(surface.ReplaceGeneration))
	lines = append(lines, "contentGeneration="+formatUint(surface.ContentGeneration))
	if len(surface.Replacements) == 0 {
		lines = append(lines, "replacement=none")
	}
	for _, replacement := range surface.Replacements {
		lines = append(lines, "replacement=seq:"+formatUint(uint64(replacement.Seq))+
			" start:"+formatUint(uint64(replacement.Start))+
			" end:"+formatUint(uint64(replacement.End))+
			" shadowed:"+formatSeqs(replacement.Shadowed))
	}
	messages, err := surface.DeriveMessages(events)
	if err != nil {
		return "", fmt.Errorf("scenario %s: derive failed: %w", scenario.ID, err)
	}
	lines = append(lines, "messages="+formatUint(uint64(len(messages))))
	for _, message := range messages {
		// Roles and visible text are the model-facing facts; reasoning and unknown
		// blocks are reported by kind so a difference in block vocabulary shows up.
		lines = append(lines, "message role:"+string(message.Role)+
			" text:"+quoteText(contentText(message.Content))+
			" kinds:"+strings.Join(contentKinds(message.Content), ","))
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// sessionProjections returns the interpreters the scenario needs.
//
// The reference's `image/offload` type requires an interpreter, and so does this
// package's. A scenario that contains one therefore runs with the equivalent
// interpreter so both sides apply the same rewrite.
func sessionProjections(scenario *SessionScenario) []session.MessageProjection {
	for _, event := range scenario.Events {
		if event.Type == "image/offload" {
			return []session.MessageProjection{placeholderOffload{}}
		}
	}
	return nil
}

// placeholderOffload mirrors the effect of an offload decision: replace the target
// message's content while preserving its identity and producer attribution.
type placeholderOffload struct{}

// Type reports the event type this interpreter owns.
func (placeholderOffload) Type() string { return "image/offload" }

// Project rewrites the target message's content.
func (placeholderOffload) Project(event session.Event, context session.ProjectionContext) (map[session.Seq]json.RawMessage, error) {
	var payload struct {
		// Target is the position whose message is rewritten.
		Target uint64 `json:"target"`
	}
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, err
	}
	var target session.Event
	for _, candidate := range context.Events {
		if uint64(candidate.Seq) == payload.Target {
			target = candidate
		}
	}
	raw := target.Data
	if target.Type != "user/message" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(target.Data, &fields); err != nil {
			return nil, err
		}
		raw = fields["message"]
	}
	message, err := llm.UnmarshalMessage(raw)
	if err != nil {
		return nil, err
	}
	// A projection rewrites content and preserves everything else.
	message.Content = []llm.ContentBlock{llm.TextBlock{Text: "[image omitted]"}}
	encoded, err := llm.MarshalMessage(message)
	if err != nil {
		return nil, err
	}
	return map[session.Seq]json.RawMessage{session.Seq(payload.Target): encoded}, nil
}

// formatSeqs renders a position list the way both runners do.
func formatSeqs(seqs []session.Seq) string {
	parts := make([]string, 0, len(seqs))
	for _, seq := range seqs {
		parts = append(parts, formatUint(uint64(seq)))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// formatUint renders an unsigned integer.
func formatUint(value uint64) string { return fmt.Sprintf("%d", value) }

// contentText concatenates the visible text of a message.
func contentText(blocks []llm.ContentBlock) string { return llm.ContentText(blocks) }

// contentKinds lists each block's discriminator in order, so a difference in block
// vocabulary shows up in the trace even when the visible text matches.
func contentKinds(blocks []llm.ContentBlock) []string {
	kinds := make([]string, 0, len(blocks))
	for _, block := range blocks {
		kinds = append(kinds, string(block.BlockType()))
	}
	return kinds
}

// quoteText renders text as a quoted, escaped string so a difference in whitespace
// is visible.
func quoteText(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		return fmt.Sprintf("%q", text)
	}
	return string(encoded)
}

// CompareSessionTraces compares a Go session trace against a recorded reference
// trace, applying the scenario's declared divergences.
//
// It reuses the Cordis comparator so the session scenarios obey exactly the same
// rules: a declaration is applied only where both sides match it at the same point,
// every declaration must occur, and an undeclared difference fails. That reuse is
// deliberate — a second comparator with slightly different rules would let one
// family of scenarios be laxer than the other.
func CompareSessionTraces(scenario *SessionScenario, reference, got string) error {
	shared := &Scenario{ID: scenario.ID, Title: scenario.Title, Divergences: scenario.Divergences}
	return CompareTraces(shared, reference, got)
}
