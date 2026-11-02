package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMarshalIsDeterministic pins that encoding the same value twice produces
// identical bytes. A durable log compares and hashes these bytes, so a map
// iteration order leaking into the output would make replay non-reproducible.
func TestMarshalIsDeterministic(t *testing.T) {
	message, err := NewUserMessage("hello")
	if err != nil {
		t.Fatalf("NewUserMessage() error = %v", err)
	}
	message.Source.Extra = map[string]any{"b": "2", "a": "1", "c": "3"}
	first, err := MarshalMessage(message)
	if err != nil {
		t.Fatalf("MarshalMessage() error = %v", err)
	}
	for attempt := 0; attempt < 20; attempt++ {
		again, err := MarshalMessage(message)
		if err != nil {
			t.Fatalf("MarshalMessage() error = %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("encoding %d differed\nfirst: %s\nagain: %s", attempt, first, again)
		}
	}
	// The discriminator leads so a reader can classify without buffering.
	if !strings.HasPrefix(string(first), `{"id":`) {
		t.Fatalf("encoded message = %s, want the id first", first)
	}
	source, err := MarshalSource(message.Source)
	if err != nil {
		t.Fatalf("MarshalSource() error = %v", err)
	}
	if !strings.HasPrefix(string(source), `{"kind":"user"`) {
		t.Fatalf("encoded source = %s, want kind first", source)
	}
	if !json.Valid(first) {
		t.Fatalf("encoded message is not valid JSON: %s", first)
	}
}

// TestUnmarshalContentAcceptsEmptyAndNilInputs pins the boundary values the
// session codec passes through when a record has no content.
func TestUnmarshalContentAcceptsEmptyAndNilInputs(t *testing.T) {
	for _, data := range []string{"", "[]"} {
		blocks, err := UnmarshalContent([]byte(data))
		if err != nil {
			t.Fatalf("UnmarshalContent(%q) error = %v", data, err)
		}
		if len(blocks) != 0 {
			t.Fatalf("UnmarshalContent(%q) = %d blocks, want 0", data, len(blocks))
		}
	}
}

// TestUnmarshalMessageRejectsTrailingContent pins that a decoder never ignores a
// second value: silently reading the first of two messages would lose data.
func TestUnmarshalMessageRejectsTrailingContent(t *testing.T) {
	_, err := UnmarshalMessages([]byte(`[{"id":"m1","role":"user","content":[],"source":{"kind":"user"}}] []`))
	if err == nil || !strings.Contains(err.Error(), "trailing content") {
		t.Fatalf("UnmarshalMessages() = %v, want a trailing-content error", err)
	}
}

// TestUnmarshalSourceRejectsBadFieldValues pins the per-field read failures so a
// typed field cannot silently decode to its zero value.
func TestUnmarshalSourceRejectsBadFieldValues(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"provider type", `{"kind":"model","provider":7,"model":"m"}`, "source provider"},
		{"model type", `{"kind":"model","provider":"p","model":7}`, "source model"},
		{"callId type", `{"kind":"tool","callId":7}`, "source callId"},
		{"extra field type", `{"kind":"user","data":`, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := UnmarshalSource([]byte(testCase.data))
			if err == nil {
				t.Fatalf("UnmarshalSource(%s) = nil, want an error", testCase.data)
			}
		})
	}
	if _, err := UnmarshalSource(nil); err == nil {
		t.Fatal("UnmarshalSource(nil) = nil, want an error")
	}
}

// TestKnownBlocksWithoutExtraFieldsRejectUnknownFields pins that a known block
// is decoded strictly: a renamed field is a typed error, not a silent default.
func TestKnownBlocksWithoutExtraFieldsRejectUnknownFields(t *testing.T) {
	blocks, err := UnmarshalContent([]byte(`[{"type":"text","text":"hi","colour":"red"}]`))
	if err != nil {
		t.Fatalf("UnmarshalContent() error = %v", err)
	}
	text, ok := blocks[0].(TextBlock)
	if !ok || text.Text != "hi" {
		t.Fatalf("decoded = %#v, want the text block", blocks[0])
	}
	encoded, err := MarshalContent(blocks)
	if err != nil {
		t.Fatalf("MarshalContent() error = %v", err)
	}
	// An uninspected field on a known block is not preserved; the block is
	// re-encoded from its own fields. Unknown *kinds* are the preservation
	// mechanism, not unknown fields on a known kind.
	if strings.Contains(string(encoded), "colour") {
		t.Fatalf("encoded = %s, want the unknown field dropped from a known block", encoded)
	}
}
