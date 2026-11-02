---
description: "schedule：在可控时钟上驱动持久 job 的固定间隔调度器。"
kind: "package-library"
---

# plugins/schedule

## 概述

这个包在**可控时钟**上驱动持久 job。它在注入的时钟上 tick 而非墙钟，因此测试可确定地推进时间并观察哪些 entry 到期。它经 jobs store 认领到期 job，使调度与执行分离：时钟决定**何时**，runner 决定**做什么**。

读者需要 Go 时间与接口基础。状态机见 [capability/jobs](../../capability/jobs/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Register` 注册固定间隔 entry，首次到一个间隔后到期。
- `Tick` 把当前时钟下到期的 entry 入队为 job，返回 job id；用 entry+触发时间的幂等键，重复 tick 不重复入队。
- `NextRun` 查询某 entry 下次到期时间。

## 使用

下面注册一个每分钟的 entry 并在一个 tick 后观察触发。

```go fragment
scheduler := schedule.New(store, func() time.Time { return now })
_ = scheduler.Register("heartbeat", "beat", time.Minute)
fired, _ := scheduler.Tick()
```

## 理解实现

- **单 tick 不重复**：每个触发的 entry 立即推进到 now 之后，一次 tick 不会重复入队同一 entry。
- **幂等键**：job id 由 entry 名与触发时刻生成，重复 tick 命中原有 job。
- **时钟注入**：`now` 为 nil 时才用真实时钟，测试总是注入。

## 进一步阅读

- [持久任务与调度](../../docs/tasks/jobs.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/schedule/...
```

覆盖：注册、重复注册拒绝、未到期不触发、到期触发一次、重复 tick 不重复、NextRun、参数校验。

## 限制

- **固定间隔**，不含 cron 表达式。
- **不运行 job**，只入队。
