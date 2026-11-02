---
description: "sdk：把方法调用变成版本信封、按请求 id 关联响应的 Go 客户端。"
kind: "package-library"
---

# plugins/sdk

## 概述

这个包提供 **Host API 的 Go 客户端**：把方法调用变成带版本的 protocol 信封，并按请求 id 把响应匹配回来。它让远程客户端与进程内调用共享一套调用约定：客户端与传输无关，由「发送函数」驱动——测试里发送函数就是网关本身，生产里是套接字。取消经 context 表达，取消的调用不会声称拿到结果。

读者需要 Go 的 `context` 与 JSON 基础。seam 见 [protocol](../../protocol/README.md) 与 [hostapi](../hostapi/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `New` 用发送函数与可选 token 构造客户端。
- `Call` 构造信封、发送、校验响应 id、解码结果或返回 wire 错误。
- 取消的 context 在发送前返回。

## 使用

下面的片段用网关作为发送函数调用一个方法；响应按 id 关联。

```go fragment
client, _ := sdk.New(gatewaySend(g), "token")
var out map[string]string
err := client.Call(ctx, "echo", map[string]string{"v": "hi"}, &out)
```

## 理解实现

- **id 不匹配即拒绝**：响应 id 与请求不符时报错，正是请求 id 要防的关联 bug。
- **取消先于发送**：context 已取消时直接返回，取消可观察。
- **wire 错误是可 branachable 的类型**：`WireError` 带 code。

## 进一步阅读

- [协议与远程能力](../../docs/protocols/overview.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/sdk/...
```

覆盖：正常调用、wire 错误、取消、响应 id 不符、nil 发送、解码错误、传输错误。

## 限制

- **无套接字传输**。网络传输是同一调用约定上的另一个发送函数，属后续。
