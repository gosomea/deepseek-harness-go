package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// userEvent builds one append-origin user message event.
func userEvent(seq Seq, id string) Event {
	return Event{
		Type:      "user/message",
		Seq:       seq,
		Time:      TimeMS(seq),
		Data:      json.RawMessage(`{"id":"` + id + `","role":"user","content":[{"type":"text","text":"` + id + `"}],"source":{"kind":"user"}}`),
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend},
	}
}

// logOnlyEvent builds one boundary marker that must not touch the surface.
func logOnlyEvent(seq Seq, eventType string) Event {
	return Event{Type: eventType, Seq: seq, Time: TimeMS(seq), Data: json.RawMessage(`{}`)}
}

// textOf returns the visible text of a message, for assertions.
func textOf(message llm.Message) string { return llm.ContentText(message.Content) }

// TestFoldSurfaceRejectsGaps pins that the fold enforces dense positions from its
// base. A fold that accepted a gap would build a surface for a log that cannot
// exist.
func TestFoldSurfaceRejectsGaps(t *testing.T) {
	_, err := FoldSurface([]Event{logOnlyEvent(0, "turn/start"), logOnlyEvent(5, "turn/start")})
	if !errors.Is(err, ErrSeqGap) {
		t.Fatalf("FoldSurface() = %v, want ErrSeqGap", err)
	}
	// A duplicate is the same failure: the position is not the next one.
	_, err = FoldSurface([]Event{logOnlyEvent(0, "turn/start"), logOnlyEvent(0, "turn/start")})
	if !errors.Is(err, ErrSeqGap) {
		t.Fatalf("FoldSurface(duplicate) = %v, want ErrSeqGap", err)
	}
	if surface, err := FoldSurface(nil); err != nil || len(surface.Nodes) != 0 {
		t.Fatalf("FoldSurface(nil) = %v, %+v, want an empty surface", err, surface)
	}
}

// TestAuditEventsLengthenTheLogWithoutChangingHistory pins the central M3
// behaviour: interleaving non-message events produces an identical model history.
func TestAuditEventsLengthenTheLogWithoutChangingHistory(t *testing.T) {
	plain := []Event{userEvent(0, "u1")}
	interleaved := []Event{
		logOnlyEvent(0, "turn/start"),
		{Type: "user/message", Seq: 1, Time: 1,
			Data:      userEvent(0, "u1").Data,
			SurfaceOp: &SurfaceOp{Op: SurfaceAppend}},
		logOnlyEvent(2, "assistant/attempt"),
		logOnlyEvent(3, "step/start"),
		logOnlyEvent(4, "step/end"),
		logOnlyEvent(5, "turn/end"),
	}
	plainMessages, err := DeriveMessages(plain)
	if err != nil {
		t.Fatalf("DeriveMessages(plain) error = %v", err)
	}
	interleavedMessages, err := DeriveMessages(interleaved)
	if err != nil {
		t.Fatalf("DeriveMessages(interleaved) error = %v", err)
	}
	if len(plainMessages) != 1 || len(interleavedMessages) != 1 {
		t.Fatalf("histories have %d and %d messages, want one each", len(plainMessages), len(interleavedMessages))
	}
	if textOf(plainMessages[0]) != textOf(interleavedMessages[0]) {
		t.Fatalf("history changed: %q vs %q", textOf(plainMessages[0]), textOf(interleavedMessages[0]))
	}
	// The log really did grow, and only the message event is a surface node.
	interleavedSurface, err := FoldSurface(interleaved)
	if err != nil {
		t.Fatalf("FoldSurface() error = %v", err)
	}
	if len(interleaved) != 6 || len(interleavedSurface.Nodes) != 1 {
		t.Fatalf("log has %d events and %d surface nodes, want 6 and 1", len(interleaved), len(interleavedSurface.Nodes))
	}
	if interleavedSurface.Nodes[0] != 1 {
		t.Fatalf("surface node = %d, want the message event's position", interleavedSurface.Nodes[0])
	}
}

// TestEmptyMessagesKeepTheirPlaceButDeriveToNothing pins that an empty system,
// developer or assistant message occupies a surface node while producing no model
// message. This is how "no system prompt" and a max-tokens step are recorded.
func TestEmptyMessagesKeepTheirPlaceButDeriveToNothing(t *testing.T) {
	events := []Event{
		{Type: "system/message", Seq: 0, Time: 1,
			Data:      json.RawMessage(`{"message":{"id":"s1","role":"system","content":[],"source":{"kind":"system-prompt"}}}`),
			SurfaceOp: &SurfaceOp{Op: SurfaceAppend}},
		userEvent(1, "u1"),
		{Type: "assistant/message", Seq: 2, Time: 1,
			Data:      json.RawMessage(`{"message":{"id":"a1","role":"assistant","content":[],"source":{"kind":"model","provider":"p","model":"m"}}}`),
			SurfaceOp: &SurfaceOp{Op: SurfaceAppend}},
	}
	surface, err := FoldSurface(events)
	if err != nil {
		t.Fatalf("FoldSurface() error = %v", err)
	}
	if len(surface.Nodes) != 3 {
		t.Fatalf("surface nodes = %v, want all three events on the surface", surface.Nodes)
	}
	messages, err := surface.DeriveMessages(events)
	if err != nil {
		t.Fatalf("DeriveMessages() error = %v", err)
	}
	if len(messages) != 1 || textOf(messages[0]) != "u1" {
		t.Fatalf("derived %d messages, want only the non-empty one", len(messages))
	}
	// Each empty event individually derives to no message.
	for _, seq := range []Seq{0, 2} {
		message, err := surface.DeriveEventMessage(events[seq])
		if err != nil {
			t.Fatalf("DeriveEventMessage(%d) error = %v", seq, err)
		}
		if message != nil {
			t.Fatalf("DeriveEventMessage(%d) = %+v, want nil for empty content", seq, message)
		}
	}
	// A log-only event derives to nothing either.
	message, err := surface.DeriveEventMessage(logOnlyEvent(0, "turn/start"))
	if err != nil || message != nil {
		t.Fatalf("DeriveEventMessage(log-only) = %+v, %v, want nil", message, err)
	}
}

// TestReplacementShadowsOnlyItsRange pins the replacement splice and the
// replacement report.
func TestReplacementShadowsOnlyItsRange(t *testing.T) {
	events := []Event{
		userEvent(0, "u1"),
		logOnlyEvent(1, "turn/start"),
		userEvent(2, "u2"),
		userEvent(3, "u3"),
		{Type: "user/message", Seq: 4, Time: 1,
			Data:            json.RawMessage(`{"id":"summary","role":"user","content":[{"type":"text","text":"summary"}],"source":{"kind":"user"}}`),
			SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 2},
			SourceEventSeqs: []Seq{0, 2}},
	}
	surface, err := FoldSurface(events)
	if err != nil {
		t.Fatalf("FoldSurface() error = %v", err)
	}
	if len(surface.Nodes) != 2 || surface.Nodes[0] != 4 || surface.Nodes[1] != 3 {
		t.Fatalf("surface nodes = %v, want [4 3]", surface.Nodes)
	}
	if surface.ReplaceGeneration != 1 || surface.ContentGeneration != 1 {
		t.Fatalf("generations = %d/%d, want 1/1", surface.ReplaceGeneration, surface.ContentGeneration)
	}
	if len(surface.Replacements) != 1 {
		t.Fatalf("replacements = %v, want one", surface.Replacements)
	}
	replacement := surface.Replacements[0]
	if replacement.Seq != 4 || replacement.Start != 0 || replacement.End != 2 {
		t.Fatalf("replacement = %+v, want seq 4 over [0,2]", replacement)
	}
	if len(replacement.Shadowed) != 2 || replacement.Shadowed[0] != 0 || replacement.Shadowed[1] != 2 {
		t.Fatalf("shadowed = %v, want the two surface nodes in order", replacement.Shadowed)
	}
	messages, err := surface.DeriveMessages(events)
	if err != nil {
		t.Fatalf("DeriveMessages() error = %v", err)
	}
	if len(messages) != 2 || textOf(messages[0]) != "summary" || textOf(messages[1]) != "u3" {
		t.Fatalf("history = %v, want the summary then u3", texts(messages))
	}
}

// texts renders a message list for failure messages.
func texts(messages []llm.Message) []string {
	rendered := make([]string, 0, len(messages))
	for _, message := range messages {
		rendered = append(rendered, textOf(message))
	}
	return rendered
}

// TestReplacementMustTargetCurrentSurfaceNodes pins that an endpoint which exists
// in the log but is no longer on the surface is refused: it was already shadowed.
func TestReplacementMustTargetCurrentSurfaceNodes(t *testing.T) {
	// Position 0 is shadowed by position 2, so a later replacement cannot name it.
	events := []Event{
		userEvent(0, "u1"),
		userEvent(1, "u2"),
		{Type: "user/message", Seq: 2, Time: 1,
			Data:            json.RawMessage(`{"id":"s","role":"user","content":[{"type":"text","text":"s"}],"source":{"kind":"user"}}`),
			SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 0},
			SourceEventSeqs: []Seq{0}},
		{Type: "user/message", Seq: 3, Time: 1,
			Data:      json.RawMessage(`{"id":"s2","role":"user","content":[{"type":"text","text":"s2"}],"source":{"kind":"user"}}`),
			SurfaceOp: &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 1}},
	}
	_, err := FoldSurface(events)
	if err == nil || !strings.Contains(err.Error(), "not found in surface") {
		t.Fatalf("FoldSurface() = %v, want the shadowed endpoint refused", err)
	}
}

// TestReplacementDirectionAndOrdering pins the remaining range rules: endpoints
// must be earlier than the event, the start must not follow the end, and the
// citations must cover every shadowed node.
func TestReplacementDirectionAndOrdering(t *testing.T) {
	cases := []struct {
		name    string
		replace Event
		want    string
	}{
		{"endpoint in the future", Event{Type: "user/message", Seq: 2, Time: 1,
			Data:      json.RawMessage(`{"id":"x","role":"user","content":[{"type":"text","text":"x"}],"source":{"kind":"user"}}`),
			SurfaceOp: &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 2}}, "earlier events"},
		{"start after end", Event{Type: "user/message", Seq: 2, Time: 1,
			Data:            json.RawMessage(`{"id":"x","role":"user","content":[{"type":"text","text":"x"}],"source":{"kind":"user"}}`),
			SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 1, EndSeq: 0},
			SourceEventSeqs: []Seq{0, 1}}, "is after end seq"},
		{"citations miss a shadowed node", Event{Type: "user/message", Seq: 2, Time: 1,
			Data:            json.RawMessage(`{"id":"x","role":"user","content":[{"type":"text","text":"x"}],"source":{"kind":"user"}}`),
			SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 1},
			SourceEventSeqs: []Seq{0}}, "must include every shadowed surface node"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			events := []Event{userEvent(0, "u1"), userEvent(1, "u2"), testCase.replace}
			_, err := FoldSurface(events)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("FoldSurface() = %v, want error containing %q", err, testCase.want)
			}
		})
	}
}

// TestSystemHeadIsProtected pins that a replacement covering surface node 0 while
// it holds the system prompt must be a system message over exactly that node. A
// compaction range must not silently erase the instructions.
func TestSystemHeadIsProtected(t *testing.T) {
	system := Event{Type: "system/message", Seq: 0, Time: 1,
		Data:      json.RawMessage(`{"message":{"id":"s1","role":"system","content":[{"type":"text","text":"instructions"}],"source":{"kind":"system-prompt"}}}`),
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}
	user := userEvent(1, "u1")

	// A user replacement covering node 0 is refused.
	bad := Event{Type: "user/message", Seq: 2, Time: 1,
		Data:            json.RawMessage(`{"id":"x","role":"user","content":[{"type":"text","text":"x"}],"source":{"kind":"user"}}`),
		SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 0},
		SourceEventSeqs: []Seq{0}}
	if _, err := FoldSurface([]Event{system, user, bad}); err == nil || !strings.Contains(err.Error(), "node 0 holds the system prompt") {
		t.Fatalf("FoldSurface() = %v, want the head rewrite refused", err)
	}
	// A system replacement over exactly node 0 is allowed.
	good := Event{Type: "system/message", Seq: 2, Time: 1,
		Data:            json.RawMessage(`{"message":{"id":"s2","role":"system","content":[{"type":"text","text":"updated"}],"source":{"kind":"system-prompt"}}}`),
		SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 0},
		SourceEventSeqs: []Seq{0}}
	surface, err := FoldSurface([]Event{system, user, good})
	if err != nil {
		t.Fatalf("FoldSurface() error = %v, want the system head rewrite accepted", err)
	}
	messages, err := surface.DeriveMessages([]Event{system, user, good})
	if err != nil {
		t.Fatalf("DeriveMessages() error = %v", err)
	}
	if len(messages) != 2 || textOf(messages[0]) != "updated" {
		t.Fatalf("history = %v, want the updated system prompt first", texts(messages))
	}
	// A later system node carries no protection: node 0 is no longer the system
	// message, so an ordinary range may shadow it.
	withUserHead := Event{Type: "user/message", Seq: 0, Time: 1,
		Data:      json.RawMessage(`{"id":"u1","role":"user","content":[{"type":"text","text":"u1"}],"source":{"kind":"user"}}`),
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}
	overUser := Event{Type: "system/message", Seq: 1, Time: 1,
		Data:            json.RawMessage(`{"message":{"id":"s1","role":"system","content":[{"type":"text","text":"sys"}],"source":{"kind":"system-prompt"}}}`),
		SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 0},
		SourceEventSeqs: []Seq{0}}
	if _, err := FoldSurface([]Event{withUserHead, overUser}); err != nil {
		t.Fatalf("FoldSurface() = %v, want a non-system head to be unprotected", err)
	}
}

// TestToolResultReplacementChangesOnlyContent pins that a result rewrite cannot
// change which call it answers or whether it failed.
func TestToolResultReplacementChangesOnlyContent(t *testing.T) {
	original := Event{Type: "tool/result", Seq: 0, Time: 1,
		Data:      json.RawMessage(`{"message":{"id":"t1","role":"tool","content":[{"type":"text","text":"old"}],"source":{"kind":"tool","callId":"c1"},"toolCallId":"c1"}}`),
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}

	// Same everything but content: accepted.
	sameShape := Event{Type: "tool/result", Seq: 1, Time: 1,
		Data:            json.RawMessage(`{"message":{"id":"t1","role":"tool","content":[{"type":"text","text":"new"}],"source":{"kind":"tool","callId":"c1"},"toolCallId":"c1"}}`),
		SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 0},
		SourceEventSeqs: []Seq{0}}
	surface, err := FoldSurface([]Event{original, sameShape})
	if err != nil {
		t.Fatalf("FoldSurface() error = %v, want a content-only rewrite accepted", err)
	}
	messages, err := surface.DeriveMessages([]Event{original, sameShape})
	if err != nil {
		t.Fatalf("DeriveMessages() error = %v", err)
	}
	if len(messages) != 1 || textOf(messages[0]) != "new" {
		t.Fatalf("history = %v, want the rewritten content", texts(messages))
	}

	// A different call id: refused.
	differentCall := Event{Type: "tool/result", Seq: 1, Time: 1,
		Data:            json.RawMessage(`{"message":{"id":"t1","role":"tool","content":[{"type":"text","text":"new"}],"source":{"kind":"tool","callId":"c2"},"toolCallId":"c2"}}`),
		SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 0},
		SourceEventSeqs: []Seq{0}}
	if _, err := FoldSurface([]Event{original, differentCall}); err == nil || !strings.Contains(err.Error(), "may change only content") {
		t.Fatalf("FoldSurface() = %v, want a call-id change refused", err)
	}

	// Spanning more than one node: refused.
	second := Event{Type: "tool/result", Seq: 1, Time: 1,
		Data:      json.RawMessage(`{"message":{"id":"t2","role":"tool","content":[{"type":"text","text":"two"}],"source":{"kind":"tool","callId":"c2"},"toolCallId":"c2"}}`),
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}
	spanning := Event{Type: "tool/result", Seq: 2, Time: 1,
		Data:            json.RawMessage(`{"message":{"id":"t1","role":"tool","content":[],"source":{"kind":"tool","callId":"c1"},"toolCallId":"c1"}}`),
		SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 1},
		SourceEventSeqs: []Seq{0, 1}}
	if _, err := FoldSurface([]Event{original, second, spanning}); err == nil || !strings.Contains(err.Error(), "exactly one current node") {
		t.Fatalf("FoldSurface() = %v, want a multi-node tool rewrite refused", err)
	}

	// A non-tool event as the target: refused.
	user := userEvent(0, "u1")
	overUser := Event{Type: "tool/result", Seq: 1, Time: 1,
		Data:            json.RawMessage(`{"message":{"id":"t1","role":"tool","content":[],"source":{"kind":"tool","callId":"c1"},"toolCallId":"c1"}}`),
		SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 0},
		SourceEventSeqs: []Seq{0}}
	if _, err := FoldSurface([]Event{user, overUser}); err == nil || !strings.Contains(err.Error(), "must target a current tool/result") {
		t.Fatalf("FoldSurface() = %v, want a non-tool target refused", err)
	}
}

// TestSurfaceMetadataRulesOnTheFold pins that the fold applies the same metadata
// rules as the append path, so the offline and live paths cannot diverge.
func TestSurfaceMetadataRulesOnTheFold(t *testing.T) {
	// A message-producing event without a marker.
	noMarker := Event{Type: "user/message", Seq: 0, Time: 1,
		Data: json.RawMessage(`{"id":"u1","role":"user","content":[{"type":"text","text":"u1"}],"source":{"kind":"user"}}`)}
	if _, err := FoldSurface([]Event{noMarker}); err == nil || !strings.Contains(err.Error(), "requires a surfaceOp") {
		t.Fatalf("FoldSurface() = %v, want the missing marker refused", err)
	}
	// A log-only event carrying a marker.
	withMarker := Event{Type: "turn/start", Seq: 0, Time: 1, Data: json.RawMessage(`{}`),
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}
	if _, err := FoldSurface([]Event{withMarker}); err == nil || !strings.Contains(err.Error(), "cannot carry surfaceOp") {
		t.Fatalf("FoldSurface() = %v, want the marker on a log-only event refused", err)
	}
	// An unknown ignorable record with opaque metadata is tolerated.
	unknown := Event{Type: "future/note", Seq: 0, Time: 1, Data: json.RawMessage(`{}`), HasIgnorable: true, Ignorable: true,
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}
	surface, err := FoldSurface([]Event{unknown})
	if err != nil {
		t.Fatalf("FoldSurface() = %v, want an unknown ignorable record tolerated", err)
	}
	if len(surface.Nodes) != 0 {
		t.Fatalf("surface nodes = %v, want none: an unknown record never changes history", surface.Nodes)
	}
}

// TestSurfaceNodesConvenience pins the one-step helpers.
func TestSurfaceNodesConvenience(t *testing.T) {
	events := []Event{logOnlyEvent(0, "turn/start"), userEvent(1, "u1")}
	nodes, err := SurfaceNodes(events)
	if err != nil {
		t.Fatalf("SurfaceNodes() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0] != 1 {
		t.Fatalf("SurfaceNodes() = %v, want [1]", nodes)
	}
	if _, err := SurfaceNodes([]Event{userEvent(3, "u1")}); !errors.Is(err, ErrSeqGap) {
		t.Fatalf("SurfaceNodes(gap) = %v, want ErrSeqGap", err)
	}
	// DeriveMessages surfaces the fold error too.
	if _, err := DeriveMessages([]Event{logOnlyEvent(2, "turn/start")}); !errors.Is(err, ErrSeqGap) {
		t.Fatalf("DeriveMessages(gap) = %v, want ErrSeqGap", err)
	}
}

// TestLiveLogComputesTheSameSurfaceAsAFold pins determinism across the two paths:
// the incrementally maintained surface must equal a from-scratch fold of the same
// events. If these ever diverge, a live reader and a replaying reader disagree.
func TestLiveLogComputesTheSameSurfaceAsAFold(t *testing.T) {
	log := NewLog(KnownTypes())
	inputs := []AppendInput{
		{Type: "turn/start", Data: []byte(`{"turn":0}`)},
		{Type: "user/message", Data: userEvent(0, "u1").Data, SurfaceOp: &SurfaceOp{Op: SurfaceAppend}},
		{Type: "assistant/attempt", Data: []byte(`{"turn":0,"step":0}`)},
		{Type: "user/message", Data: userEvent(0, "u2").Data, SurfaceOp: &SurfaceOp{Op: SurfaceAppend}},
		{Type: "user/message", Data: userEvent(0, "u3").Data, SurfaceOp: &SurfaceOp{Op: SurfaceAppend}},
		{Type: "user/message", Data: json.RawMessage(`{"id":"s","role":"user","content":[{"type":"text","text":"s"}],"source":{"kind":"user"}}`),
			SurfaceOp: &SurfaceOp{Op: SurfaceReplace, StartSeq: 1, EndSeq: 3}, SourceEventSeqs: []Seq{1, 3}},
	}
	for _, input := range inputs {
		if _, err := log.Append(input); err != nil {
			t.Fatalf("Append(%s) error = %v", input.Type, err)
		}
	}
	live := log.Surface()
	folded, err := FoldSurface(log.Events())
	if err != nil {
		t.Fatalf("FoldSurface() error = %v", err)
	}
	if len(live.Nodes) != len(folded.Nodes) {
		t.Fatalf("live nodes %v vs folded %v", live.Nodes, folded.Nodes)
	}
	for index := range live.Nodes {
		if live.Nodes[index] != folded.Nodes[index] {
			t.Fatalf("live nodes %v vs folded %v", live.Nodes, folded.Nodes)
		}
	}
	if live.ReplaceGeneration != folded.ReplaceGeneration || live.ContentGeneration != folded.ContentGeneration {
		t.Fatalf("generations live %d/%d vs folded %d/%d",
			live.ReplaceGeneration, live.ContentGeneration, folded.ReplaceGeneration, folded.ContentGeneration)
	}
	// The live derivation equals the offline one.
	liveMessages, err := log.DeriveMessages()
	if err != nil {
		t.Fatalf("DeriveMessages() error = %v", err)
	}
	offlineMessages, err := folded.DeriveMessages(log.Events())
	if err != nil {
		t.Fatalf("DeriveMessages(offline) error = %v", err)
	}
	if len(liveMessages) != len(offlineMessages) {
		t.Fatalf("live history %v vs offline %v", texts(liveMessages), texts(offlineMessages))
	}
	for index := range liveMessages {
		if liveMessages[index].ID != offlineMessages[index].ID {
			t.Fatalf("live history %v vs offline %v", texts(liveMessages), texts(offlineMessages))
		}
	}
}

// TestLiveLogRejectsAnInvalidSurfaceTransitionBeforeCommitting pins that the
// append path validates the surface before storing, so a bad replacement leaves the
// log and its surface untouched.
func TestLiveLogRejectsAnInvalidSurfaceTransitionBeforeCommitting(t *testing.T) {
	log := NewLog(KnownTypes())
	if _, err := log.Append(AppendInput{Type: "user/message", Data: userEvent(0, "u1").Data,
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	before := log.Surface()
	bad := AppendInput{Type: "user/message",
		Data:      json.RawMessage(`{"id":"s","role":"user","content":[{"type":"text","text":"s"}],"source":{"kind":"user"}}`),
		SurfaceOp: &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 7}}
	if _, err := log.Append(bad); err == nil {
		t.Fatal("Append() = nil, want the replacement refused")
	}
	if log.Len() != 1 {
		t.Fatalf("Len() = %d, want the rejected append not stored", log.Len())
	}
	after := log.Surface()
	if len(after.Nodes) != len(before.Nodes) || after.ReplaceGeneration != before.ReplaceGeneration {
		t.Fatalf("surface changed: %v -> %v", before.Nodes, after.Nodes)
	}
}

// TestGenerationsDistinguishGrowthFromRewrite pins the signal a derived cache
// depends on: a plain append does not bump either generation, while a replacement
// bumps both.
func TestGenerationsDistinguishGrowthFromRewrite(t *testing.T) {
	log := NewLog(KnownTypes())
	if _, err := log.Append(AppendInput{Type: "user/message", Data: userEvent(0, "u1").Data,
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if log.ReplaceGeneration() != 0 || log.ContentGeneration() != 0 {
		t.Fatalf("generations after an append = %d/%d, want 0/0", log.ReplaceGeneration(), log.ContentGeneration())
	}
	if _, err := log.Append(AppendInput{Type: "user/message", Data: userEvent(0, "u2").Data,
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend}}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if log.ContentGeneration() != 0 {
		t.Fatalf("a plain append bumped contentGeneration to %d, want 0", log.ContentGeneration())
	}
	if _, err := log.Append(AppendInput{Type: "user/message",
		Data:            json.RawMessage(`{"id":"s","role":"user","content":[{"type":"text","text":"s"}],"source":{"kind":"user"}}`),
		SurfaceOp:       &SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 1},
		SourceEventSeqs: []Seq{0, 1}}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if log.ReplaceGeneration() != 1 || log.ContentGeneration() != 1 {
		t.Fatalf("generations after a replacement = %d/%d, want 1/1", log.ReplaceGeneration(), log.ContentGeneration())
	}
	// A log-only append bumps neither.
	if _, err := log.Append(AppendInput{Type: "turn/end", Data: []byte(`{"turn":0}`)}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if log.ReplaceGeneration() != 1 || log.ContentGeneration() != 1 {
		t.Fatalf("a log-only append bumped the generations to %d/%d, want 1/1",
			log.ReplaceGeneration(), log.ContentGeneration())
	}
}
