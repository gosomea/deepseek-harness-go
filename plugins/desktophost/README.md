---
description: "desktophost：Desktop 载体的 Host 侧启动握手——就绪、致命失败、凭证注入与关闭。"
kind: "package-library"
---

# plugins/desktophost

## 概述

这个包提供 **Desktop 载体启动握手的 Go 侧**。桌面应用是带自己生命周期的私有 profile：以注入的凭证启动、必须报告就绪或致命启动错误、关闭时释放 host 与监听器。本包在 Go 侧建模该握手，使规则无需真实桌面壳或签名即可测试。

读者需要 Go 的互斥与状态机基础。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Ready`／`Fail` 记录就绪或致命启动错误。
- `Inject` 只在就绪后返回注入的凭证，证明壳收到确切凭证。
- `Close` 幂等关闭并计数。

## 使用

下面的片段走过就绪、注入与关闭；注入在就绪前被拒绝。

```go fragment
b := desktophost.New()
_ = b.Ready()
token, _ := b.Inject("tok")
_ = b.Close()
```

## 理解实现

- **就绪前不可注入**：`Inject` 在非就绪返回 `ErrNotReady`。
- **关闭后一律拒绝**：`Ready`/`Fail`/`Inject` 在关闭后返回 `ErrClosed`。
- **关闭幂等**：`CloseCount` 只增一次。

## 进一步阅读

- [客户端与投影](../../docs/client/overview.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/desktophost/...
```

覆盖：完整生命周期、就绪前注入拒绝、关闭幂等、关闭后拒绝、致命失败。

## 限制

- **仅 Host 侧**。Electron/原生壳打包与签名属未验证缺口。
