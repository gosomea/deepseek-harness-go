package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// offloadProjection is a test interpreter standing in for the real one the
// reference ships for `image/offload`: it replaces an existing message's content
// with placeholder text. It proves the projection seam without needing the
// attachment service that owns images.
type offloadProjection struct {
	// placeholder is the text a rewritten message receives.
	placeholder string
	// failAt makes Project fail when it is asked to interpret that position.
	failAt *Seq
	// calls counts how many times Project ran, so a test can prove caching and
	// that a rejected append still ran the interpreter.
	calls int
	// mutateInput makes the projection write into the context it was given, which
	// the contract forbids; a test uses it to prove the log is not corrupted.
	mutateInput bool
}

// Type reports the event type this interpreter owns.
func (p *offloadProjection) Type() string { return "image/offload" }

// Project replaces the content of the message at the event's target position.
func (p *offloadProjection) Project(event Event, context ProjectionContext) (map[Seq]json.RawMessage, error) {
	p.calls++
	if p.failAt != nil && *p.failAt == event.Seq {
		return nil, errors.New("offload rejected by the interpreter")
	}
	var payload struct {
		// Target is the surface position whose message is rewritten.
		Target Seq `json:"target"`
	}
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, err
	}
	if p.mutateInput {
		context.Nodes = append(context.Nodes, 999)
	}
	// Find the target event in the window so the replacement keeps its identity.
	var target Event
	for _, candidate := range context.Events {
		if candidate.Seq == payload.Target {
			target = candidate
			break
		}
	}
	if target.Seq == 0 && payload.Target != 0 {
		return nil, errors.New("target is outside the projection window")
	}
	message, err := llm.UnmarshalMessage(targetMessageData(target))
	if err != nil {
		return nil, err
	}
	rewritten := llm.Message{
		ID:      message.ID,
		Role:    message.Role,
		Content: []llm.ContentBlock{llm.TextBlock{Text: p.placeholder}},
		Source:  message.Source,
	}
	encoded, err := llm.MarshalMessage(rewritten)
	if err != nil {
		return nil, err
	}
	return map[Seq]json.RawMessage{payload.Target: encoded}, nil
}

// targetMessageData reads the message payload out of a user or nested event.
func targetMessageData(event Event) json.RawMessage {
	if event.Type == "user/message" {
		return event.Data
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &fields); err != nil {
		return nil
	}
	return fields["message"]
}

// offloadEvent builds one `image/offload` event targeting a surface position.
func offloadEvent(seq Seq, target Seq) Event {
	return Event{Type: "image/offload", Seq: seq, Time: TimeMS(seq),
		Data: json.RawMessage(`{"target":` + itoa(target) + `}`)}
}

// itoa renders a sequence number as a decimal string.
func itoa(seq Seq) string {
	if seq == 0 {
		return "0"
	}
	var digits []byte
	for seq > 0 {
		digits = append([]byte{byte('0' + seq%10)}, digits...)
		seq /= 10
	}
	return string(digits)
}

// TestProjectionRewritesExistingMessageContent pins the projection contract: it
// changes the content of a position already on the surface and preserves that
// message's identity.
func TestProjectionRewritesExistingMessageContent(t *testing.T) {
	projection := &offloadProjection{placeholder: "[image omitted]"}
	events := []Event{
		userEvent(0, "u1"),
		offloadEvent(1, 0),
	}
	surface, err := FoldSurface(events, projection)
	if err != nil {
		t.Fatalf("FoldSurface() error = %v", err)
	}
	// A projection adds no surface node: it rewrites content, it does not produce
	// history.
	if len(surface.Nodes) != 1 || surface.Nodes[0] != 0 {
		t.Fatalf("surface nodes = %v, want the original message only", surface.Nodes)
	}
	if surface.ContentGeneration != 1 || surface.ReplaceGeneration != 0 {
		t.Fatalf("generations = %d/%d, want content 1 and replace 0",
			surface.ContentGeneration, surface.ReplaceGeneration)
	}
	messages, err := surface.DeriveMessages(events)
	if err != nil {
		t.Fatalf("DeriveMessages() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("derived %d messages, want one", len(messages))
	}
	if textOf(messages[0]) != "[image omitted]" {
		t.Fatalf("history = %q, want the placeholder", textOf(messages[0]))
	}
	// Identity is preserved: a rewrite must not invent a new message.
	original, err := llm.UnmarshalMessage(events[0].Data)
	if err != nil {
		t.Fatalf("UnmarshalMessage() error = %v", err)
	}
	if messages[0].ID != original.ID {
		t.Fatalf("message id changed: %q -> %q", original.ID, messages[0].ID)
	}
}

// TestRequiredProjectionIsRefusedWithoutAnInterpreter pins the four-path rule: an
// event type that requires an interpreter is refused rather than silently ignored,
// because ignoring it would rebuild history without the change.
func TestRequiredProjectionIsRefusedWithoutAnInterpreter(t *testing.T) {
	if !RequiresProjection("image/offload") {
		t.Fatal("RequiresProjection(image/offload) = false, want the type to require one")
	}
	if RequiresProjection("user/message") {
		t.Fatal("RequiresProjection(user/message) = true, want only plugin-owned types")
	}
	events := []Event{userEvent(0, "u1"), offloadEvent(1, 0)}
	_, err := FoldSurface(events)
	if err == nil || !strings.Contains(err.Error(), "requires a message projection") {
		t.Fatalf("FoldSurface() = %v, want the missing interpreter refused", err)
	}
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("FoldSurface() = %v, want ErrInvalidEvent", err)
	}
	// The live append path refuses it too, and leaves the log untouched.
	log := NewLog(KnownTypes())
	if _, err := log.Append(AppendInput{Type: "user/message", Data: userEvent(0, "u1").Data,
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if _, err := log.Append(AppendInput{Type: "image/offload", Data: []byte(`{"target":0}`)}); err == nil {
		t.Fatal("Append() = nil, want the missing interpreter refused")
	}
	if log.Len() != 1 {
		t.Fatalf("Len() = %d, want the refused append not stored", log.Len())
	}
}

// TestProjectionFailureAbortsAtomically pins that a rejected decision leaves the
// log, the surface and the derived history unchanged.
func TestProjectionFailureAbortsAtomically(t *testing.T) {
	log := NewLog(KnownTypes())
	position := Seq(1)
	projection := &offloadProjection{placeholder: "[x]", failAt: &position}
	remove, err := log.RegisterProjection(projection)
	if err != nil {
		t.Fatalf("RegisterProjection() error = %v", err)
	}
	defer remove()
	if _, err := log.Append(AppendInput{Type: "user/message", Data: userEvent(0, "u1").Data,
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	before := log.Surface()
	if _, err := log.Append(AppendInput{Type: "image/offload", Data: []byte(`{"target":0}`)}); err == nil {
		t.Fatal("Append() = nil, want the interpreter's rejection propagated")
	}
	if log.Len() != 1 || log.ContentGeneration() != before.ContentGeneration {
		t.Fatalf("the rejected projection changed the log: Len=%d generation=%d", log.Len(), log.ContentGeneration())
	}
	messages, err := log.DeriveMessages()
	if err != nil {
		t.Fatalf("DeriveMessages() error = %v", err)
	}
	if len(messages) != 1 || textOf(messages[0]) != "u1" {
		t.Fatalf("history = %v, want the original content", texts(messages))
	}
}

// TestDuplicateProjectionRegistrationIsRefused pins that one event type has one
// interpreter: two would make history depend on registration order.
func TestDuplicateProjectionRegistrationIsRefused(t *testing.T) {
	log := NewLog(KnownTypes())
	first := &offloadProjection{placeholder: "a"}
	remove, err := log.RegisterProjection(first)
	if err != nil {
		t.Fatalf("RegisterProjection() error = %v", err)
	}
	if _, err := log.RegisterProjection(&offloadProjection{placeholder: "b"}); err == nil {
		t.Fatal("RegisterProjection(duplicate) = nil, want an error")
	}
	if _, err := log.RegisterProjection(nil); err == nil {
		t.Fatal("RegisterProjection(nil) = nil, want an error")
	}
	if _, err := log.RegisterProjection(&unnamedProjection{}); err == nil {
		t.Fatal("RegisterProjection(empty type) = nil, want an error")
	}
	// Removal is idempotent and frees the type for a new interpreter.
	remove()
	remove()
	second := &offloadProjection{placeholder: "b"}
	if _, err := log.RegisterProjection(second); err != nil {
		t.Fatalf("RegisterProjection() after removal = %v, want the type free", err)
	}
}

// unnamedProjection reports an empty type, which registration must refuse.
type unnamedProjection struct{}

// Type reports an empty event type.
func (unnamedProjection) Type() string { return "" }

// Project never runs: registration is refused first.
func (unnamedProjection) Project(Event, ProjectionContext) (map[Seq]json.RawMessage, error) {
	return nil, errors.New("unreachable")
}

// TestProjectionOutputMustBeAMessageForAnExistingPosition pins that an interpreter
// cannot invent history or return a non-message.
func TestProjectionOutputMustBeAMessageForAnExistingPosition(t *testing.T) {
	cases := []struct {
		name    string
		output  map[Seq]json.RawMessage
		wantErr string
	}{
		{"empty payload", map[Seq]json.RawMessage{0: nil}, "empty message"},
		{"not a message", map[Seq]json.RawMessage{0: json.RawMessage(`{"nope":1}`)}, "is not a message"},
		{"invented position", map[Seq]json.RawMessage{99: json.RawMessage(
			`{"id":"x","role":"user","content":[{"type":"text","text":"x"}],"source":{"kind":"user"}}`)}, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			projection := &fixedOutputProjection{output: testCase.output}
			events := []Event{userEvent(0, "u1"), offloadEvent(1, 0)}
			surface, err := FoldSurface(events, projection)
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("FoldSurface() = %v, want error containing %q", err, testCase.wantErr)
				}
				return
			}
			// An invented position is recorded in the projection cache but never
			// becomes a surface node, so it cannot add model history.
			if err != nil {
				t.Fatalf("FoldSurface() error = %v", err)
			}
			if len(surface.Nodes) != 1 || surface.Nodes[0] != 0 {
				t.Fatalf("surface nodes = %v, want only the real message", surface.Nodes)
			}
		})
	}
}

// fixedOutputProjection returns a fixed map so a test can probe the validation of
// a projection's output.
type fixedOutputProjection struct {
	output map[Seq]json.RawMessage
}

// Type reports the event type this interpreter owns.
func (fixedOutputProjection) Type() string { return "image/offload" }

// Project returns the canned output.
func (p *fixedOutputProjection) Project(Event, ProjectionContext) (map[Seq]json.RawMessage, error) {
	return p.output, nil
}

// TestProjectionInputIsNotSharedWithTheLog pins that an interpreter writing into
// the context it was handed cannot corrupt the surface.
func TestProjectionInputIsNotSharedWithTheLog(t *testing.T) {
	log := NewLog(KnownTypes())
	projection := &offloadProjection{placeholder: "[x]", mutateInput: true}
	remove, err := log.RegisterProjection(projection)
	if err != nil {
		t.Fatalf("RegisterProjection() error = %v", err)
	}
	defer remove()
	if _, err := log.Append(AppendInput{Type: "user/message", Data: userEvent(0, "u1").Data,
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if _, err := log.Append(AppendInput{Type: "image/offload", Data: []byte(`{"target":0}`)}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	nodes := log.SurfaceNodes()
	if len(nodes) != 1 || nodes[0] != 0 {
		t.Fatalf("surface nodes = %v, want the mutation to have no effect", nodes)
	}
}

// TestProjectionIsPureAcrossRepeatedFolds pins determinism: folding the same log
// twice with the same interpreter yields the same history. This is the property a
// cache and a replay both rely on.
func TestProjectionIsPureAcrossRepeatedFolds(t *testing.T) {
	events := []Event{userEvent(0, "u1"), offloadEvent(1, 0), userEvent(2, "u2")}
	first, err := DeriveMessages(events, &offloadProjection{placeholder: "[x]"})
	if err != nil {
		t.Fatalf("DeriveMessages() error = %v", err)
	}
	second, err := DeriveMessages(events, &offloadProjection{placeholder: "[x]"})
	if err != nil {
		t.Fatalf("DeriveMessages() error = %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("histories differ in length: %d vs %d", len(first), len(second))
	}
	for index := range first {
		if first[index].ID != second[index].ID || textOf(first[index]) != textOf(second[index]) {
			t.Fatalf("history %d differs: %+v vs %+v", index, first[index], second[index])
		}
	}
	// The interpreter only runs for its own event type, not for every event.
	projection := &offloadProjection{placeholder: "[x]"}
	if _, err := FoldSurface(events, projection); err != nil {
		t.Fatalf("FoldSurface() error = %v", err)
	}
	if projection.calls != 1 {
		t.Fatalf("projection ran %d times, want once for its own event type", projection.calls)
	}
}

// TestProjectionRemovedAfterUseIsReported pins that a missing interpreter is
// caught on the next fold instead of silently dropping the change.
func TestProjectionRemovedAfterUseIsReported(t *testing.T) {
	events := []Event{userEvent(0, "u1"), offloadEvent(1, 0)}
	projection := &offloadProjection{placeholder: "[x]"}
	if _, err := FoldSurface(events, projection); err != nil {
		t.Fatalf("FoldSurface() error = %v", err)
	}
	// Without the interpreter the same log is refused, which is the detectable
	// form of "the owning plugin is gone".
	if _, err := FoldSurface(events); err == nil {
		t.Fatal("FoldSurface() = nil, want the log refused without its interpreter")
	}
}
