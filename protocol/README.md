---
description: "protocol：稳定的 wire DTO、冻结版本与错误词汇，区分内部 Go 接口与外部协议。"
kind: "package-library"
---

# protocol

## 概述

这个包定义 Go Host 对外暴露的 **wire DTO、版本协商与错误词汇**。内部 Go 接口不是稳定协议：Go 方法改签名有编译器抓，而 wire 消息改形状会悄悄破坏几个月前编译的客户端。因此本包命名自己的请求／响应值、冻结协议版本，并把未知字段、方法或版本当作**明确可检查的条件**而非静默忽略。

读者需要 Go 的 JSON 与接口基础。网关分发见 [plugins/hostapi](../plugins/hostapi/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Envelope` 是每条消息的外框：版本、请求 id、方法、可选认证与载荷。
- `Version` 是冻结的协议版本整数；版本不符返回 `ErrVersionMismatch`。
- `WireError` 用稳定 `ErrorCode` 与人类消息分离，客户端按 code 分支。
- `Response` 成功带 Result、失败带 Error，二者不可同时存在。

## 使用

下面的片段构造并校验一条请求信封；未知版本会被 `Validate` 拒绝。

```go fragment
env := protocol.Envelope{Version: protocol.Version, ID: "r1", Method: "session.list"}
err := env.Validate()
```

## 理解实现

- **版本是单点而非范围**：两端要么一致要么一端拒绝，因此「能发哪些消息」在发请求前可判定。
- **请求 id 必需**：响应按 id 匹配而非到达顺序，慢请求不会收到别人的答复。
- **未知方法拒绝、未知字段容忍**：忽略字段是版本陷阱；静默忽略方法调用更糟。

## 进一步阅读

- [协议与远程能力](../docs/protocols/overview.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./protocol/...
```

覆盖：Envelope 校验、版本不符、解码／编码、Response 互斥、WireError 渲染。

## 限制

- **只定义线格式**。传输与分发属 hostapi 与后续 SDK。
- **不含具体方法 schema**。方法带各自的 DTO。
