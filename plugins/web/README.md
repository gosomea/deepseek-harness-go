---
description: "web：有界的 fetch 与 search seam，把外部内容当数据而非指令。"
kind: "package-library"
---

# plugins/web

## 概述

这个包提供 **web fetch 与 search 能力 seam**。web 内容是外部不可信输入，可能巨大、缓慢或恶意；fetcher 返回有界、结构化结果，并把正文当数据、绝不当指令。search 是独立小接口，只取不搜的宿主不依赖 search Provider。Provider 是 seam：真实网络客户端与 keyless mock 满足同一接口，测试用函数驱动重定向、大小上限与超时，live smoke 单列。

读者需要 Go 的 `io` 与 `context` 基础。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Provider.Fetch` 经 `Backend` 拉取 URL，按 `limit` 截断并标记 `Truncated`。
- `FetchResult` 带最终 URL、状态码与有界正文。
- `Searcher` 接口返回搜索命中。

## 使用

下面的片段用一个函数 backend 构造 provider 并取一个 URL，正文超过 10 字节时截断。

```go fragment
p, _ := web.New(backend)
result, _ := p.Fetch(ctx, "https://example.test/x", 10)
// result.Truncated == true when the body is longer than 10 bytes
```

## 理解实现

- **有界读**：只读 `limit+1` 字节，多读 1 字节判定截断，不把整段读进内存。
- **默认上限**：`limit<=0` 用 1 MiB，调用方无法请求无界读。
- **截断显式**：超限截断并置 `Truncated`，不静默伪装成完整内容。

## 进一步阅读

- [协议与远程能力](../../docs/protocols/overview.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/web/...
```

覆盖：截断、不截断、nil 正文、空 URL、backend 错误、无 backend。

## 限制

- **不含真实网络客户端**。真实 fetch／search Provider 属后续；本阶段 keyless。
