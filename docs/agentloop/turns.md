# turn 与 step：一次轮次写了哪些事件

## 读完要解决的问题

你已经知道 Session 日志是权威、消息数组只是投影。现在的问题是：当用户说一句话之后，**日志里到底多了什么**，以及哪些增长只是痕迹、哪些会变成模型能看到的历史？

最直接的想法是“多一条 user 消息、多一条 assistant 消息”。它会崩在三处：模型请求工具时，那一次调用和它的结果如果不在日志里，重启后就无法解释第二次请求为什么长那样；一次失败的尝试如果被写成普通消息，重放就会假装它成功了；而如果只有消息、没有边界，“第几条到第几条属于同一轮”就只能靠猜。

这一篇把一条轮次拆成 **turn → step → 请求 → 结算 → 工具批**。前置知识是 Go 的切片与 `context.Context`，以及 [事件溯源](../session/go-primer.md)。关联行为见 [M5 执行契约](../10-plans/dsh-go-replication-m05/plans.md)。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/core/agent-loop/src/agent.ts` 与 `docs/agent-lifecycle.zh.md`。

## 从熟悉的 Go 写法出发

最直白的做法是维护一个内存历史：

```go fragment
history = append(history, userMessage)
reply := callModel(history)
history = append(history, reply)
```

它清楚、能跑。边界在于**历史只存在于内存里**。进程重启后你无法回答“第二次请求为什么带着工具结果”，因为那份历史从未被记录；并发投递输入时也无法判断哪条输入属于哪一轮，因为没有轮次边界。

参考实现把每次尝试和每次调用都写进日志，并在日志里维护轮次与步骤的边界。于是“重建请求”变成从日志派生，而不是从内存读取。

## 最小实验

准备条件：仓库根目录，Go 1.22，不需要密钥或网络。

```sh
go run ./examples/harness
```

它用 fake Provider 跑一条轮次：模型请求 `read_file`，工具返回内容，模型给出最终回答。程序打印**完整事件顺序**，并从日志重建两次请求。输出由 `examples/harness/expected.txt` 保存并逐字比对。

观察重点：`tool/call` 出现在 `tool/result` **之前**，两者都出现在第二次 `step/start` 之前。这个顺序是「先记录请求、再执行、再结算」的直接体现。

## 先预测，再观察

先写下预期：如果模型在流中途失败（没有终止 chunk），日志里会不会多一条 assistant 消息？

不会。那次尝试写成 `assistant/attempt` 且 `kind=incomplete`，**不产生** `assistant/message`。可用 M5 的测试验证：

```sh
go test -count=1 -run TestIncompleteStreamIsRecordedAsAnIncompleteAttempt ./agentloop/...
```

观察重点：失败被记录为失败，而不是被写成一条短回答。要恢复到正常场景，让 fake 脚本带上终止 chunk。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | turn/step 边界、request header、assistant attempt、tool call 与 result |
| TypeScript 入口 | `packages/core/agent-loop/src/agent.ts` 的驱动序列、`docs/agent-lifecycle.zh.md` 的时序图 |
| Go 入口 | [agentloop/turn.go](../../agentloop/turn.go)、[agentloop/events.go](../../agentloop/events.go) |
| 保持的行为 | 请求先记录后发送；tool/call 先于 tool/result；失败与非完整尝试留痕但不成消息；每个出口都写 turn/end |
| Go 表达 | 每种事件一个带判别字段的载荷结构体，JSON 编码后入日志；`stepOutcome` 只描述“下一步为什么继续”，不携带状态 |
| 不等价边界 | 未实现 hook waterfall；未写 `agent/assistant-stream` 对应的实时投影事件，实时输出不在日志里承担权威角色 |

## 一次操作的时序与所有权

1. `Run` claim inbox 的全部输入；无输入则**不开轮次**。
2. 写 `turn/start`，随后把每条输入写成 `user/message`。
3. 每个 step：写 `step/start` → 从日志派生请求 → 写 `request/header` → 发送 → 写 `assistant/attempt` → 成功则写 `assistant/message`。
4. 若模型请求工具：对每个调用先写 `tool/call`，执行后写 `tool/result`，然后用结果继续下一步。
5. 模型自然停止且 inbox 为空 → 写 `step/end` 并结束轮次。
6. 任何出口（成功、失败、取消）都写 `turn/end` 与对应的 `step/end`。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| `llm: stream ended without a terminal finish` | 流被截断 | `TestIncompleteStreamIsRecordedAsAnIncompleteAttempt` | 检查 provider；日志里会留下 `kind=incomplete` 的 attempt |
| 第二次请求没有工具结果 | 工具调用未闭合（缺 `block-end`） | `TestSecondRequestIsRebuiltFromTheLog` | 让适配器发出 `block-end`；未闭合的调用不会成为消息 |
| `agentloop: step limit reached` | 模型持续请求工具 | `TestStepLimitStopsARunawayTurn` | 提高上限或修正提示；命中上限会把轮次记为失败 |
| 事件被日志拒绝 | 载荷形态不合 surface 规则 | `TestTurnAbortsWhenAnAttemptCannotBeRecorded` | `user/message` 用顶层消息，其余四类用 `"message"` 嵌套 |

## 回到源码与下一步

阅读顺序：`agentloop/turn.go` 的 `Run` → `runSteps` → `runStep`，再读 `agentloop/events.go` 的各类载荷。对应测试：`agentloop/agentloop_test.go`、`agentloop/error_paths_test.go`。

下一步：[inbox 与追加输入](inbox.md)——轮次进行中追加的输入在哪里生效。
