---
description: "用 fake Provider 与 fake 工具跑完一次轮次，打印权威事件顺序并重建每次模型请求。"
kind: "command-example"
---

# examples/harness

## 概述

这个程序不联网、不需要密钥：它让脚本化的 fake Provider 先请求一次 `read_file` 工具，工具返回内容后模型再给出文本回答，然后打印三件事——**权威日志的事件顺序**、**Provider 实际收到的两次请求**、以及**从日志重建出的历史**。读它能让[turn 与 step](../../docs/agentloop/turns.md) 里的每一步都有可对照的实体。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

演示 M5 的四条核心行为：

1. **完整轮次**：两次模型请求加一次工具调用，事件顺序为 `turn/start → user/message → step/start → request/header → assistant/attempt → assistant/message → tool/call → tool/result → step/end → … → turn/end`。
2. **请求可由日志重建**：第二次请求携带了工具结果，而它是驱动从 `session.Log` 派生出来的。
3. **先记录后执行**：`tool/call` 出现在 `tool/result` 之前。
4. **工具调用是块**：请求工具的 assistant 消息可见文本为空，但它带有 `tool-call` 块——这一点由程序显式打印，避免被误读成「空回答」。

它不负责真实模型、持久化或审批——那些属 M7 与 M6。

## 使用

```sh
go run ./examples/harness
```

输出逐字保存在 [expected.txt](expected.txt)，并由 `TestDemoMatchesDocumentedOutput` 比对。程序使用固定时钟与脚本化 Provider，因此两次运行输出相同。

## 验证

```sh
go test -count=1 ./examples/harness/...
go run ./examples/harness
```

测试逐字比对 `expected.txt` 与两次运行的一致性，并断言 `tool/call` 必须先于 `tool/result`。

## 限制

- **没有真实模型**：fake Provider 重放固定脚本；真实端点与凭据属 M7。
- **工具是替身**：不读写本机文件；能力、审批与沙箱属 M7。
- **没有持久化**：日志在内存中；磁盘格式与恢复属 M6。
- 本示例只跑一条轮次，不演示排队输入与取消；那两条行为由 `agentloop` 的测试覆盖。
