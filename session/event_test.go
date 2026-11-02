package session

import (
	"errors"
	"strings"
	"testing"
)

// TestEventRoundTripIsLossless pins that a stored envelope reproduces its own
// bytes, which the log relies on to compare and hash history.
func TestEventRoundTripIsLossless(t *testing.T) {
	original := `{"type":"user/message","seq":3,"time":1700000000000,"data":{"id":"u1","role":"user"},"surfaceOp":"append"}`
	event, err := UnmarshalEvent([]byte(original))
	if err != nil {
		t.Fatalf("UnmarshalEvent() error = %v", err)
	}
	if event.Type != "user/message" || event.Seq != 3 || event.Time != 1700000000000 {
		t.Fatalf("decoded = %+v, want the envelope fields preserved", event)
	}
	if event.SurfaceOp == nil || !event.SurfaceOp.IsAppend() {
		t.Fatalf("SurfaceOp = %+v, want an append", event.SurfaceOp)
	}
	encoded, err := MarshalEvent(event)
	if err != nil {
		t.Fatalf("MarshalEvent() error = %v", err)
	}
	if string(encoded) != original {
		t.Fatalf("round trip changed bytes\nwant: %s\ngot:  %s", original, encoded)
	}
}

// TestReplaceSurfaceOpRoundTrip pins the replacement variant, whose object shape
// must carry exactly the three keys its validation requires.
func TestReplaceSurfaceOpRoundTrip(t *testing.T) {
	original := `{"type":"user/message","seq":5,"time":1,"data":{},"surfaceOp":{"op":"replace","startSeq":1,"endSeq":3},"sourceEventSeqs":[1,3]}`
	event, err := UnmarshalEvent([]byte(original))
	if err != nil {
		t.Fatalf("UnmarshalEvent() error = %v", err)
	}
	if event.SurfaceOp == nil || !event.SurfaceOp.IsReplace() {
		t.Fatalf("SurfaceOp = %+v, want a replacement", event.SurfaceOp)
	}
	if event.SurfaceOp.StartSeq != 1 || event.SurfaceOp.EndSeq != 3 {
		t.Fatalf("SurfaceOp = %+v, want the range preserved", event.SurfaceOp)
	}
	if len(event.SourceEventSeqs) != 2 {
		t.Fatalf("SourceEventSeqs = %v, want both citations", event.SourceEventSeqs)
	}
	encoded, err := MarshalEvent(event)
	if err != nil {
		t.Fatalf("MarshalEvent() error = %v", err)
	}
	if string(encoded) != original {
		t.Fatalf("round trip changed bytes\nwant: %s\ngot:  %s", original, encoded)
	}
}

// TestEnvelopeRejectsUnknownFields pins that an unmodeled envelope field is
// refused: it could change how the rest of the envelope is read, so ignoring it
// would be a guess.
func TestEnvelopeRejectsUnknownFields(t *testing.T) {
	_, err := UnmarshalEvent([]byte(`{"type":"turn/start","seq":0,"time":1,"data":{},"extra":1}`))
	if err == nil || !strings.Contains(err.Error(), "unknown envelope field") {
		t.Fatalf("UnmarshalEvent() = %v, want the unknown field refused", err)
	}
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("UnmarshalEvent() = %v, want ErrInvalidEvent", err)
	}
}

// TestEnvelopeRequiresMandatoryFields pins each missing required field.
func TestEnvelopeRequiresMandatoryFields(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"no type", `{"seq":0,"time":1,"data":{}}`, "requires a type"},
		{"empty type", `{"type":"","seq":0,"time":1,"data":{}}`, "type is empty"},
		{"no seq", `{"type":"turn/start","time":1,"data":{}}`, "requires a seq"},
		{"no time", `{"type":"turn/start","seq":0,"data":{}}`, "requires a time"},
		{"no data", `{"type":"turn/start","seq":0,"time":1}`, "requires data"},
		{"negative seq", `{"type":"turn/start","seq":-1,"time":1,"data":{}}`, "non-negative integer"},
		{"fractional time", `{"type":"turn/start","seq":0,"time":1.5,"data":{}}`, "whole number"},
		{"bad ignorable", `{"type":"turn/start","seq":0,"time":1,"data":{},"ignorable":"yes"}`, "must be a boolean"},
		{"false ignorable", `{"type":"turn/start","seq":0,"time":1,"data":{},"ignorable":false}`, "must be true when present"},
		{"bad surface op", `{"type":"turn/start","seq":0,"time":1,"data":{},"surfaceOp":7}`, "surfaceOp"},
		{"bad source list", `{"type":"turn/start","seq":0,"time":1,"data":{},"sourceEventSeqs":7}`, "must be an array"},
		{"empty envelope", ``, "empty event envelope"},
		{"not an object", `[]`, "must be an object"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := UnmarshalEvent([]byte(testCase.data))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("UnmarshalEvent(%s) = %v, want error containing %q", testCase.data, err, testCase.want)
			}
		})
	}
}

// TestReplaceSurfaceOpRejectsAliasedObjects pins that a replacement op with a
// missing or extra key is refused rather than interpreted leniently.
func TestReplaceSurfaceOpRejectsAliasedObjects(t *testing.T) {
	cases := []string{
		`{"type":"user/message","seq":5,"time":1,"data":{},"surfaceOp":{"op":"replace","startSeq":1}}`,
		`{"type":"user/message","seq":5,"time":1,"data":{},"surfaceOp":{"op":"replace","startSeq":1,"endSeq":3,"extra":1}}`,
		`{"type":"user/message","seq":5,"time":1,"data":{},"surfaceOp":{"op":"truncate","startSeq":1,"endSeq":3}}`,
		`{"type":"user/message","seq":5,"time":1,"data":{},"surfaceOp":{"op":"replace","startSeq":"a","endSeq":3}}`,
		`{"type":"user/message","seq":5,"time":1,"data":{},"surfaceOp":"truncate"}`,
	}
	for _, data := range cases {
		if _, err := UnmarshalEvent([]byte(data)); err == nil {
			t.Fatalf("UnmarshalEvent(%s) = nil, want the aliased op refused", data)
		}
	}
}

// TestSurfaceEligibilityRules pins which events may carry surface metadata and
// which must not. A message-producing event without its marker would be invisible
// to history; a log-only event with one would corrupt it.
func TestSurfaceEligibilityRules(t *testing.T) {
	surfaceEvent := func(surfaceOp *SurfaceOp) Event {
		return Event{Type: "user/message", Seq: 0, Time: 1, Data: []byte(`{}`), SurfaceOp: surfaceOp}
	}
	if err := ValidateEventEnvelope(surfaceEvent(nil)); err == nil || !strings.Contains(err.Error(), "requires a surfaceOp") {
		t.Fatalf("ValidateEventEnvelope() = %v, want a missing marker refused", err)
	}
	appendOp := SurfaceOp{Op: SurfaceAppend}
	if err := ValidateEventEnvelope(surfaceEvent(&appendOp)); err != nil {
		t.Fatalf("ValidateEventEnvelope() = %v, want an append accepted", err)
	}
	logOnly := Event{Type: "turn/start", Seq: 0, Time: 1, Data: []byte(`{}`), SurfaceOp: &appendOp}
	if err := ValidateEventEnvelope(logOnly); err == nil || !strings.Contains(err.Error(), "cannot carry surfaceOp") {
		t.Fatalf("ValidateEventEnvelope() = %v, want surface metadata on a log-only event refused", err)
	}
	logOnly.SurfaceOp = nil
	logOnly.SourceEventSeqs = []Seq{0}
	if err := ValidateEventEnvelope(logOnly); err == nil || !strings.Contains(err.Error(), "cannot carry sourceEventSeqs") {
		t.Fatalf("ValidateEventEnvelope() = %v, want citations on a log-only event refused", err)
	}
	// An unknown ignorable record keeps opaque metadata without affecting history.
	unknown := Event{Type: "future/note", Seq: 4, Time: 1, Data: []byte(`{}`), HasIgnorable: true, Ignorable: true, SurfaceOp: &appendOp}
	if err := ValidateEventEnvelope(unknown); err != nil {
		t.Fatalf("ValidateEventEnvelope() = %v, want an unknown ignorable record tolerated", err)
	}
}

// TestReplacementMustReferenceEarlierEvents pins that a replacement cannot cite
// the future, and that its range must precede its own position.
func TestReplacementMustReferenceEarlierEvents(t *testing.T) {
	op := SurfaceOp{Op: SurfaceReplace, StartSeq: 0, EndSeq: 5}
	event := Event{Type: "user/message", Seq: 5, Time: 1, Data: []byte(`{}`), SurfaceOp: &op}
	if err := ValidateEventEnvelope(event); err == nil || !strings.Contains(err.Error(), "earlier events") {
		t.Fatalf("ValidateEventEnvelope() = %v, want the range refused", err)
	}
}

// TestSourceCitationRules pins the source list rules: non-empty, no duplicates and
// strictly earlier, and that an assistant message may not cite sources at all
// because it embeds its stream.
func TestSourceCitationRules(t *testing.T) {
	appendOp := SurfaceOp{Op: SurfaceAppend}
	cases := []struct {
		name    string
		event   Event
		shadow  []Seq
		wantErr string
	}{
		{"empty list", Event{Type: "user/message", Seq: 2, Time: 1, Data: []byte(`{}`), SurfaceOp: &appendOp, SourceEventSeqs: []Seq{}}, nil, "must not be empty"},
		{"duplicate", Event{Type: "user/message", Seq: 3, Time: 1, Data: []byte(`{}`), SurfaceOp: &appendOp, SourceEventSeqs: []Seq{1, 1}}, nil, "must not contain duplicates"},
		{"future", Event{Type: "user/message", Seq: 3, Time: 1, Data: []byte(`{}`), SurfaceOp: &appendOp, SourceEventSeqs: []Seq{3}}, nil, "earlier events"},
		{"missing shadowed", Event{Type: "user/message", Seq: 5, Time: 1, Data: []byte(`{}`), SurfaceOp: &appendOp, SourceEventSeqs: []Seq{1}}, []Seq{1, 3}, "missing"},
		{"assistant cites", Event{Type: "assistant/message", Seq: 3, Time: 1, Data: []byte(`{}`), SurfaceOp: &appendOp, SourceEventSeqs: []Seq{1}}, nil, "cannot carry sourceEventSeqs"},
		{"complete", Event{Type: "user/message", Seq: 5, Time: 1, Data: []byte(`{}`), SurfaceOp: &appendOp, SourceEventSeqs: []Seq{1, 3}}, []Seq{1, 3}, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateSources(testCase.event, testCase.shadow)
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("validateSources() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("validateSources() = %v, want error containing %q", err, testCase.wantErr)
			}
		})
	}
}

// TestEventCloneIsDetached pins that a returned event shares no mutable state with
// the log, so a caller cannot rewrite a committed fact.
func TestEventCloneIsDetached(t *testing.T) {
	op := SurfaceOp{Op: SurfaceAppend}
	original := Event{Type: "user/message", Seq: 1, Time: 1, Data: []byte(`{"a":1}`),
		SurfaceOp: &op, SourceEventSeqs: []Seq{0}}
	cloned := original.Clone()
	cloned.Data[3] = '9'
	cloned.SourceEventSeqs[0] = 7
	cloned.SurfaceOp.Op = SurfaceReplace
	if string(original.Data) != `{"a":1}` {
		t.Fatalf("original data changed through the clone: %s", original.Data)
	}
	if original.SourceEventSeqs[0] != 0 {
		t.Fatal("original citations changed through the clone")
	}
	if original.SurfaceOp.Op != SurfaceAppend {
		t.Fatal("original surface op changed through the clone")
	}
	if !original.HasSurface() || original.IsSurfaceEligible() != true {
		t.Fatal("HasSurface/IsSurfaceEligible did not report correctly")
	}
}

// TestSurfaceOpValidate pins the two exact shapes and their rejections.
func TestSurfaceOpValidate(t *testing.T) {
	if err := (SurfaceOp{Op: SurfaceAppend}).Validate(); err != nil {
		t.Fatalf("append Validate() = %v, want nil", err)
	}
	if err := (SurfaceOp{Op: SurfaceAppend, StartSeq: 1}).Validate(); err == nil {
		t.Fatal("append with a range = nil, want an error")
	}
	if err := (SurfaceOp{Op: SurfaceReplace, StartSeq: 1, EndSeq: 2}).Validate(); err != nil {
		t.Fatalf("replace Validate() = %v, want nil", err)
	}
	if err := (SurfaceOp{}).Validate(); err == nil {
		t.Fatal("empty op = nil, want an error")
	}
	if err := (SurfaceOp{Op: "truncate"}).Validate(); err == nil {
		t.Fatal("unknown op = nil, want an error")
	}
	if (SurfaceOp{Op: SurfaceReplace}).IsAppend() {
		t.Fatal("IsAppend() = true for a replacement")
	}
	if !Ignorable(true).IsSet() || Ignorable(false).IsSet() {
		t.Fatal("Ignorable.IsSet did not report correctly")
	}
}
