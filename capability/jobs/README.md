---
description: "jobs：持久任务记录与状态机，lease 认领、幂等键、重试与 unknown 状态。"
kind: "package-library"
---

# capability/jobs

## 概述

这个包记录**持久、可恢复的后台任务**。goroutine 不是任务：它随进程消失且不留记录。job 是有状态机的持久记录，由 owner 在 lease 下认领、按有界规则重试、重启后可恢复。本包只管记录与状态转移；调度器或时钟驱动它们。

**不承诺 exactly-once**：可能已产生外部副作用但结算未记录的 job 被置为 `unknown`，由人工裁决。把这种 job 当安全可重放，正是本包拒绝的错误。

读者需要 Go 的互斥与时间基础。调度见 [plugins/schedule](../../plugins/schedule/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Store.Enqueue` 记录 job；`IdempotencyKey` 使重复请求返回已有 job。
- `Claim` 在 lease 下认领；过期 lease 可重新认领（崩溃恢复）。
- `Settle`／`Fail`／`MarkUnknown` 完成状态转移；`Fail` 在仍有尝试时回到 pending。
- `ByStatus`／`IDs` 查询。

## 使用

下面入队、认领并结算一个 job；省略错误检查。

```go fragment
store := jobs.NewStore()
store.SetClock(clock)
job, _ := store.Enqueue(jobs.Job{ID: "j1", Kind: "k", MaxAttempts: 1})
job, _ = store.Claim("j1", "owner")
job, _ = store.Settle("j1")
```

## 理解实现

- **已结算不可重启**：`Claim` 对非 pending（且 lease 未过期）的 job 返回 `ErrNotClaimable`。
- **lease 过期可回收**：另一个 owner 在 lease 过期后可认领，崩溃的 job 不会永久卡住。
- **unknown 不自动重放**：`MarkUnknown` 的 job 不可再 claim，必须人工处理。
- **重试有界**：`Attempts > MaxAttempts` 时 `Fail` 终态失败，否则回 pending。

## 进一步阅读

- [持久任务与调度](../../docs/tasks/jobs.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./capability/jobs/...
```

覆盖：幂等、lease 冲突与过期、已结算拒绝、重试、unknown、重复 id、非法状态转移、排序。

## 限制

- **内存存储**。持久化 Provider 属 M12。
- **不运行任务**。执行由调度器与 runner 负责。
