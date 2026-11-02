package llmtrpc

import (
	"fmt"
	"strings"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// CapabilityRequest describes what one request needs from a route.
//
// The adapter inspects this before translating, so a caller learns that a
// feature cannot be expressed here instead of discovering it as a provider
// error. The fields are derived from the request rather than declared by the
// caller, which is why this type is built by RequiredCapabilities.
type CapabilityRequest struct {
	// NeedsReasoning is set when any message carries reasoning content.
	NeedsReasoning bool
	// NeedsToolCalls is set when any message requests a tool call or answers one.
	NeedsToolCalls bool
	// NeedsStreaming is always set: every attempt in this harness streams.
	NeedsStreaming bool
}

// RequiredCapabilities derives the capabilities one request depends on.
//
// A tool message counts as needing tool calls even when it only carries a
// result: sending a result back to a route that cannot describe the original
// call would produce a request the provider rejects.
func RequiredCapabilities(request llm.Request) CapabilityRequest {
	required := CapabilityRequest{NeedsStreaming: true}
	for _, message := range request.Messages {
		if message.Role == llm.RoleTool {
			required.NeedsToolCalls = true
		}
		for _, block := range message.Content {
			switch block.BlockType() {
			case llm.BlockTypeReasoning:
				required.NeedsReasoning = true
			case llm.BlockTypeToolCall:
				required.NeedsToolCalls = true
			}
		}
	}
	return required
}

// Missing returns the capabilities a route must declare for one request, in a
// fixed order so a diagnostic is stable.
func (r CapabilityRequest) Missing(declared []llm.Capability) []llm.Capability {
	supported := make(map[llm.Capability]bool, len(declared))
	for _, capability := range declared {
		supported[capability] = true
	}
	var missing []llm.Capability
	for _, requirement := range []struct {
		needed     bool
		capability llm.Capability
	}{
		{r.NeedsStreaming, llm.CapabilityStreaming},
		{r.NeedsReasoning, llm.CapabilityReasoning},
		{r.NeedsToolCalls, llm.CapabilityToolCalls},
	} {
		if requirement.needed && !supported[requirement.capability] {
			missing = append(missing, requirement.capability)
		}
	}
	return missing
}

// String renders the missing capabilities for a diagnostic.
func (r CapabilityRequest) String() string {
	var names []string
	if r.NeedsStreaming {
		names = append(names, string(llm.CapabilityStreaming))
	}
	if r.NeedsReasoning {
		names = append(names, string(llm.CapabilityReasoning))
	}
	if r.NeedsToolCalls {
		names = append(names, string(llm.CapabilityToolCalls))
	}
	return strings.Join(names, ",")
}

// CheckCapabilities reports whether a route declares everything a request
// needs, naming the first missing capability when it does not.
//
// It is separate from translation because a capability gap is a routing
// decision the caller makes before an attempt, not a conversion failure.
func CheckCapabilities(provider string, request llm.Request, declared []llm.Capability) error {
	missing := RequiredCapabilities(request).Missing(declared)
	if len(missing) == 0 {
		return nil
	}
	return &llm.UnsupportedCapabilityError{Provider: provider, Capability: missing[0]}
}

// DescribeCapabilities renders a route's declaration for a diagnostic.
func DescribeCapabilities(info llm.ProviderInfo) string {
	if len(info.Capabilities) == 0 {
		return fmt.Sprintf("provider %s declares no capabilities", info.ID)
	}
	names := make([]string, 0, len(info.Capabilities))
	for _, capability := range info.Capabilities {
		names = append(names, string(capability))
	}
	return fmt.Sprintf("provider %s declares %s", info.ID, strings.Join(names, ","))
}
