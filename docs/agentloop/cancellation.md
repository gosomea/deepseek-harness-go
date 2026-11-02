# 取消与关闭：三件不同的事

## 读完要解决的问题

你已经会写 `context.WithCancel`。现在的问题是：对一个正在跑的 Agent，**取消一轮**、**销毁实例**、**关闭注册表**分别意味着什么，以及为什么它们必须分开。

把它们混成一件会发生三类事故：取消一轮却顺手销毁了实例，调用方再也无法继续对话；销毁时不等活动轮次结束，调用方一返回就去读共享状态，结果读到驱动还在写的内容；取消时把「流被截断」当成真实原因上报，掩盖了「是调用方取消的」这一事实。

这一篇区分三件事：**取消（Cancel）**、**销毁（Dispose）**、**关闭注册表（Registry.Close）**。前置知识是 Go 的 `context` 与 `sync`。关联行为见 [M5 执行契约](../10-plans/dsh-go-replication-m05/plans.md)，以及 [C03 资源所有权](../cordis/lifecycle.md)。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/core/agent-loop/src/agent.ts` 的取消路径。

## 从熟悉的 Go 写法出发

最直白的做法是让 `Stop` 既取消又释放：

```go fragment
func (a *Agent) Stop() {
	a.cancel()
	close(a.done)
}
```

它清楚、能跑。边界在于**它同时做了两件事**，而调用方往往只需要一件。想「打断这一轮但继续用」的代码只能重建实例；而 `close(a.done)` 立刻执行意味着「已停止」变成了「可能还在写日志」。

正确形状是把能力拆开，并让销毁**等待**活动轮次真正释放：

```go fragment
type Canceler interface {
	Cancel(ctx context.Context) (bool, error)
}

type Owner interface {
	Dispose(ctx context.Context) error
}
```

## 最小实验

准备条件：仓库根目录，Go 1.22。实验用阻塞 provider，让轮次真正处于「飞行中」。

```sh
go test -count=1 -run 'TestCancelInFlightTurnKeepsTheAgentUsable|TestDisposeWhileATurnIsInFlightReturns' ./agentloop/...
```

观察重点：取消之后实例仍然 `idle` 且能接受新输入；而 `Dispose` 在活动轮次中会**等待**该轮次结算后才返回——测试用 10 秒上限来证明它不会挂死。

## 先预测，再观察

先写下预期：一个「取消导致的流中断」，驱动应该报 `ErrIncompleteStream` 还是 `context.Canceled`？

应当是 `context.Canceled`。流被截断是**症状**，取消才是**原因**；报症状会让调用方无法区分「provider 出错」和「是我取消的」。观察方式：

```sh
go test -count=1 -run TestCanceledTurnClosesWithACancellationRecord ./agentloop/...
```

日志里会留下 `turn/end` 且带 `"canceled":true`，而**不会**出现 `assistant/message`——被取消的尝试不得变成一条成功消息。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 取消原因、流中断、工具取消、关闭与并发控制 |
| TypeScript 入口 | `packages/core/agent-loop/src/agent.ts`；`packages/core/llm/src/types.ts` 的 `FinishReasonMap`（`aborted` 携带 failure） |
| Go 入口 | [agentloop/control.go](../../agentloop/control.go) 的 `Cancel`／`Dispose`、[agentloop/agent.go](../../agentloop/agent.go) 的 `dispose`／`waitForSettlement` |
| 保持的行为 | 取消保留 inbox 与实例；销毁等待活动轮次释放；取消后不派发新工具；已发生的结果仍入日志；取消是独立于失败的事实 |
| Go 表达 | 三种能力三个接口；`dispose` 等 **idle 通道**而不是等 `done`（`done` 只表示已释放，等它会自锁）；context 已取消时以原因为准 |
| 不等价边界 | 未实现参考的取消传播到工具批内部的细粒度阶段；本阶段保证「未派发的调用不执行、已派发的按结果结算」 |

## 一次操作的时序与所有权

1. `Cancel`：取出当前轮次的 cancel 函数并调用；返回「是否有活动工作」；实例保持存活。
2. 被取消的 step：写 `assistant/attempt`（`kind=aborted`），不再派发新的工具调用，轮次以 `turn/end{canceled:true}` 关闭。
3. `Dispose`：关闭 inbox → 取消活动轮次 → **等待结算**（受调用方 context 约束）→ 置为 `disposed` 并关闭 `done`。
4. `Registry.Close`：关闭注册表标记 → `factory.DisposeAll` 逆序释放 → 清空工厂。

所有权：调用方拥有 `Owner`；`Canceler` 是独立能力；`done` 只表示「已释放」，不表示「已请求停止」。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| `Dispose` 长时间不返回 | 活动轮次未结算（provider 不响应取消） | `TestDisposeHonoursADeadlineWhileATurnIsInFlight` | 给 `Dispose` 传带超时的 context；它会返回 `context.DeadlineExceeded` |
| 取消后实例像被销毁了 | 用错了方法（`Dispose` 而非 `Cancel`） | `TestCancelInFlightTurnKeepsTheAgentUsable` | 只用 `Canceler.Cancel` 打断工作 |
| 报 `ErrIncompleteStream` 而非取消 | 原因被症状覆盖 | `TestCanceledTurnClosesWithACancellationRecord` | 已修：context 已取消时以取消为准 |
| 销毁后仍能 `Send` | 不应该发生：inbox 会先关闭 | `TestSendAfterDisposalIsRefused` | 检查是否直接操作了内部状态 |

## 回到源码与下一步

阅读顺序：`agentloop/control.go` 的 `Cancel`／`Dispose` → `agentloop/agent.go` 的 `dispose`／`settle`／`waitForSettlement` → `agentloop/turn.go` 的取消检查。对应测试：`agentloop/cancel_test.go`、`agentloop/defensive_test.go`。

读者应能回答：机制解决什么问题（把打断、销毁与关闭分开，并让销毁可等待）、谁拥有资源（工厂拥有实例，调用方拥有 context）、Go 与 DSH 的差异在哪里（取消原因以 Go 的 context 为准，未实现工具批内部细粒度取消）。
