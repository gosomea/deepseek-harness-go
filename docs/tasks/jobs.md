# 持久任务与调度

## 读完要解决的问题

一个后台任务怎样在进程重启后仍然可解释？读完后你能：区分 goroutine 与持久 job；用 lease 认领、幂等键去重、有界重试；解释为什么「可能已产生副作用但结算丢失」的 job 必须记为 unknown 而不是自动重放。需要 Go 的互斥与时间基础。当前实现是 `capability/jobs` 与 `plugins/schedule`。

## 从熟悉的 Go 写法出发

普通 Go 里，`go doWork()` 启动一个 goroutine。它并发执行，但进程一退出就消失，且没有任何记录说明「这个任务请求过、跑到哪、结算没」。DSH 的 job 是**持久记录 + 状态机**：它在被 claim 前就存在，离开进程后仍在记录里，重启后按状态决定是否可继续。类比在「并发执行」上成立，在「跨重启可解释」上不等价。

## 最小实验

在仓库根目录：

```sh
go run ./examples/jobs
```

输出逐字保存在 [examples/jobs/expected.txt](../../examples/jobs/expected.txt)：

```text output-file=examples/jobs/expected.txt
fired: 1
settled status: settled
duplicate fires: 0
unknown not claimable: true
unknown count: 1
```

观察重点：`fired: 1`（一个间隔后触发一次）；`duplicate fires: 0`（同一 tick 不重复）；`unknown not claimable: true`（结算丢失的 job 不再被认领）。

## 先预测，再观察

先预测：一个 `MaxAttempts: 1` 的 job 第一次 `Fail` 后处于什么状态？写出预期再对照实现——`Attempts(1) <= MaxAttempts(1)`，因此回到 `pending` 可重试；第二次 `Fail` 时 `Attempts(2) > 1`，转为终态 `failed`。恢复方式：调整 `MaxAttempts` 或改为 `Settle`。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 持久 jobs、可控时钟调度、lease 与幂等 |
| TypeScript 入口 | 固定提交 `packages/jobs`、`packages/schedule` |
| Go 入口 | [capability/jobs](../../capability/jobs/README.md)、[plugins/schedule](../../plugins/schedule/README.md) |
| 保持的行为 | 已结算不可重启；lease 过期可回收；unknown 不自动重放；重试有界 |
| Go 表达 | 互斥保护的 state machine + 注入时钟 + 幂等键 |
| 不等价边界 | 内存存储；持久化 Provider 属 M12 |

## 一次操作的时序与所有权

`Scheduler.Tick` 在注入时钟上判断到期 entry，经 store `Enqueue` 入队（幂等键防重复）；runner `Claim` 在 lease 下认领，`Settle`／`Fail`／`MarkUnknown` 完成转移。store 持有记录，调度器只入队不运行。

## 失败与定位

- **`ErrLeaseHeld`**：lease 未过期仍被另一 owner 持有。修复：等待过期或换 owner。
- **`ErrNotClaimable`**：已结算或 unknown 的 job 不可认领。修复：核对状态。
- **`ErrUnknownJob`**／**`ErrDuplicateProvider`**：id 不存在／重复。修复：核对 id。

## 回到源码与下一步

读 [capability/jobs](../../capability/jobs/README.md) 与 [plugins/schedule](../../plugins/schedule/README.md)。下一步：[Workflow 与委派](workflow.md)。
