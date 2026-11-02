---
description: "tRPC 适配器的回环 HTTP 固定正文：无密钥、无外网地验证转换行为。"
kind: "package-library"
---

# testdata/parity/llmtrpc

## 概述

本目录保存 `plugins/llmtrpc` 的**回环 HTTP** 固定正文：这些文本被 `httptest` 服务器原样返回，适配器用真实的 HTTP 客户端、真实的 SSE 解码器与真实的转换函数处理它们，只有端点在本机。因此契约在**无密钥、无外网**的条件下被验证。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- 提供三种 wire 形状的固定正文：正常流、HTTP 200 内嵌错误、被截断的流。
- 记录这些正文的**来源与依赖版本**，使「探测结论随版本失效」这件事可核对。
- 不判断适配器行为是否正确；断言由 `plugins/llmtrpc` 的测试承担。

## 使用

```sh
go test -count=1 ./plugins/llmtrpc/...
```

测试启动进程内的 `httptest` 服务器并返回这些正文。测试断言直接针对适配器输出，因此正文用于**人工核对**：当探测结论与文档不符时，先比对这几份正文。

## 理解实现

| 文件 | 覆盖的形状 | 对应断言 |
| --- | --- | --- |
| `sse/text-reasoning-toolcall.txt` | reasoning、文本与分片参数交错到达 | 三类内容落到不同块索引；参数拼回原始 JSON；`finish=tool_calls` |
| `sse/api-error.txt` | HTTP 200 内嵌 provider 错误 | 转成终止失败而不是成功空回答，且恢复 provider 的 `type` 与 `message` |
| `sse/truncated-stream.txt` | 响应结束、通道关闭、无终止原因 | 转成显式 `missing-finish` 失败 |
| `provenance.json` | 固定依赖版本与探测事实 | 升级框架后需重新探测并更新 `DIVERGENCES.md` |

## 进一步阅读

- [差异清单](DIVERGENCES.md)：探测出的四条框架语义与本项目的六条差异。
- [tRPC 适配器参考](../../../docs/llm/trpc-adapter.md)：转换与生命周期说明。
- [适配器包](../../../plugins/llmtrpc/README.md)：公开接口与限制。

## 验证

```sh
go test -count=1 ./plugins/llmtrpc/...
go run ./scripts/doccheck
```

后一条检查本目录的本地链接与 README 章节完整性。

## 限制

- 这些正文**不是**从真实服务录制的，而是按 OpenAI 兼容流式协议手工构造的最小样例；它们证明适配器在给定 wire 形状下的行为，**不**证明与某个具体服务商的兼容性。
- 没有真实的 SSE 分帧边界场景（例如一个事件被拆成多个 TCP 分段）；该场景由框架的 SSE 解码器负责，不在本目录覆盖范围。
- 升级 `trpc.group/trpc-go/trpc-agent-go` 后，`DIVERGENCES.md` 中记录的框架语义需要重新探测。
