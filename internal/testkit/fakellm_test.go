package testkit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
)

func fakeRequest(t *testing.T) llm.Request {
	t.Helper()
	message, err := llm.NewUserMessage("hello")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	return llm.Request{Provider: "fake", Model: "fake-1", Messages: []llm.Message{message}}
}

func TestFakeProviderServesScriptsInOrder(t *testing.T) {
	provider := testkit.NewFakeProvider(
		testkit.ScriptTextAttempt([]string{"first"}),
		testkit.ScriptTextAttempt([]string{"second"}),
	)
	for attempt := 0; attempt < 2; attempt++ {
		stream, err := provider.Generate(context.Background(), fakeRequest(t))
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		chunks, err := testkit.Drain(stream)
		if err != nil {
			t.Fatalf("attempt %d drain: %v", attempt, err)
		}
		if err := testkit.RequireTerminalFinish(chunks); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if got := len(provider.Attempts()); got != 2 {
		t.Fatalf("want 2 recorded attempts, got %d", got)
	}
	if provider.Closed() {
		t.Fatal("provider must not report closed before Close")
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !provider.Closed() {
		t.Fatal("Close must be observable")
	}
}

// A scripted mid-stream failure must reach the consumer as a stream error after
// the chunks before it, which is how an interrupted attempt is reproduced.
func TestFakeStreamReportsScriptedErrorAfterChunks(t *testing.T) {
	cut := errors.New("connection reset")
	stream := testkit.NewFakeStreamWithError(
		[]llm.StreamChunk{{Type: llm.ChunkTextDelta, Index: 0, Text: "partial"}},
		cut,
	)
	chunks, err := testkit.Drain(stream)
	if !errors.Is(err, cut) {
		t.Fatalf("want the scripted error, got %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks before the failure must still be delivered, got %d", len(chunks))
	}
	if err := testkit.RequireTerminalFinish(chunks); err == nil {
		t.Fatal("a cut stream must be reported as lacking a terminal finish")
	}
	if !stream.Closed() {
		t.Fatal("Drain must close the stream")
	}
}

func TestFakeStreamIsIdempotentAndFinite(t *testing.T) {
	stream := testkit.NewFakeStream([]llm.StreamChunk{{Type: llm.ChunkTextDelta, Index: 0, Text: "x"}})
	if _, ok, err := stream.Recv(); !ok || err != nil {
		t.Fatalf("first recv: ok=%v err=%v", ok, err)
	}
	if _, ok, err := stream.Recv(); ok || err != nil {
		t.Fatalf("exhausted recv: want ok=false err=nil, got ok=%v err=%v", ok, err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
}

func TestFakeProviderScriptsSplitToolArguments(t *testing.T) {
	provider := testkit.NewFakeProvider(testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"pa`, `th":"a.txt"}`}))
	stream, err := provider.Generate(context.Background(), fakeRequest(t))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	chunks, err := testkit.Drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	var raw string
	for _, chunk := range chunks {
		raw += chunk.ArgumentsDelta
	}
	if raw != `{"path":"a.txt"}` {
		t.Fatalf("fragments must concatenate to the whole arguments, got %q", raw)
	}
	reason, ok := llm.Finish(chunks)
	if !ok || reason.Kind != llm.FinishToolCalls {
		t.Fatalf("want tool-calls finish, got %v (ok=%v)", reason, ok)
	}
}

func TestFakeProviderReportsStartupFailureAndOptions(t *testing.T) {
	boom := errors.New("boom")
	provider := testkit.NewFakeProviderWithOptions(
		testkit.FakeProviderID("named"),
		testkit.FakeProviderName("Named"),
		testkit.FakeProviderFailure(boom),
	)
	info := provider.Info()
	if info.ID != "named" || info.Name != "Named" {
		t.Fatalf("unexpected info %+v", info)
	}
	if !info.Supports(llm.CapabilityStreaming) {
		t.Fatal("the default option set must declare streaming so a route is reachable")
	}
	if _, err := provider.Generate(context.Background(), fakeRequest(t)); !errors.Is(err, boom) {
		t.Fatalf("want the startup failure, got %v", err)
	}
	empty := testkit.NewFakeProviderWithOptions(testkit.FakeProviderCapabilities())
	if len(empty.Info().Capabilities) != 0 {
		t.Fatal("FakeProviderCapabilities() with no argument must declare nothing")
	}
}
