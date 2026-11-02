# 流与适配：为什么流式分片不是最终答案

## 读完要解决的问题

你已经会用 Go 的通道读一串值，也知道 `context.Context` 能取消一次操作。现在的问题是：当模型通过 HTTP 一点一点往外吐内容时，你在 `for` 循环里拿到的那一串东西，到底应该被当成什么？

最直接的想法是把每个分片当成“答案的一部分”，边收边拼成一个字符串。在做一次性演示时它够用。它会崩在四个地方：模型可能先把 reasoning 吐出来再吐可见文本，两者的**位置**不同却被拼在一起；工具调用的参数是**分片**到达的，几个调用的分片还可能交错；一次被中途切断的流如果按“拼到哪算哪”处理，就会被当成一个成功的短回答；同一份请求要能分别交给 fake 与真实适配器，让两边产出同样的东西。

这一篇从这个问题进入 DSH 的 **StreamChunk**、**Assembler** 与 **Provider**。前置知识是 Go 的接口、通道、`context.Context` 与错误包装。关联行为见 [M4 执行契约](../10-plans/dsh-go-replication-m04/plans.md)；当前实现范围是流契约、组装与 tRPC 适配，真实端点与凭据属 M7。读完你能解释：为什么“分片”和“消息”是两个必须分开的概念，以及为什么适配器必须拒绝它无法表达的输入。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/llm/llm/src/types.ts`（`StreamChunk` 联合类型与 `FinishReason`）、`packages/llm/llm/src/assembler.ts`（`BlockAssembler`）与 `packages/llm/llm/src/assistant-stream.ts`（`AssistantStreamAccumulator`）。

## 从熟悉的 Go 写法出发

最直白的表示是让消费者自己拼字符串：

```go fragment
var text string
for {
	chunk, ok := <-stream
	if !ok {
		break
	}
	text += chunk.Text
}
```

它清楚，也能跑。边界在于**只有一种东西**被累加。真实流里至少要区分三种块：reasoning、可见文本、工具调用参数，而且每个块有自己的 index。参考实现用一个 `Map<number, PartialBlock>` 保存每个 index 的累积状态，键是**分片的 index**，不是到达顺序——这正是多个工具调用的分片交错时不会串味的机制。

第二个边界是：`+=` 让结束变得不可判定。循环退出只说明“通道关了”，不说明“模型说完了”。两者必须分开，否则一次断线的流会被当成完整回答。

## 最小实验

准备条件：仓库根目录，Go 1.22。这个实验不需要密钥、不需要网络。

先看组装：三段参数分片组成一个工具调用。

```go runnable=streaming-assemble
package main

import (
	"fmt"

	"github.com/gosomea/deepseek-harness-go/llm"
)

func main() {
	// The chunk sequence a provider emits for one tool call: the name arrives
	// first, then the argument string in fragments, then the terminal finish.
	name := "read_file"
	chunks := []llm.StreamChunk{
		{Type: llm.ChunkToolCallDelta, Index: 0, ID: "call-1", Name: &name, ArgumentsDelta: `{"pa`},
		{Type: llm.ChunkToolCallDelta, Index: 0, ArgumentsDelta: `th":"a.txt"}`},
		{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishToolCalls}},
	}
	assembler := llm.NewAssembler()
	for _, chunk := range chunks {
		if err := assembler.Push(chunk); err != nil {
			panic(err)
		}
	}
	for _, block := range assembler.Blocks() {
		if call, ok := block.(llm.ToolCallBlock); ok {
			fmt.Printf("id=%s name=%s arguments=%s\n", call.ID, call.Name, call.Arguments)
		}
	}
	reason, _ := assembler.FinishReason()
	fmt.Println("finish=" + string(reason.Kind))
}
```

```text output=streaming-assemble
id=call-1 name=read_file arguments={"path":"a.txt"}
finish=tool-calls
```

三段碎片拼回原始 JSON 字符串，字节未变。`finish=tool-calls` 说明这次尝试以“请求工具调用”结束——它是**终止事实**，不是文本的一部分。

再看完整往返：[examples/model](../../examples/model/README.md) 把同一份请求分别交给 fake 与 tRPC 适配器，并打印每一步：

```sh
go run ./examples/model
```

## 先预测，再观察

在执行下面的程序之前，先写下你的预期：把同一份 `llm.Request` 交给两个不同的 Provider，组装出的文本会一样吗？把 fake 的脚本换成“没有终止 chunk”的一段，`Assemble` 会返回什么？

```go runnable=streaming-incomplete
package main

import (
	"errors"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/llm"
)

// scriptedStream replays a fixed chunk sequence and then ends. It is the
// smallest possible provider: the point of the experiment is the consumer, so
// the source is written out rather than imported from a test helper.
type scriptedStream struct {
	chunks []llm.StreamChunk
	index  int
}

func (s *scriptedStream) Recv() (llm.StreamChunk, bool, error) {
	if s.index >= len(s.chunks) {
		return llm.StreamChunk{}, false, nil
	}
	chunk := s.chunks[s.index]
	s.index++
	return chunk, true, nil
}

func (s *scriptedStream) Close() error { return nil }

func main() {
	// A provider that stops mid-answer: chunks arrive, then the stream ends
	// without a terminal finish.
	stream := &scriptedStream{chunks: []llm.StreamChunk{
		{Type: llm.ChunkTextDelta, Index: 0, Text: "half an ans"},
	}}
	assembler := llm.NewAssembler()
	blocks, err := assembler.Assemble(stream)
	fmt.Println("blocks=" + llm.ContentText(blocks))
	fmt.Println("incomplete=" + fmt.Sprint(errors.Is(err, llm.ErrIncompleteStream)))

	// A tool call whose arguments were still streaming when the attempt ended.
	open := llm.NewAssembler()
	name := "danger"
	_ = open.Push(llm.StreamChunk{Type: llm.ChunkToolCallDelta, Index: 0, ID: "c1", Name: &name, ArgumentsDelta: `{"half`})
	_ = open.Push(llm.StreamChunk{Type: llm.ChunkFinish, Reason: &llm.FinishReason{Kind: llm.FinishToolCalls}})
	_, messageErr := open.Message("p", "m")
	fmt.Println("openToolCallRefused=" + fmt.Sprint(errors.Is(messageErr, llm.ErrToolCallNotClosed)))
}
```

```text output=streaming-incomplete
blocks=half an ans
incomplete=true
openToolCallRefused=true
```

观察重点：**部分输出仍然可见**（`blocks=half an ans`），但 `err` 非 nil，所以调用方可以显示半截回答却不把它当成成功结果。要恢复到正常场景，在 `chunks` 末尾补一个 `finish` chunk 即可（`llm.ChunkFinish`），此时 `incomplete=false`。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 适配器把 provider 的 wire 分片转成 `StreamChunk`；`BlockAssembler` 按 index 累积成 `ContentBlock`；`finish` 是终止事实 |
| TypeScript 入口 | `packages/llm/llm/src/types.ts`（`StreamChunk` 联合与 `FinishReason`）、`assembler.ts`（`BlockAssembler.push/blocks/message`） |
| Go 入口 | [llm/stream.go](../../llm/stream.go)、[llm/assembler.go](../../llm/assembler.go)、[llm/provider.go](../../llm/provider.go)、[plugins/llmtrpc/adapter.go](../../plugins/llmtrpc/adapter.go) |
| 保持的行为 | 分片按 index 累积；`block-end` 携带的块是权威值、之后的 delta 被忽略；终止 chunk 恰好一个；被拒绝的调用 `Attempts=0`；失败尝试不产生成功消息 |
| Go 表达 | `StreamChunk` 是有判别字段的结构体（不是每类一个接口），因为参考的 chunk map 可合并扩展，未知种类必须能存活；`Stream` 接口用 `Recv() (chunk, ok, error)` 而不是通道，让“结束”与“错误”成为两个显式信号 |
| 不等价边界 | ① 参考的 `assistant-stream` 把一次 attempt 压成可存储的紧凑记录（`text-chunks`/`tool-call-chunks` 等），M4 只实现组装，**未实现该紧凑表示**，属 M6；② 参考的 `FinishReasonMap` 允许适配器声明任意终止原因，Go 侧同样接受未知 kind，但**不为其提供专门语义**；③ 重试由本项目策略拥有，适配器显式关闭 SDK 内部重试 |

## 一次操作的时序与所有权

1. 调用方构造 `llm.Request`（消息来自 session surface），交给 `ProviderRegistry.Generate`。
2. 注册表在**启动尝试之前**校验请求并检查路由与能力：未知路由返回 `ErrProviderNotFound`，缺少声明能力返回 `UnsupportedCapabilityError`，此时不会产生任何 attempt。
3. Provider 返回 `Stream`。谁创建谁关闭：调用方（或 `Assembler.Assemble`）负责 `Close`。
4. `Assembler.Push` 按 index 累积；`block-end` 冻结该 index，之后的 delta 被忽略（防止行为异常的适配器改写已完成块）。
5. 流结束：有终止 chunk 则 `Assemble` 返回 nil；没有则返回 `ErrIncompleteStream`，**同时**返回已组装的块。
6. `Message` 只在“已终止、无未闭合工具调用、非失败 attempt”时才铸造消息。

tRPC 适配器的所有权：`Adapter` 持有底层框架模型（惰性创建），`Close` 后拒绝新 attempt；`stream.Close` **不取消调用方的 context**——取消是调用方的决定，不是读取的副作用。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| `llm: stream ended without a terminal finish` | provider 没发终止 chunk（断线或被截断） | `llm.ErrIncompleteStream`，测试 `TestAssembleReportsAnIncompleteAttempt` | 检查 provider 是否发送 `finish`；retry 由策略层决定 |
| `llm: tool call arguments were not completed` | 工具参数还在分片时 attempt 结束 | `llm.ErrToolCallNotClosed`，测试 `TestAssemblerRefusesAnOpenToolCall` | 不要执行该调用；等完整调用或向模型报告 |
| `llmtrpc: unsupported content block image` | 请求含适配器无法表达的块（图片/文件/未知块） | `llmtrpc.ErrUnsupported`，测试 `TestToRequestRefusesUnrepresentableBlocks` | 附件应在更早的层被投影成文本（M8）；不要吞掉该错误 |
| `stream ended without a terminal finish`（真实端点） | 框架的 `Done` 字段从不置位，终止信号是 `IsPartial=false` | [适配器差异记录](../../testdata/parity/llmtrpc/DIVERGENCES.md) | 不要用 `Done` 判断结束；按框架实际语义处理 |

## 回到源码与下一步

建议的阅读顺序：先读 `llm/stream.go` 的 `StreamChunk` 与 `Validate`，理解“什么是一个合法分片”；再读 `llm/assembler.go` 的 `Push`，注意 `block-end` 之后的 delta 为什么被忽略；最后读 `plugins/llmtrpc/translate.go` 的 `Translate`，看 wire 字段怎样映射到同一套分片。

对应测试：`llm/stream_test.go`、`llm/assembler_test.go`、`plugins/llmtrpc/loopback_test.go`、`plugins/llmtrpc/translate_test.go`、`internal/testkit/provider_contract_test.go`。

读者应能回答：机制解决什么问题（把不可靠的分片流变成可判定的事实）、谁拥有资源（调用方关闭流，适配器持有底层模型）、Go 与 DSH 的差异在哪里（紧凑 attempt 记录未实现；重试归策略层）。

下一篇：[tRPC 适配器](trpc-adapter.md)——同一份契约怎样落到具体框架，以及探测出的框架真实语义。
