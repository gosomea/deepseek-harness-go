---
description: "不调用真实模型：用脚本化的 fake Provider 跑完一次流式尝试，并把它翻译成 tRPC 请求形态。"
kind: "command-example"
---

# examples/model

## 概述

这个程序不联网、不需要密钥：它让脚本化的 fake Provider 依次吐出 `block-start`、三段 `tool-call-delta`、`block-end`、`usage` 与 `finish`，然后打印 assembler 组装出的块、终止原因与 token 计数，最后把**同一个请求**交给 `plugins/llmtrpc` 的转换函数，观察它在 wire 上是什么样子，以及无法表达的块怎样被明确拒绝。读它能让[流与适配笔记](../../docs/llm/streaming.md)里的每一步都有可对照的实体。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

演示 M4 的四条核心行为：

1. **分片不等于消息**：三段参数分片组成一个 `tool-call` 块，原始字节一字不改。
2. **流可以结算**：`usage` 在终止 chunk 之前到达，`finish=tool-calls` 表明这次尝试为什么结束。
3. **同一契约两个 Provider**：fake Provider 与 tRPC 适配器接受同一个 `llm.Request`，形态转换只发生在适配器内。
4. **不能表达就报错**：`image` 块不会进入 wire，适配器返回错误并点名该块。

它不负责真实端点、工具执行与持久化——那些属 M7 及后续阶段。

## 使用

```sh
go run ./examples/model
```

输出逐字保存在 [expected.txt](expected.txt)，并由 `TestDemoMatchesDocumentedOutput` 比对。程序不读时钟、不联网，因此两次运行输出相同。

## 验证

```sh
go test -count=1 ./examples/model/...
go run ./examples/model
```

它由 `TestDemoMatchesDocumentedOutput` 与 `TestDemoIsDeterministic` 固定：前者逐字比对 `expected.txt`，后者要求两次运行输出相同。

## 限制

- **没有真实模型**。fake Provider 重放固定脚本；真实端点、凭据与联网 smoke 属 M7，缺密钥不算通过。
- **没有工具执行**。程序只到“模型请求了一次调用”为止；准入与执行管线见 [examples/tools](../tools/README.md)。
- **tRPC 部分只做转换**，不发请求；回环 HTTP 验证在 `plugins/llmtrpc` 的测试里。
