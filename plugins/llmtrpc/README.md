---
description: "把自有模型契约接到 tRPC-Agent-Go 模型层，并保持它只出现在这一个包里。"
kind: "package-library"
---

# plugins/llmtrpc

## 概述

这个包是仓库里**唯一**允许导入 `trpc.group/trpc-go/trpc-agent-go` 的地方。它把 `llm` 的请求、消息与流契约翻译成框架类型，再把框架响应翻译回同一套 `StreamChunk`。这样做的目的是：更换模型库不会波及核心包与持久化格式，而框架的真实语义边界被记录在一处。

读者需要 Go 的接口与 `context.Context` 基础。当前实现范围是 M4.3 的转换与回环验证；真实端点、凭据与联网 smoke 属 M7。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `ToRequest` 把 `llm.Request` 转换成框架请求，按角色映射消息，保留工具调用的 id 与原始参数串。
- `Translate`/`FromResponse` 把框架响应转成 `llm.StreamChunk`：reasoning、文本与工具调用分片各落到固定块索引。
- `Adapter` 实现 `llm.Provider`，惰性创建底层模型，负责取消、关闭与能力声明。
- `CheckCapabilities` 在尝试开始前比对请求需求与路由声明，让能力缺口成为路由决策而不是中途失败。

## 使用

```go fragment
adapter := llmtrpc.New(llmtrpc.Config{
	ID: "openai-compatible", Name: "OpenAI Compatible", Model: "gpt-4o-mini",
	BaseURL: "http://127.0.0.1:8080/v1",
})
registry := llm.NewProviderRegistry()
err := registry.Register(adapter)   // adapter 满足 llm.Provider
stream, err := registry.Generate(ctx, request)
```

`BaseURL` 必填；测试与示例把它指向本机回环服务器，因此**无需密钥与外网**。`Adapter.Close` 之后 `Generate` 返回 `ErrClosed`。

## 理解实现

- **块索引固定**：reasoning=0、text=1、工具调用从 2 开始并按框架的 tool-call index 偏移。一个递增计数器会让 reasoning 与文本落到同一索引而被合并。
- **终止信号是 `IsPartial=false`，不是 `Done`**：框架对完整流的 `Done` 始终为 `false`。`stream` 因而自己排队分片、usage 与终止 chunk，保证 usage 在终止之前且终止只出现一次。
- **没有终止原因时补显式失败**：通道关闭而未见原因时发出 `missing-finish`，而不是伪造 `stop`。
- **错误恢复**：框架把连接失败与 provider 错误都塞进流内 `Response.Error`（`Type="stream_error"`，`Code` 为空）。适配器从消息里恢复 provider 的 `type`/`message` 与 HTTP 状态码，并把 dial 消息里的端口号排除在外（否则 `127.0.0.1:1` 会被读成状态码 1）。
- **重试关闭**：适配器不启用框架内部重试，一次 attempt 恰好一次 HTTP 请求。

## 进一步阅读

- [tRPC 适配器参考](../../docs/llm/trpc-adapter.md)：四条探测出的框架语义与差异表。
- [流与适配](../../docs/llm/streaming.md)：`StreamChunk` 与 assembler 的契约。
- [差异清单](../../testdata/parity/llmtrpc/DIVERGENCES.md)：已知差异及原因。
- [回环 fixture](../../testdata/parity/llmtrpc/README.md)：固定 SSE 正文与来源。

## 验证

```sh
go test -race -count=1 ./plugins/llmtrpc/...
```

覆盖：回环 SSE 端到端的 reasoning/文本/分片参数、usage 不相交转换、API 错误与连接失败的终止语义、非 2xx 的状态码恢复、关闭与取消、能力声明与缺失检查，以及无法表达块的拒绝。

## 限制

- **没有真实端点**。全部检查在 `httptest` 回环上进行；凭据与联网验证属 M7，缺密钥不算通过。
- **框架语义基于 v1.11.2 探测**。升级后需重新探测并更新差异清单。
- **未实现**：`assistant-stream` 的紧凑 attempt 记录（M6）、图片与附件的 offload（M8）、reasoning 签名（面向 AWS Bedrock）、`IMAGE_OFFLOAD_REQUIRED`。
- **provider 错误的 `code` 字段未使用**：provider 之间该字段类型不一致，状态码单独从状态行恢复。
- 本包不拥有重试与 attempt 记账；那属本项目策略层。
