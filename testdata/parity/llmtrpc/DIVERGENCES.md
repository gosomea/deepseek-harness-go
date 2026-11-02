# 已声明的差异：tRPC-Agent-Go 适配器

本文件记录 `plugins/llmtrpc` 与固定参考之间的**已知且已解释**的差异。差异不等于缺陷：每一条都写明依据与影响，避免读者把“行为不同”误读成“行为等价”。

固定依赖：`trpc.group/trpc-go/trpc-agent-go v1.11.2`。固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`。

## 框架侧的真实语义（探测所得）

以下四条不是本项目的选择，而是框架的实际行为；探测方式见 `plugins/llmtrpc/loopback_test.go` 与开发时的临时探针，均由回环 HTTP 服务器复现，无需网络与密钥。

| # | 框架行为 | 若按直觉处理会怎样 | 适配器的处理 |
| --- | --- | --- | --- |
| 1 | `model.Response.Done` 对完整流始终为 `false`；终止信号是 `IsPartial=false` | 每次尝试都被当成被截断 | 用 `IsPartial` 判定终止，`Done` 不参与判断 |
| 2 | 工具调用的参数分片默认不出现在 delta 中，需 `openai.WithShowToolCallDelta(true)` | 工具调用静默消失 | 适配器显式开启该选项 |
| 3 | 连接失败与 API 错误都通过流内 `Response.Error`（`Type="stream_error"`）到达，`GenerateContent` 不返回错误 | 连接失败被当成成功空回答 | 在 `stream.Recv` 内转成终止失败 chunk |
| 4 | `ResponseError.Code` 为空；HTTP 状态码嵌在消息文本里 | 状态码丢失，无法区分限流与请求错误 | 从文本恢复状态码，并把 dial 消息里的端口号排除在外 |

## 本项目相对于参考实现的差异

| # | 差异 | 原因 | 影响与限制 |
| --- | --- | --- | --- |
| D1 | 未实现 `assistant-stream` 的紧凑 attempt 记录（`text-chunks`/`tool-call-chunks` 等） | 属 M6 持久化范围 | 不声称能读写 DSH 的 attempt 记录格式；M4 只有组装 |
| D2 | 失败事实只恢复 provider 的 `type` 与 `message`，不使用其 `code` 字段 | provider 之间 `code` 类型不一致（数字或字符串） | HTTP 状态单独从状态行恢复；`RequestID` 仅在框架提供字符串 code 时填充 |
| D3 | 适配器显式关闭框架内部重试 | 重试与 attempt 记账由本项目策略拥有 | 一次 attempt 恰好一次 HTTP 请求，可被测试断言；避免不可观察的重复请求 |
| D4 | 未实现 reasoning 签名回传 | 参考的 `ReasoningSignature` 面向 AWS Bedrock | 未接入该 provider，不声称支持 |
| D5 | 未实现 `IMAGE_OFFLOAD_REQUIRED` 与图片计价 | 属 M8 上下文管理 | 附件在本阶段被明确拒绝（`ErrUnsupported`），不会被静默丢弃 |
| D6 | 未知块（`UnknownBlock`）在适配器被拒绝 | 本层无法在 wire 上表达 | 持久层仍保留原文；拒绝发生在适配器而不是被改写成文本 |

## 未验证项

- **没有调用任何真实端点**。全部检查使用本地回环 HTTP 服务器与固定 SSE 正文；真实 API、凭据与联网 smoke 属 M7。
- **框架语义基于 v1.11.2 探测**。升级版本后第 1–4 条需要重新验证，验证方式与新结论应更新到本文件。
