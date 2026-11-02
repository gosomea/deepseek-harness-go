package llm

import (
	"strings"
	"testing"
)

// TestMessageValidateRequiresRoleConsistency pins the cross-field rules later
// subsystems rely on: a role and its source kind must agree, and tool fields
// belong only to a tool message.
func TestMessageValidateRequiresRoleConsistency(t *testing.T) {
	base := func(role Role, source MessageSource) Message {
		return Message{ID: "m1", Role: role, Source: source}
	}
	cases := []struct {
		name    string
		message Message
		want    string
	}{
		{"system needs system-prompt source", base(RoleSystem, MessageSource{Kind: SourceUser}), "system-prompt source"},
		{"system ok", base(RoleSystem, MessageSource{Kind: SourceSystemPrompt}), ""},
		{"assistant needs model source", base(RoleAssistant, MessageSource{Kind: SourceUser}), "model source"},
		{"model source needs provider", base(RoleAssistant, MessageSource{Kind: SourceModel, Model: "m"}), "provider"},
		{"model source needs model", base(RoleAssistant, MessageSource{Kind: SourceModel, Provider: "p"}), "model"},
		{"user needs a kind", base(RoleUser, MessageSource{}), "requires a source kind"},
		{"developer needs a kind", base(RoleDeveloper, MessageSource{}), "requires a source kind"},
		{"tool needs call id", base(RoleTool, MessageSource{Kind: SourceTool, CallID: "c1"}), "requires a toolCallId"},
		{"tool source needs call id", base(RoleTool, MessageSource{Kind: SourceTool}), "requires a toolCallId"},
		{"unknown role", base(Role("robot"), MessageSource{Kind: SourceUser}), "unknown message role"},
		{"missing role", base("", MessageSource{Kind: SourceUser}), "requires a role"},
		{"missing id", Message{Role: RoleUser, Source: MessageSource{Kind: SourceUser}}, "requires an id"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.message.Validate()
			if testCase.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("Validate() = %v, want error containing %q", err, testCase.want)
			}
		})
	}
}

// TestToolMessageRequiresMatchingCallID pins that the call identifier recorded
// in both places must agree: a disagreement would make the result unanswerable.
func TestToolMessageRequiresMatchingCallID(t *testing.T) {
	message, err := NewToolResultMessage("c1", TextContent("ok"), false)
	if err != nil {
		t.Fatalf("NewToolResultMessage() error = %v", err)
	}
	if message.ToolCallID != "c1" || message.Source.CallID != "c1" {
		t.Fatalf("call id recorded as %q/%q, want c1/c1", message.ToolCallID, message.Source.CallID)
	}
	message.ToolCallID = "c2"
	err = message.Validate()
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Validate() = %v, want a mismatch error", err)
	}
}

// TestToolResultErrorFlagRoundTrips pins that a failed invocation stays marked,
// so a consumer can present it without inspecting content that may be absent.
func TestToolResultErrorFlagRoundTrips(t *testing.T) {
	message, err := NewToolResultMessage("c1", TextContent("boom"), true)
	if err != nil {
		t.Fatalf("NewToolResultMessage() error = %v", err)
	}
	raw, err := MarshalMessage(message)
	if err != nil {
		t.Fatalf("MarshalMessage() error = %v", err)
	}
	if !strings.Contains(string(raw), `"isError":true`) {
		t.Fatalf("encoded message = %s, want isError true", raw)
	}
	decoded, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatalf("UnmarshalMessage() error = %v", err)
	}
	if !decoded.IsError || decoded.ToolCallID != "c1" {
		t.Fatalf("decoded = %+v, want isError with call c1", decoded)
	}
}

// TestMessageRoundTripIsLossless pins that a stored message reproduces its own
// bytes, which the session log relies on.
func TestMessageRoundTripIsLossless(t *testing.T) {
	message, err := NewAssistantMessage("deepseek", "v4", []ContentBlock{
		ReasoningBlock{Text: "think"},
		TextBlock{Text: "answer"},
		ToolCallBlock{ID: "c1", Name: "read", Arguments: `{"path":"a"}`},
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage() error = %v", err)
	}
	raw, err := MarshalMessage(message)
	if err != nil {
		t.Fatalf("MarshalMessage() error = %v", err)
	}
	decoded, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatalf("UnmarshalMessage() error = %v", err)
	}
	again, err := MarshalMessage(decoded)
	if err != nil {
		t.Fatalf("MarshalMessage(decoded) error = %v", err)
	}
	if string(again) != string(raw) {
		t.Fatalf("message round trip changed bytes\nwant: %s\ngot:  %s", raw, again)
	}
	if decoded.Source.Provider != "deepseek" || decoded.Source.Model != "v4" {
		t.Fatalf("decoded source = %+v, want the route preserved", decoded.Source)
	}
}

// TestForeignSourceKindIsPreservedFromScratch pins that an unknown source kind is
// not rejected except for the user role, whose kind is required to be known.
func TestForeignSourceKindIsPreservedFromScratch(t *testing.T) {
	message, err := NewMessage(RoleUser, TextContent("hi"), MessageSource{
		Kind:  "plugin:recall",
		Extra: map[string]any{"origin": "other-session", "count": float64(2)},
	})
	if err != nil {
		t.Fatalf("NewMessage() error = %v", err)
	}
	raw, err := MarshalMessage(message)
	if err != nil {
		t.Fatalf("MarshalMessage() error = %v", err)
	}
	decoded, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatalf("UnmarshalMessage() error = %v", err)
	}
	if decoded.Source.Kind != "plugin:recall" {
		t.Fatalf("Kind = %q, want the foreign kind preserved", decoded.Source.Kind)
	}
	if decoded.Source.Extra["origin"] != "other-session" {
		t.Fatalf("Extra = %v, want the foreign fields preserved", decoded.Source.Extra)
	}
	again, err := MarshalMessage(decoded)
	if err != nil {
		t.Fatalf("MarshalMessage(decoded) error = %v", err)
	}
	if string(again) != string(raw) {
		t.Fatalf("foreign source round trip changed bytes\nwant: %s\ngot:  %s", raw, again)
	}
}

// TestSystemMessageSourceIsEnforced pins that a system message cannot claim a
// producer other than the system-prompt plugin: the roles are not interchangeable.
func TestSystemMessageSourceIsEnforced(t *testing.T) {
	if _, err := NewMessage(RoleSystem, TextContent("p"), MessageSource{Kind: SourceUser}); err == nil {
		t.Fatal("NewMessage() = nil, want a rejected system message source")
	}
	message, err := NewSystemMessage("")
	if err != nil {
		t.Fatalf("NewSystemMessage(\"\") error = %v", err)
	}
	if !message.Empty() {
		t.Fatalf("NewSystemMessage(\"\") content = %v, want empty", message.Content)
	}
}

// TestUnmarshalMessageRejectsIncompleteValues pins the read-side failures: a
// message missing its source or carrying tool fields on a non-tool role is
// refused, never silently repaired.
func TestUnmarshalMessageRejectsIncompleteValues(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"no source", `{"id":"m1","role":"user","content":[]}`, "requires a source"},
		{"bad role", `{"id":"m1","role":"robot","content":[],"source":{"kind":"user"}}`, "unknown message role"},
		{"call id mismatch", `{"id":"m1","role":"tool","content":[],"source":{"kind":"tool","callId":"c2"},"toolCallId":"c1"}`, "does not match"},
		{"bad content", `{"id":"m1","role":"user","content":7,"source":{"kind":"user"}}`, "content must be an array"},
		{"source not an object", `{"id":"m1","role":"user","content":[],"source":7}`, "source must be an object"},
		{"bad kind type", `{"id":"m1","role":"user","content":[],"source":{"kind":7}}`, "source kind"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := UnmarshalMessage([]byte(testCase.data))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("UnmarshalMessage() = %v, want error containing %q", err, testCase.want)
			}
		})
	}
}

// TestMessagesRoundTrip pins the slice codec, including an empty conversation.
func TestMessagesRoundTrip(t *testing.T) {
	user, err := NewUserMessage("hi")
	if err != nil {
		t.Fatalf("NewUserMessage() error = %v", err)
	}
	assistant, err := NewAssistantMessage("p", "m", TextContent("yo"))
	if err != nil {
		t.Fatalf("NewAssistantMessage() error = %v", err)
	}
	raw, err := MarshalMessages([]Message{user, assistant})
	if err != nil {
		t.Fatalf("MarshalMessages() error = %v", err)
	}
	decoded, err := UnmarshalMessages(raw)
	if err != nil {
		t.Fatalf("UnmarshalMessages() error = %v", err)
	}
	if len(decoded) != 2 || decoded[0].Role != RoleUser || decoded[1].Role != RoleAssistant {
		t.Fatalf("decoded = %+v, want the two messages in order", decoded)
	}
	empty, err := UnmarshalMessages([]byte("[]"))
	if err != nil {
		t.Fatalf("UnmarshalMessages([]) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("UnmarshalMessages([]) = %d messages, want 0", len(empty))
	}
	if _, err := MarshalMessages([]Message{{Role: RoleUser, Source: MessageSource{Kind: SourceUser}, Content: []ContentBlock{TextBlock{}}}}); err == nil {
		t.Fatal("MarshalMessages() = nil, want the message without an id rejected")
	}
	// A structurally incomplete but representable block is written, not refused.
	if _, err := MarshalMessages([]Message{{ID: "m", Role: RoleUser, Source: MessageSource{Kind: SourceUser}, Content: []ContentBlock{ToolCallBlock{}}}}); err != nil {
		t.Fatalf("MarshalMessages() = %v, want an incomplete block written", err)
	}
}

// TestNewMessageIDIsUniqueAndShaped pins that minted identifiers are distinct and
// look like a UUID, and that an arbitrary caller-supplied id is still accepted.
func TestNewMessageIDIsUniqueAndShaped(t *testing.T) {
	first, err := NewMessageID()
	if err != nil {
		t.Fatalf("NewMessageID() error = %v", err)
	}
	second, err := NewMessageID()
	if err != nil {
		t.Fatalf("NewMessageID() error = %v", err)
	}
	if first == second {
		t.Fatalf("NewMessageID() produced %q twice", first)
	}
	if len(first) != 36 || strings.Count(first.String(), "-") != 4 {
		t.Fatalf("NewMessageID() = %q, want a UUID shape", first)
	}
	if first.IsZero() || MessageID("").IsZero() != true {
		t.Fatal("IsZero did not distinguish an unset id")
	}
	legacy := Message{ID: "legacy-id-1", Role: RoleUser, Source: MessageSource{Kind: SourceUser}}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a non-UUID id accepted", err)
	}
	if MustMessageID() == "" {
		t.Fatal("MustMessageID() returned an empty id")
	}
}

// TestValidateContentRejectsNilBlock pins that a nil block cannot hide in a
// content slice.
func TestValidateContentRejectsNilBlock(t *testing.T) {
	err := ValidateContent([]ContentBlock{TextBlock{}, nil})
	if err == nil || !strings.Contains(err.Error(), "is nil") {
		t.Fatalf("ValidateContent() = %v, want a nil-block error", err)
	}
}
