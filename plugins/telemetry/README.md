---
description: "telemetry：隐私边界受限的产品事件记录，非白名单字段被拒绝。"
kind: "package-library"
---

# plugins/telemetry

## 概述

这个包记录**隐私边界受限**的产品事件。遥测位于必须显式化的边界：离开 host 的只有固定的数值/状态字段，绝不含消息内容或凭据。本包让该边界可检查——非白名单字段被拒绝——使「发送了什么」是规则而非期望。keyless、进程内。

读者需要 Go 的接口与 map 基础。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Recorder.Emit` 校验字段是否在白名单内，再转交 `Sink`。
- `Sent` 返回已发送事件，供测试观察。

## 使用

下面的片段记录一个只含白名单字段（status/count）的事件；含 `prompt` 等字段会被拒绝。

```go fragment
r, _ := telemetry.New(sink)
err := r.Emit(telemetry.Event{Name: "turn", Fields: map[string]string{"status": "ok", "count": "3"}})
```

## 理解实现

- **白名单强制**：非白名单字段返回 `ErrForbiddenField`，且在到达 sink 之前拒绝。
- **内容/凭据不在白名单**：prompt、消息正文、token 等不允许出现。

## 进一步阅读

- [发行缺口](../../docs/release/distribution.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/telemetry/...
```

覆盖：允许字段发送、禁止字段拒绝且不达 sink、空名与 nil sink 拒绝。

## 限制

- **不实现真实遥测后端**。真实上报与隐私审查属后续/缺口。
