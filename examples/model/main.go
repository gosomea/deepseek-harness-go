// Command model demonstrates one model attempt end to end without a credential:
// a scripted fake provider streams reasoning, text and tool-call fragments, the
// assembler turns them into one assistant message, and the same request is then
// sent through the tRPC adapter's loopback translation so the two providers can
// be compared.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/plugins/llmtrpc"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run performs one scripted attempt and prints what the assembler produced.
//
// It is deterministic: the fake provider replays a fixed script, the clock is
// never read, and the adapter translation is a pure function, so the output is
// byte-stable across runs.
func run(out io.Writer) error {
	message, err := llm.NewUserMessage("read a.txt")
	if err != nil {
		return err
	}
	request := llm.Request{Provider: "fake", Model: "fake-1", Messages: []llm.Message{message}}

	provider := testkit.NewFakeProvider(testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"pa`, `th":"a.txt"}`}))
	registry := llm.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		return err
	}
	stream, err := registry.Generate(context.Background(), request)
	if err != nil {
		return err
	}
	assembler := llm.NewAssembler()
	blocks, err := assembler.Assemble(stream)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "--- attempt ---")
	fmt.Fprintf(out, "provider=%s model=%s\n", request.Provider, request.Model)

	fmt.Fprintln(out, "--- stream chunks ---")
	replay := testkit.NewFakeStream(testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"pa`, `th":"a.txt"}`}))
	chunks, err := testkit.Drain(replay)
	if err != nil {
		return err
	}
	for index, chunk := range chunks {
		fmt.Fprintf(out, "chunk=%d type=%s index=%d text=%q arguments=%q\n",
			index, chunk.Type, chunk.Index, chunk.Text, chunk.ArgumentsDelta)
	}

	fmt.Fprintln(out, "--- assembled blocks ---")
	for index, block := range blocks {
		switch typed := block.(type) {
		case llm.TextBlock:
			fmt.Fprintf(out, "block=%d kind=text text=%q\n", index, typed.Text)
		case llm.ReasoningBlock:
			fmt.Fprintf(out, "block=%d kind=reasoning text=%q\n", index, typed.Text)
		case llm.ToolCallBlock:
			fmt.Fprintf(out, "block=%d kind=tool-call id=%s name=%s arguments=%q\n", index, typed.ID, typed.Name, typed.Arguments)
		}
	}

	reason, ok := assembler.FinishReason()
	fmt.Fprintf(out, "finish=%s ok=%v\n", reason.Kind, ok)
	usage, reported := assembler.Usage()
	fmt.Fprintf(out, "usage=input:%d output:%d reported=%v\n", usage.InputTokens, usage.OutputTokens, reported)

	assembled, err := assembler.Message("fake", "fake-1")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "message role=%s provider=%s blocks=%d\n", assembled.Role, assembled.Source.Provider, len(assembled.Content))

	fmt.Fprintln(out, "--- tRPC translation (no network) ---")
	converted, err := llmtrpc.ToRequest(request)
	if err != nil {
		return err
	}
	encoded, err := llmtrpc.MarshalRequest(converted)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "wire messages=%d stream=%v\n", len(converted.Messages), converted.GenerationConfig.Stream)
	fmt.Fprintf(out, "wire role=%s content=%q\n", converted.Messages[0].Role, converted.Messages[0].Content)
	fmt.Fprintf(out, "wire json contains messages=%v\n", strings.Contains(string(encoded), `"messages"`))

	fmt.Fprintln(out, "--- refusal ---")
	// An image block cannot reach a provider natively, so the adapter names it
	// instead of dropping it.
	imageMessage, err := llm.NewUserMessage("look")
	if err != nil {
		return err
	}
	imageMessage.Content = append(imageMessage.Content, llm.ImageBlock{
		Attachment: llm.ImageAttachmentRef{AttachmentID: "sha256:abc", MediaType: llm.MediaTypePNG, Bytes: 3, Width: 1, Height: 1},
	})
	_, refusal := llmtrpc.ToRequest(llm.Request{Provider: "p", Model: "m", Messages: []llm.Message{imageMessage}})
	fmt.Fprintf(out, "unsupported=%v\n", refusal != nil)
	fmt.Fprintf(out, "names the block=%v\n", refusal != nil && strings.Contains(refusal.Error(), "image"))
	return nil
}
