# inbox 与追加输入：claim 为什么必须是原子的

## 读完要解决的问题

你已经知道一次轮次会 claim 输入。现在的问题是：**轮次进行中追加的输入**去了哪里，以及为什么“队列非空就继续”是错的。

最直接的想法是用一个 channel 收输入、循环里 `len(ch) > 0` 就再跑一步。它会崩在两处：检查队列长度**不是**取走元素，于是循环会空转；而两个 step 若各自读一次队列，同一份输入可能被处理两次，用户就会看到自己的话被回答两遍。

这一篇说明 `inbox` 的 **claim 语义**：一次 claim 取走全部待处理输入，并在同一个临界区里清空队列。

前置知识是 Go 的 `sync.Mutex` 与切片。关联行为见 [M5 执行契约](../10-plans/dsh-go-replication-m05/plans.md)。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/core/agent-loop/src/inbox.ts`。

## 从熟悉的 Go 写法出发

```go fragment
type inbox struct {
	mu      sync.Mutex
	pending []Input
}

// 错误写法：只观察，不取走。
func (b *inbox) hasWork() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending) > 0
}
```

`hasWork` 本身没有错，错在**用它来决定继续**：驱动会一遍遍看到非空、却从不把它取走，于是要么空转要么重复处理。

正确的形状是把“观察”和“取走”合并成一次操作：

```go fragment
func (b *inbox) claim() []Input {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) == 0 {
		return nil
	}
	claimed := append([]Input(nil), b.pending…)
	b.pending = b.pending[:0]
	return claimed
}
```

复制与清空在同一个临界区里完成，这正是“输入不会被领取两次”的机制。

## 最小实验

准备条件：仓库根目录，Go 1.22。

```sh
go test -count=1 -run 'TestQueuedInputIsClaimedExactlyOnce|TestSteeringDuringATurnAddsAStep' ./agentloop/...
```

观察重点：第一条断言两条排队输入恰好变成两条 user 消息（不多不少）；第二条断言**在轮次进行中**追加的输入会让同一轮多走一个 step，而不是另开一轮。

## 先预测，再观察

先写下预期：在轮次开始**之前**排两条输入，会产生两次模型请求吗？

不会。两条输入都属于**同一次 claim**，因此只产生一次请求，该请求携带两条 user 消息。这是参考实现里“claim 待处理的下步输入以及一条排队提示”的实际形状：一轮不是一条消息一轮。

要观察这个行为：

```sh
go test -count=1 -run TestPreQueuedInputsAreClaimedAsOneBatch ./agentloop/...
```

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | inbox、claim、steering、下一步输入 |
| TypeScript 入口 | `packages/core/agent-loop/src/inbox.ts`（`ReactLoopInbox`、`inboxProjectionSchema`） |
| Go 入口 | [agentloop/inbox.go](../../agentloop/inbox.go)、[agentloop/turn.go](../../agentloop/turn.go) 的 `claimInto` |
| 保持的行为 | claim 原子且清空队列；空队列是正常状态而非错误；轮次进行中的输入由下一步 claim；同一输入不会被领取两次 |
| Go 表达 | 复制与清空在同一把锁内完成；`claim` 返回切片副本，调用方无法改动队列 |
| 不等价边界 | 未实现参考的 `agent/inbox/spliced` 与 `agent/inbox/inserted` 等实时事件；本阶段只保证权威日志与 claim 语义 |

## 一次操作的时序与所有权

1. `Send` 校验输入并加锁入队；实例已销毁时返回 `ErrInputRejected`。
2. `Run` 开始时 `claim` 一次，把结果写成 user 消息并开始轮次。
3. 每个 step 结束后：若模型请求过工具，继续；否则若 inbox 非空，**先 claim 再继续**。
4. claim 把输入交给这一轮，队列随之清空，因此后续 step 不会再看到它们。

所有权：inbox 由实例拥有；调用方拥有它投递的输入副本；claim 出的切片归当轮所有。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| 轮次无限循环、最后报步数上限 | 只检查队列而不 claim | `TestSteeringDuringATurnAddsAStep` | 已修：续跑前必须 claim |
| 同一句话被回答两次 | 两次 claim 拿到同一份输入 | `TestQueuedInputIsClaimedExactlyOnce` | claim 原子性保证不会发生；检查是否自己绕过了 inbox |
| `agent: input rejected` | 实例已销毁或输入为空 | `TestSendAfterDisposalIsRefused` | 改用仍在运行的实例，或修正输入 |

## 回到源码与下一步

阅读顺序：`agentloop/inbox.go` 的 `push`／`claim` → `agentloop/turn.go` 的 `Run` 与 `claimInto`。对应测试：`agentloop/inbox_test.go`、`agentloop/defensive_test.go`。

下一步：[取消与关闭](cancellation.md)——取消一轮、保留 inbox、等待释放。
