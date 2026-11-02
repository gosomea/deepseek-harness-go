# tRPC 适配器：同一份契约落在具体框架上

## 读完要解决的问题

你已经定义了自己的 `llm.Request` 与 `StreamChunk`，现在要接一个现成的模型库。问题是：**框架里的类型该出现在哪些包里？**

最省事的做法是让核心包直接用框架的 `Request`/`Response`。它会崩在两处：持久化格式会被第三方类型绑死，换库时改动横跨整个仓库；而框架的能力边界（哪些字段它会丢、哪些语义与你的契约不同）会悄悄变成你的语义。

这一篇说明 `plugins/llmtrpc` 如何承担**唯一**的框架导入边界，以及探测框架真实行为得到的四条差异。前置知识是 Go 的包边界与接口。关联行为见 [M4 执行契约](../10-plans/dsh-go-replication-m04/plans.md)。

固定依赖：`trpc.group/trpc-go/trpc-agent-go v1.11.2`（其 `go.mod` 要求 `go 1.21`，本项目 `go 1.22` 满足）。固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/llm/llm-deepseek/src/adapter.ts` 与 `packages/llm/llm/src/types.ts`。

## 从熟悉的 Go 写法出发

普通 Go 做法是把第三方类型包在适配器里，对外只暴露自己的类型：

```go fragment
// 调用方看到的是自己的契约。
func (a *Adapter) Generate(ctx context.Context, request dsh.Request) (dsh.Stream, error)
```

边界在于**谁来保证没有泄漏**。约定会被无意破坏：一次“就临时用一下框架的错误类型”就让核心包永久依赖了第三方。所以这里不靠约定，而是靠一条可执行的检查：仓库里除 `plugins/llmtrpc/` 外，任何 `.go` 文件都不得出现 `trpc.group/trpc-go/trpc-agent-go`。

## 最小实验

准备条件：仓库根目录，Go 1.22，依赖已下载（`go mod download`）。**不需要密钥、不需要网络**——回环服务器在本机。

```sh
go test -count=1 -run TestAdapter ./plugins/llmtrpc/...
```

它会启动一个 `httptest` 服务器，按固定 SSE 正文回答一次 `/chat/completions`，然后断言适配器把 reasoning、文本与分片参数转成了同一套 `StreamChunk`，并把 usage 转成**不相交**的计数。

```sh
go run ./examples/model
```

`examples/model` 打印 fake 与适配器两条路径的对比；它不发真实请求。

## 先预测，再观察

先写下预期：框架的一个流式响应里，`Response.Done` 什么时候变成 `true`？工具调用的参数分片默认会出现在 delta 里吗？

观察方式（真实命令，结果见下）：

```sh
go test -count=1 -run TestTranslateReportsFinalAndUsageSeparately ./plugins/llmtrpc/...
```

实测结论（用探针直接打印框架响应得到，见[差异清单](../../testdata/parity/llmtrpc/DIVERGENCES.md)）：

1. **`Done` 从不置位**。完整流的最后一个响应是 `IsPartial=false`，而 `Done` 始终为 `false`。若按 `Done` 判断结束，**每一次**尝试都会被当成被截断。
2. **工具参数分片默认不发出**。需要 `openai.WithShowToolCallDelta(true)`；不开启时 delta 里根本没有 `tool_calls`，工具调用会静默消失。
3. **连接失败不以 `GenerateContent` 的错误返回**。框架把 dial 错误塞进流里的第一个响应（`Response.Error`，`Type="stream_error"`），所以适配器必须在流内把它变成终止失败。
4. **HTTP 状态码不是字段**。框架的消息文本里嵌了 URL 与状态（`POST "http://…": 429 Too Many Requests {...}`），`ResponseError.Code` 为空。适配器从文本恢复状态码与 provider 自身的错误类型，而**不能**把 dial 消息里的端口号误读成状态码。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | adapter 负责类型转换、能力声明与生命周期；重试与 attempt 记账归 Harness 策略 |
| TypeScript 入口 | `packages/llm/llm-deepseek/src/adapter.ts`、`packages/llm/llm/src/retry-policy.ts` |
| Go 入口 | [plugins/llmtrpc/translate.go](../../plugins/llmtrpc/translate.go)、[plugins/llmtrpc/adapter.go](../../plugins/llmtrpc/adapter.go) |
| 保持的行为 | reasoning/文本/工具调用分片转成同一套 chunk；usage 转成不相交计数；API 错误与连接失败都变成终止失败；无法表达的块报错而非丢弃 |
| Go 表达 | 适配器实现 `llm.Provider`；块索引固定为 reasoning=0、text=1、工具调用从 2 开始并按框架的 tool-call index 偏移，保证同一调用的分片共享索引而不同调用不共享 |
| 不等价边界 | 见下节 |

### 与参考实现的差异

| 差异 | 原因 | 影响 |
| --- | --- | --- |
| 未实现 `assistant-stream` 的紧凑 attempt 记录 | 属 M6 持久化范围 | 当前不声称能读写 DSH 的 attempt 记录格式 |
| provider 错误只恢复 `type`/`message`，不用其 `code` 字段 | provider 之间 `code` 类型不一致（数字或字符串） | 状态码单独从状态行恢复；`RequestID` 仅在框架提供字符串 code 时填充 |
| 适配器显式关闭 SDK 内部重试 | 重试由本项目策略拥有 | 一次 attempt 恰好一次 HTTP 请求，可被测试断言 |
| 无 reasoning 签名回传 | 参考的 `ReasoningSignature` 用于 AWS Bedrock | 未接入该 provider，不声称支持 |

## 一次操作的时序与所有权

1. `Adapter.Generate` 校验 context 与请求，转换成本框架请求；**转换失败在发请求之前**返回。
2. 惰性创建底层 `openai.Model`（`WithBaseURL` + `WithShowToolCallDelta(true)`），并关闭内部重试。
3. `GenerateContent` 返回响应通道；适配器用 `stream` 包装它。
4. `stream.Recv` 按响应顺序翻译：分片 → usage → 终止。终止 chunk 只在最后发出一次。
5. 通道关闭而没有终止 chunk 时补一个显式的 `missing-finish` 失败，而不是伪造 `stop`。
6. `Adapter.Close` 置位并释放模型；之后的 `Generate` 返回 `ErrClosed`。`stream.Close` 不取消调用方 context。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| 每次尝试都被当成截断 | 用 `Done` 判断结束 | [差异清单](../../testdata/parity/llmtrpc/DIVERGENCES.md) 第 1 条 | 改用 `IsPartial=false` 作为终止信号 |
| 工具调用凭空消失 | 未开启 `WithShowToolCallDelta` | `loopback_test.go` 的 `block=… kind=tool-call` 断言 | 适配器已默认开启；自定义 Model 时需注意 |
| 连接失败被当成成功空回答 | 只检查 `GenerateContent` 的返回值 | `TestAdapterReportsConnectionFailureInsideTheStream` | 在流内处理 `Response.Error` |
| `unsupported content block image` | 请求带附件或未知块 | `TestToRequestRefusesUnrepresentableBlocks` | 附件投影属 M8；不要吞错误 |

## 回到源码与下一步

阅读顺序：`plugins/llmtrpc/translate.go` 的 `ToRequest` → `Translate` → `fromResponseError`，再读 `adapter.go` 的 `stream.Recv`。对应测试：`translate_test.go`、`loopback_test.go`、`adapter_coverage_test.go`，以及跨 Provider 契约 `internal/testkit/provider_contract_test.go`。

读者应能回答：机制解决什么问题（把框架语义隔离在一个包里）、谁拥有资源（适配器持有底层模型，调用方关闭流）、Go 与 DSH 的差异在哪里（紧凑 attempt 记录、reasoning 签名、重试归属）。

下一篇：[从 Go 理解工具](../tools/go-primer.md)——工具定义、准入与执行管线。
