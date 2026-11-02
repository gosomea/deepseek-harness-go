---
description: "重开一个会话之后，哪些工作会继续、哪些必须停下来让人决定：恢复只描述，不重放副作用。"
kind: "concept-note"
---

# 恢复与 checkpoint：重开之后哪些工作会继续

## 读完要解决的问题

日志能落盘、能修复之后，下一个问题是**回去之后该做什么**。假设进程在一条轮次中间被杀掉，重开时日志里停在这些位置：

- 工具调用**已经记录**，但没有对应的结果 —— 工具体执行了吗？
- 轮次 `turn/start` 有，`turn/end` 没有 —— 这条轮次算完成了吗？
- 用户消息记下了，但后面没有 `step/start` —— 这条输入被消费过吗？

把这三个问题合并成「恢复到上次状态继续跑」是很危险的：**「结果没结算」不等于「副作用没发生」**。写文件、发请求、改数据库这些副作用是不可逆的，自动重跑一遍可能造成重复执行。

读完后你能：用 `persistence.Resume` 把「还欠什么工作」列出来；解释为什么恢复是**描述**而不是**行动**；用 `session/end-seed` 记录并读回一个 checkpoint；对照 `agentloop` 里一次轮次的事件顺序判断崩溃点。本文讨论的是**已实现**的代码。

需要的 Go 知识：切片遍历、`map`、`errors.Is`、`context`。

## 从熟悉的 Go 写法出发

普通的 Go 做法是「启动时把状态读出来，接着往下跑」：

```go fragment
state, err := store.Load(ctx, id)
if err != nil {
	return err
}
for _, call := range state.PendingCalls {
	// 普通做法：重跑没完成的调用。
	if _, err := runTool(ctx, call); err != nil {
		return err
	}
}
```

这段代码在一个前提下是对的：**「没完成」等价于「没发生」**。对纯函数成立，对副作用不成立。崩溃可能发生在工具体执行**之后**、结果写入日志**之前**，此时重跑就是重复执行。

DSH 的做法是让恢复**只描述**：

- **恢复状态（ResumeState）**：重开之后能确定的事实——未结算的调用、未关闭的轮次、未被取走的输入。
- **需要关注（NeedsAttention）**：上述任意一项非空。
- **checkpoint**：一个显式的持久边界，标记「到这为止已经落盘」。
- **不是重放（no replay）**：`Resume` 不改日志、不执行工具，只读。

## 最小实验

准备：仓库根、无需密钥、无需文件系统（用内存 Store）。观察重点是「恢复报告了什么，以及它没做什么」。

```sh
go test -count=1 -run 'Resume|Checkpoint|Restore|Cache' -v ./persistence/
```

预期全部 PASS。核心断言：

- `TestResumeReportsUnsettledWork`：日志停在 `tool/call` 之后 → `OpenTurn=true`、`UnsettledCalls=["call-1"]`、`NeedsAttention()==true`；**并且**恢复后事件数仍是 4 —— 恢复没有追加任何东西。
- `TestResumeReportsCleanState`：有 `tool/result`、有 `turn/end` → 三项都干净，`NeedsAttention()==false`。
- `TestResumeReportsPendingInput`：`step/end` 之后又来了一条 `user/message` → `PendingInputs=1`，且 `OpenTurn=false`（轮次确实关了）。
- `TestCheckpointRoundTrip`：`WriteCheckpoint` 返回后 `ReadCheckpoint` 读回同一个 `Seq`；没有 checkpoint 的日志返回 `ErrNoCheckpoint`。
- `TestRestoreReturnsStateAndHistory`：`Restore` 同时给出状态与派生历史，并填充缓存。

亲手观察一次「崩溃后重开」的完整序列：

```sh
go test -race -count=1 ./persistence/... ./plugins/sessionjsonl/...
```

## 先预测，再观察

**预测一**：一个会话在 `tool/call` 之后被杀掉。重开后把未结算调用**自动重跑**，会有什么可观察的后果？

观察：`TestResumeReportsUnsettledWork`。结果：本实现**不重跑**，只把 `call-1` 列进 `UnsettledCalls`。原因在 `persistence/resume.go` 的 `analyze`：它只读事件类型序列，从不调用工具。要重跑必须由调用方显式决定，而调用方通常需要更多信息（比如该工具是否幂等）——所以把判断留给它是正确的默认。涉及副作用不确定性的策略必须显式写出，这条与 [M6 细化方案](../10-plans/dsh-go-replication/stages/m06-persistence.md)的「崩溃前工具副作用已发生但结果未结算，恢复策略不自动重执行」一致。

**预测二**：`analyze` 判定「输入是否被取走」用的是什么信号？

结果：用 `step/start`。日志的形状是「先把一批输入记成 `user/message`，再由一个 step 取走」，因此 `step/start` 之前记录的输入算已消费、之后记录的算待处理。这是 `analyze` 里 `claimed` map 的全部逻辑，`TestResumeReportsPendingInput` 用「`step/end` 之后又来一条输入」验证了这个边界。

**预测三**：为什么 checkpoint 记成一条**日志事件**，而不是一个单独的 `.checkpoint` 文件？

结果：`WriteCheckpoint` 走 `Handle.Append` + `Flush`。理由是一致性——单独的文件可能丢失、也可能与它描述的日志不同步，而记在日志里的边界天然与日志一起被截断、修复和重开。注意它复用了日志词汇里已有的边界类型 `session/end-seed`（见 `CheckpointEventType`）：日志**拒绝未知的必需事件**，所以发明一个新名字会让所有没学过的读者拒绝打开这份日志。这是一个刻意的取舍：语义上「checkpoint」与「seed 边界」不完全等同，代价落在可读性上，因此类型名与 payload 都在代码里写明了。

**预测四**：`Restore` 为什么不把「打开、恢复、派生历史」拆成三个由调用方自己串的步骤？

结果：因为它必须避免一个具体的错误——调用方拿到空视图就往下跑。`Restore` 在同一个函数里返回状态与历史，并在失败时不返回部分结果；`TestRestorePropagatesStorageFailure` 断言存储失败会原样冒出来，而不是变成「空会话」。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 重开会话后的恢复：读回日志、确定待处理状态、checkpoint/`whenIdle` 边界；恢复不重放已发生的副作用；派生缓存可从日志重建 |
| TypeScript 入口 | 固定提交 `00102833` 的 `packages/session/session-persistence/src/handle.ts`（重开与读取契约）与 `packages/session/session-persistence-jsonl/src/worker.ts`；checkpoint 策略见 `packages/session/session-checkpoint-policy` |
| Go 入口 | [persistence.Resume](../../persistence/resume.go)、[persistence.ResumeState](../../persistence/resume.go)、[persistence.WriteCheckpoint/ReadCheckpoint](../../persistence/resume.go)、[persistence.Restore](../../persistence/cache.go)；测试见 [resume_test.go](../../persistence/resume_test.go) |
| 保持的行为 | 重开得到相同历史与待处理状态；未结算调用被列出而不被重跑；未关闭轮次被标记；未取走的输入被计数；checkpoint 落盘后才返回 |
| Go 表达 | `ResumeState` 值类型承载结论；`analyze` 只看事件类型序列；`WriteCheckpoint` 复用 `session/end-seed` 并在写后 `Flush`；`Restore` 同时返回状态与历史 |
| 不等价边界 | 参考有完整的 checkpoint**策略**包（何时该 checkpoint、`whenIdle` 语义），本阶段只实现「写一个边界并读回」，**没有实现自动 checkpoint 策略**；参考的 generation 迁移与 catalog 在本阶段未实现；`ResumeState` 是本项目自己的形状，不是参考类型的直译 |

## 一次操作的时序与所有权

恢复的参与者：调用方、`Store`、`Handle`、`Cache`。

```text
调用方          Store            Handle            Cache
  |--------------|-----------------|-----------------|
  |-- Restore -->|                 |                 |
  |              |-- Open(read) -->|                 |
  |              |<-- Handle ------|                 |
  |              |-- Resume ------>|-- Read(0) ----->|（不涉及）
  |              |                 |<-- events ------|
  |              |<-- ResumeState（未结算/未关轮次/待处理输入）
  |              |-- Messages(events) ------------->| 未命中则重建
  |              |<-- messages -------------------- |
  |<-- 状态 + 历史 ----------------|                 |
  |              |-- Close(handle)（defer）          |
```

所有权要点：

- **谁持有状态**：`Handle` 在 `Restore` 期间临时持有；`Cache` 持有派生视图；日志本身是权威。
- **何时发布结果**：`Restore` 在状态与历史**都**成功后才返回，失败不返回部分结果。
- **取消传播**：`context` 贯穿 `Open`/`Read`，在真正读盘之前生效。
- **谁释放什么**：`Restore` 用 defer 关闭自己的句柄；`Cache` 由调用方持有并可随时 `Discard`。
- **恢复不做的事**：不 `Append`、不 `Flush`、不执行工具。这三条都由测试断言。

## 失败与定位

| 现象 | 原因 | 定位方式 |
| --- | --- | --- |
| 重开后工具被重复执行 | 把「未结算」当成「未发生」 | 看 `Resume` 是否只读；`TestResumeReportsUnsettledWork` 断言事件数不变 |
| 待处理输入被丢掉 | 只用 `turn/end` 判断，忽略了 step 边界 | `analyze` 的 `claimed` 逻辑；`TestResumeReportsPendingInput` |
| checkpoint 写入了却读不回 | 只 `Append` 没 `Flush` | `WriteCheckpoint` 在 append 后显式 Flush；`TestCheckpointRoundTrip` |
| 日志因未知事件类型拒绝打开 | checkpoint 用了新造的事件类型 | `CheckpointEventType` 必须是日志词汇里的类型；见 `session/registry.go` |
| 存储故障被当成空会话 | 错误被吞掉后返回零值 | `TestRestorePropagatesStorageFailure` 断言错误原样冒泡 |

## 回到源码与下一步

- 恢复与 checkpoint：[persistence/resume.go](../../persistence/resume.go)
- 恢复与缓存的组合：[persistence/cache.go](../../persistence/cache.go)
- 被恢复的那份日志：[session/README.md](../../session/README.md)
- 轮次事件顺序：[docs/agentloop/turns.md](../agentloop/turns.md)
- 下一步读：[派生缓存](cache.md)
