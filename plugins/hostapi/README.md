---
description: "hostapi：按版本信封分发方法、认证与订阅通知的进程内 Host API 网关。"
kind: "package-library"
---

# plugins/hostapi

## 概述

这个包提供**进程内 Host API 网关**：把带版本的 protocol 信封分发到已注册方法、对受保护方法强制认证、把通知投递给订阅者。它是协议的 host 一半：wire DTO 在 protocol，本包负责**关联（按请求 id）、授权与订阅扇出**。网关与传输无关，测试用信封进、响应出，无套接字；网络传输复用同一分发，因此授权与关联只在 keyless 下证明一次。

读者需要 Go 的接口与 `context` 基础。线格式见 [protocol](../../protocol/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Register` 注册方法（名字、是否需认证、处理器）。
- `Dispatch` 校验信封 → 解析方法 → 强制认证 → 调用处理器；版本或方法错误在处理器之前拒绝。
- `Subscribe`／`Publish` 订阅与投递通知，慢消费者丢弃而非阻塞。
- `AuthContext` 把认证主体显式传给处理器。

## 使用

下面的片段注册一个需认证的方法并分发一条带 token 的请求；网关在处理器前完成认证。

```go fragment
g := hostapi.New(hostapi.Options{Authenticate: authenticate})
_ = g.Register(hostapi.Method{Name: "secure", RequiresAuth: true, Handler: handler})
resp := g.Dispatch(ctx, env)
```

## 理解实现

- **顺序固定**：信封校验→方法解析→认证→处理器；错误在处理器前返回确定 code。
- **认证只做一次**：处理器拿 `AuthContext`，不能把无认证当成已认证。
- **慢消费者丢事件**：缓冲满时 `Publish` 丢弃而非阻塞发布会话。

## 进一步阅读

- [协议与远程能力](../../docs/protocols/overview.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/hostapi/...
```

覆盖：未知方法、认证缺失／错误／成功、版本不符、重复方法、处理器错误、订阅发布与退订、方法列表。

## 限制

- **进程内分发**。网络传输包装同一分发，属后续。
- **订阅为内存缓冲**，不持久重放。
