---
description: "clientview：session 权威日志的客户端投影——快照、增量 delta 与断线恢复。"
kind: "package-library"
---

# plugins/clientview

## 概述

这个包构建 **session 权威日志的客户端投影**。UI 绝不能成为第二份权威：客户端显示的是 host 日志的投影，以快照（序列之前）或增量 delta（序列之后）下发。按序列重连使「无缺失、无重复」可检查而非寄望。投影是纯函数、keyless。

读者需要 Go 的切片与接口基础。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `BuildSnapshot` 构建到某序列的快照；乱序输入报错。
- `BuildDelta` 构建某序列之后的增量；出现空洞返回 `ErrGap`。
- `View.ApplySnapshot/ApplyDelta` 重建视图；delta 不从 throughSeq+1 起始即拒绝。
- `Hub.Resume` 客户端重连：优先增量，空洞时回退快照，并报告是否 resync。

## 使用

下面的片段从一个 hub 重建客户端视图：先取快照，再应用其后的增量。

```go fragment
hub, _ := clientview.NewHub(entries)
view, resynced, _ := hub.Resume(lastSeenSeq)
// resynced == true means the client had to resync from a full snapshot
```

## 理解实现

- **快照+delta == 全快照**：测试断言两者 `SameView`。
- **delta 起点严格**：不从 throughSeq+1 起始即 `ErrGap`，避免重复或跳条。
- **乱序即错**：乱序输入产生的客户端视图会与日志不符，因此是错误而非静默排序。

## 进一步阅读

- [客户端与投影](../../docs/client/overview.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/clientview/...
```

覆盖：快照/增量构建、乱序拒绝、快照+delta 等于全快照、delta 起点校验、重连增量与回退快照。

## 限制

- **纯投影**。真实前端 UI 与浏览器 E2E 属未验证缺口。
