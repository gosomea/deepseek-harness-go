package session

import (
	"strings"
	"testing"
)

// TestEnvelopeFieldOrderIsFixed pins that the encoded envelope leads with its
// discriminator and keeps the rest in a fixed order. The log compares and hashes
// these bytes, so a nondeterministic order would make replay unverifiable.
func TestEnvelopeFieldOrderIsFixed(t *testing.T) {
	op := SurfaceOp{Op: SurfaceAppend}
	event := Event{Type: "user/message", Seq: 2, Time: 7, Data: []byte(`{"a":1}`),
		SurfaceOp: &op, SourceEventSeqs: []Seq{0, 1}}
	first, err := MarshalEvent(event)
	if err != nil {
		t.Fatalf("MarshalEvent() error = %v", err)
	}
	for attempt := 0; attempt < 20; attempt++ {
		again, err := MarshalEvent(event)
		if err != nil {
			t.Fatalf("MarshalEvent() error = %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("encoding %d differed\nfirst: %s\nagain: %s", attempt, first, again)
		}
	}
	if !strings.HasPrefix(string(first), `{"type":"user/message","seq":2,"time":7,"data":{"a":1}`) {
		t.Fatalf("encoded = %s, want type, seq, time, data leading", first)
	}
	if !strings.Contains(string(first), `"surfaceOp":"append"`) {
		t.Fatalf("encoded = %s, want the append marker", first)
	}
	if !strings.Contains(string(first), `"sourceEventSeqs":[0,1]`) {
		t.Fatalf("encoded = %s, want the citations", first)
	}
	// An empty payload object is a real payload and is written verbatim.
	empty, err := MarshalEvent(Event{Type: "turn/start", Seq: 0, Time: 1, Data: []byte(`{}`)})
	if err != nil {
		t.Fatalf("MarshalEvent() error = %v", err)
	}
	if !strings.Contains(string(empty), `"data":{}`) {
		t.Fatalf("encoded = %s, want the empty payload object", empty)
	}
	// An omitted payload is refused: every event carries one.
	if _, err := MarshalEvent(Event{Type: "turn/start", Seq: 0, Time: 1}); err == nil {
		t.Fatal("MarshalEvent() = nil, want an omitted payload refused")
	}
}

// TestMarshalEventRejectsInvalidValues pins that an event which cannot be
// reconstructed is refused when written.
func TestMarshalEventRejectsInvalidValues(t *testing.T) {
	if _, err := MarshalEvent(Event{Type: "user/message", Seq: 0, Time: 1, Data: []byte(`{}`)}); err == nil {
		t.Fatal("MarshalEvent() = nil, want a surface event without a marker refused")
	}
	if _, err := MarshalEvent(Event{Time: 1, Data: []byte(`{}`)}); err == nil {
		t.Fatal("MarshalEvent() = nil, want an event without a type refused")
	}
	if _, err := MarshalEvent(Event{Type: "turn/start", Time: 1}); err == nil {
		t.Fatal("MarshalEvent() = nil, want an event without data refused")
	}
	if _, err := MarshalEvent(Event{Type: "turn/start", Time: 1, Data: []byte(`{`)}); err == nil {
		t.Fatal("MarshalEvent() = nil, want a malformed payload refused")
	}
	if _, err := MarshalEvent(Event{Type: "user/message", Seq: 2, Time: 1, Data: []byte(`{}`),
		SurfaceOp: &SurfaceOp{Op: SurfaceAppend, StartSeq: 1}}); err == nil {
		t.Fatal("MarshalEvent() = nil, want an append carrying a range refused")
	}
}

// TestIgnorableMarkerRoundTrip pins the marker's exact contract: present means
// true, and a written false is refused rather than read as "required".
func TestIgnorableMarkerRoundTrip(t *testing.T) {
	event := Event{Type: "future/note", Seq: 0, Time: 1, Data: []byte(`{}`), HasIgnorable: true, Ignorable: true}
	encoded, err := MarshalEvent(event)
	if err != nil {
		t.Fatalf("MarshalEvent() error = %v", err)
	}
	if !strings.Contains(string(encoded), `"ignorable":true`) {
		t.Fatalf("encoded = %s, want the marker written", encoded)
	}
	decoded, err := UnmarshalEvent(encoded)
	if err != nil {
		t.Fatalf("UnmarshalEvent() error = %v", err)
	}
	if !decoded.HasIgnorable || !decoded.Ignorable.IsSet() {
		t.Fatalf("decoded = %+v, want the marker preserved", decoded)
	}
	// A false marker is refused: the reference accepts only a literal true.
	if _, err := UnmarshalEvent([]byte(`{"type":"future/note","seq":0,"time":1,"data":{},"ignorable":false}`)); err == nil {
		t.Fatal("UnmarshalEvent(false marker) = nil, want an error")
	}
}

// TestEventCodecRejectsUnknownFields pins the aliasing refusal at the decode
// boundary, which is what makes the envelope a fixed shape.
func TestEventCodecRejectsUnknownFields(t *testing.T) {
	if _, err := UnmarshalEvent([]byte(`{"type":"turn/start","seq":0,"time":1,"data":{},"payload":{}}`)); err == nil {
		t.Fatal("UnmarshalEvent(payload alias) = nil, want the unknown field refused")
	}
	if _, err := UnmarshalEvent([]byte(`{"type":"turn/start","seq":0,"time":1,"data":{},"turn":1}`)); err == nil {
		t.Fatal("UnmarshalEvent(envelope-level turn) = nil, want the unknown field refused")
	}
	if _, err := UnmarshalEvent([]byte(`{"type":"turn/start","seq":0,"time":1,"data":{},"id":"x"}`)); err == nil {
		t.Fatal("UnmarshalEvent(envelope-level id) = nil, want the unknown field refused")
	}
}

// TestLogCodecRoundTripIsByteExact pins that a serialized log reproduces its own
// bytes, so two implementations can compare the same artifact.
func TestLogCodecRoundTripIsByteExact(t *testing.T) {
	log := NewLog(KnownTypes())
	if err := log.SetClock(fixedClock()); err != nil {
		t.Fatalf("SetClock() error = %v", err)
	}
	appendOp := SurfaceOp{Op: SurfaceAppend}
	inputs := []AppendInput{
		{Type: "turn/start", Data: []byte(`{"turn":0}`)},
		{Type: "user/message", Data: []byte(`{"id":"u1"}`), SurfaceOp: &appendOp},
		{Type: "assistant/attempt", Data: []byte(`{"turn":0,"step":0}`)},
		{Type: "future/note", Data: []byte(`{"note":true}`), Ignorable: true},
	}
	for _, input := range inputs {
		if _, err := log.Append(input); err != nil {
			t.Fatalf("Append(%s) error = %v", input.Type, err)
		}
	}
	encoded, err := MarshalLog(log.Events())
	if err != nil {
		t.Fatalf("MarshalLog() error = %v", err)
	}
	decoded, err := UnmarshalLog(encoded)
	if err != nil {
		t.Fatalf("UnmarshalLog() error = %v", err)
	}
	again, err := MarshalLog(decoded)
	if err != nil {
		t.Fatalf("MarshalLog(decoded) error = %v", err)
	}
	if string(again) != string(encoded) {
		t.Fatalf("log round trip changed bytes\nwant: %s\ngot:  %s", encoded, again)
	}
	if len(decoded) != 4 {
		t.Fatalf("decoded %d events, want 4", len(decoded))
	}
	// The unknown ignorable event kept its payload and its marker.
	if decoded[3].Type != "future/note" || !decoded[3].Ignorable.IsSet() || string(decoded[3].Data) != `{"note":true}` {
		t.Fatalf("decoded[3] = %+v, want the unknown event preserved", decoded[3])
	}
}

// TestMarshalLogBlamesTheFailingEvent pins that one bad event fails the whole log
// with a locating index, instead of being dropped.
func TestMarshalLogBlamesTheFailingEvent(t *testing.T) {
	good := Event{Type: "turn/start", Seq: 0, Time: 1, Data: []byte(`{}`)}
	bad := Event{Type: "user/message", Seq: 1, Time: 1, Data: []byte(`{}`)}
	_, err := MarshalLog([]Event{good, bad})
	if err == nil || !strings.Contains(err.Error(), "event 1") {
		t.Fatalf("MarshalLog() = %v, want the failing index blamed", err)
	}
	encoded, err := MarshalLog(nil)
	if err != nil || string(encoded) != "[]" {
		t.Fatalf("MarshalLog(nil) = %s, %v, want an empty array", encoded, err)
	}
}
