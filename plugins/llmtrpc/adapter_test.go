package llmtrpc_test

import (
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/plugins/llmtrpc"
)

func messageWith(t *testing.T, role llm.Role, source llm.MessageSource, blocks ...llm.ContentBlock) llm.Message {
	t.Helper()
	message, err := llm.NewMessage(role, blocks, source)
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	return message
}

func TestRequiredCapabilitiesDerivesFromContent(t *testing.T) {
	plain := messageWith(t, llm.RoleUser, llm.MessageSource{Kind: llm.SourceUser}, llm.TextBlock{Text: "hi"})
	required := llmtrpc.RequiredCapabilities(llm.Request{Messages: []llm.Message{plain}})
	if !required.NeedsStreaming || required.NeedsReasoning || required.NeedsToolCalls {
		t.Fatalf("a plain request needs streaming only, got %+v", required)
	}
	if got := required.String(); got != "streaming" {
		t.Fatalf("unexpected rendering %q", got)
	}
}

// A request is validated against the route's own declaration before any attempt
// starts, so a capability gap is a routing decision rather than a provider
// error discovered mid-flight.
func TestCheckCapabilitiesNamesTheMissingFeature(t *testing.T) {
	source := llm.MessageSource{Kind: llm.SourceModel, Provider: "p", Model: "m"}
	assistant := messageWith(t, llm.RoleAssistant, source,
		llm.ReasoningBlock{Text: "why"},
		llm.ToolCallBlock{ID: "c1", Name: "read_file", Arguments: "{}"},
	)
	request := llm.Request{Messages: []llm.Message{assistant}}
	required := llmtrpc.RequiredCapabilities(request)
	if !required.NeedsReasoning || !required.NeedsToolCalls {
		t.Fatalf("reasoning and tool calls must be detected, got %+v", required)
	}
	// A route that streams but cannot do reasoning is refused by name.
	err := llmtrpc.CheckCapabilities("limited", request, []llm.Capability{llm.CapabilityStreaming, llm.CapabilityToolCalls})
	if err == nil {
		t.Fatal("a missing reasoning capability must be refused")
	}
	if !strings.Contains(err.Error(), string(llm.CapabilityReasoning)) {
		t.Fatalf("the refusal must name reasoning, got %v", err)
	}
	full := []llm.Capability{llm.CapabilityStreaming, llm.CapabilityReasoning, llm.CapabilityToolCalls}
	if err := llmtrpc.CheckCapabilities("full", request, full); err != nil {
		t.Fatalf("a fully declared route must be accepted, got %v", err)
	}
}

// A tool result needs tool-call support even when it carries only text: sending
// a result to a route that cannot describe the original call produces a request
// the provider rejects.
func TestToolResultRequiresToolCallSupport(t *testing.T) {
	source := llm.MessageSource{Kind: llm.SourceTool, CallID: "c1"}
	message := messageWith(t, llm.RoleTool, source, llm.TextBlock{Text: "contents"})
	required := llmtrpc.RequiredCapabilities(llm.Request{Messages: []llm.Message{message}})
	if !required.NeedsToolCalls {
		t.Fatal("a tool result must require tool-call support")
	}
}

func TestMissingCapabilitiesKeepAFixedOrder(t *testing.T) {
	required := llmtrpc.CapabilityRequest{NeedsStreaming: true, NeedsReasoning: true, NeedsToolCalls: true}
	missing := required.Missing(nil)
	want := []llm.Capability{llm.CapabilityStreaming, llm.CapabilityReasoning, llm.CapabilityToolCalls}
	if len(missing) != len(want) {
		t.Fatalf("want %d missing, got %v", len(want), missing)
	}
	for index := range want {
		if missing[index] != want[index] {
			t.Fatalf("order differs at %d: want %s, got %s", index, want[index], missing[index])
		}
	}
	if got := required.String(); got != "streaming,reasoning,tool-calls" {
		t.Fatalf("unexpected rendering %q", got)
	}
}

func TestDescribeCapabilitiesHandlesTheEmptyDeclaration(t *testing.T) {
	if got := llmtrpc.DescribeCapabilities(llm.ProviderInfo{ID: "bare"}); !strings.Contains(got, "declares no capabilities") {
		t.Fatalf("unexpected description %q", got)
	}
	got := llmtrpc.DescribeCapabilities(llm.ProviderInfo{ID: "full", Capabilities: []llm.Capability{llm.CapabilityStreaming, llm.CapabilityUsage}})
	if !strings.Contains(got, "streaming,usage") {
		t.Fatalf("unexpected description %q", got)
	}
}
