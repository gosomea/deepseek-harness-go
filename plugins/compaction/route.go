package compaction

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gosomea/deepseek-harness-go/capability/compaction"
	"github.com/gosomea/deepseek-harness-go/llm"
)

// Errors this file's route returns.
var (
	// ErrRouteNotConfigured reports a route with no provider.
	ErrRouteNotConfigured = errors.New("compaction: summary route has no provider")
	// ErrRouteBudget reports a region whose estimated size exceeds the budget.
	ErrRouteBudget = errors.New("compaction: region exceeds the summary budget")
	// ErrRouteEmptyModel reports a route without a model id.
	ErrRouteEmptyModel = errors.New("compaction: summary route requires a model")
)

// RouteOptions configures a real summary route.
//
// A route is a Provider-backed SummaryProvider: it turns a region of history
// into one request and reads the answer back. It is separate from the flow in
// compact.go so that "how the summary is produced" can change (a real model, a
// loopback fake) without the three-phase flow changing.
type RouteOptions struct {
	// Provider is the route that produces the summary. Required.
	Provider llm.Provider
	// Model is the model id sent with the request. Required.
	Model string
	// MaxRegionTokens is the largest region, in estimated tokens, the route will
	// summarize. A larger region is refused before a request is sent, so the
	// failure is a routing decision rather than a provider error.
	MaxRegionTokens int
	// SystemPrompt is the instruction prepended to the region. Empty uses a
	// fixed one.
	SystemPrompt string
}

// Estimate is a deterministic token estimate together with its error bound.
//
// It exists because a budget decision made on an estimate must say it is an
// estimate: reporting an estimate as measured usage is the failure this type
// prevents. The reference distinguishes measured usage from an estimate; so does
// this route.
type Estimate struct {
	// Tokens is the estimated token count.
	Tokens int
	// Upper is the upper bound of the estimate, used for a conservative budget.
	Upper int
	// Method names how the estimate was produced, for a reader to judge it.
	Method string
}

// EstimateRegion estimates the tokens a region will cost.
//
// The estimate is deliberately simple and deterministic: four characters per
// token plus one token per entry of structure. It is an estimate, bounded above,
// not a measurement.
func EstimateRegion(entries []compaction.Entry) Estimate {
	chars := 0
	for _, entry := range entries {
		chars += len(entry.Text) + len(entry.ID) + len(string(entry.Kind))
	}
	tokens := chars/4 + len(entries)
	// A 25% headroom keeps the bound honest for the crude 4-chars/token rule.
	upper := tokens + tokens/4 + len(entries)
	return Estimate{Tokens: tokens, Upper: upper, Method: "chars/4 + one per entry; upper adds 25% and one per entry"}
}

// Route is a Provider-backed summary route.
type Route struct {
	provider   llm.Provider
	model      string
	maxRegion  int
	systemText string
}

// NewRoute returns a summary route from options.
func NewRoute(options RouteOptions) (*Route, error) {
	if options.Provider == nil {
		return nil, fmt.Errorf("%w", ErrRouteNotConfigured)
	}
	if strings.TrimSpace(options.Model) == "" {
		return nil, fmt.Errorf("%w", ErrRouteEmptyModel)
	}
	systemText := options.SystemPrompt
	if strings.TrimSpace(systemText) == "" {
		systemText = "Summarize the following conversation region, preserving tool call/result pairing and any facts a later step needs."
	}
	return &Route{
		provider:   options.Provider,
		model:      options.Model,
		maxRegion:  options.MaxRegionTokens,
		systemText: systemText,
	}, nil
}

// ProviderID reports the route's provider key, so a caller can tell which
// provider produced a summary.
//
// It exists because a route switch must not silently inherit the previous
// provider's state. Exposing the identity lets a caller re-validate capabilities
// before reusing anything.
func (r *Route) ProviderID() string {
	if r == nil || r.provider == nil {
		return ""
	}
	return r.provider.Info().ID
}

// Summarize produces a summary of a region through the provider.
//
// The order is: refuse an oversized region before sending, refuse an empty
// region, send one request, read the stream to completion, and only return text
// when the stream finished cleanly. A cancelled context ends the attempt without
// reporting a summary: a cancelled summary is not a short one.
func (r *Route) Summarize(region []compaction.Entry) (string, error) {
	return r.SummarizeContext(context.Background(), region)
}

// SummarizeContext is Summarize with an explicit context for cancellation.
func (r *Route) SummarizeContext(ctx context.Context, region []compaction.Entry) (string, error) {
	if r == nil || r.provider == nil {
		return "", fmt.Errorf("%w", ErrRouteNotConfigured)
	}
	if len(region) == 0 {
		return "", fmt.Errorf("%w: region is empty", ErrSummaryProviderFailed)
	}
	estimate := EstimateRegion(region)
	if r.maxRegion > 0 && estimate.Upper > r.maxRegion {
		return "", fmt.Errorf("%w: estimated %d tokens (upper %d) exceeds budget %d",
			ErrRouteBudget, estimate.Tokens, estimate.Upper, r.maxRegion)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrSummaryProviderFailed, err)
	}

	request := llm.Request{
		Provider: r.provider.Info().ID,
		Model:    r.model,
		Messages: []llm.Message{
			{
				ID:      llm.MessageID("summary-system"),
				Role:    llm.RoleSystem,
				Source:  llm.MessageSource{Kind: llm.SourceSystemPrompt},
				Content: []llm.ContentBlock{llm.TextBlock{Text: r.systemText}},
			},
			{
				ID:      llm.MessageID("summary-region"),
				Role:    llm.RoleUser,
				Source:  llm.MessageSource{Kind: llm.SourceUser},
				Content: []llm.ContentBlock{llm.TextBlock{Text: renderRegion(region)}},
			},
		},
	}
	if err := request.Validate(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrSummaryProviderFailed, err)
	}

	stream, err := r.provider.Generate(ctx, request)
	if err != nil {
		return "", fmt.Errorf("%w: provider refused the request: %v", ErrSummaryProviderFailed, err)
	}
	defer func() { _ = stream.Close() }()

	var builder strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("%w: cancelled while reading: %v", ErrSummaryProviderFailed, err)
		}
		chunk, ok, err := stream.Recv()
		if err != nil {
			return "", fmt.Errorf("%w: stream error: %v", ErrSummaryProviderFailed, err)
		}
		if !ok {
			break
		}
		if chunk.Type == llm.ChunkTextDelta {
			builder.WriteString(chunk.Text)
		}
	}
	summary := builder.String()
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("%w: the provider produced no text", ErrSummaryProviderFailed)
	}
	return summary, nil
}

// renderRegion turns a region into the body text of one request.
//
// Each entry is prefixed by its role and, for a call/result pair, its call id, so
// the summary can keep a pair related as it compresses.
func renderRegion(region []compaction.Entry) string {
	var builder strings.Builder
	for _, entry := range region {
		builder.WriteString("[")
		builder.WriteString(string(entry.Kind))
		if entry.CallID != "" {
			builder.WriteString(" call=")
			builder.WriteString(entry.CallID)
		}
		builder.WriteString("] ")
		builder.WriteString(entry.Text)
		builder.WriteString("\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}
