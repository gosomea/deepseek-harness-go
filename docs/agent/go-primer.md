# 从 Go 理解 Agent：句柄、实例与运行驱动

## 读完要解决的问题

你已经写过接口，也体会过“同一契约多种实现”的好处。现在的问题是：当一段代码说“把输入发给这个 Agent”时，它手里握着的到底是什么？

最直接的想法是给一个 `struct Agent`，上面挂 `Send` 和 `Run` 两个方法。它在单线程演示里够用。它会崩在四处：调用方**找不到**一个安全的方式说明“谁能销毁它”；扩展想替换循环时**必须**改核心类型；并发投递输入与读取状态时没有明确的同步边界；测试里无法在不启动真实循环的情况下验证“创建失败不留半个实例”。

这一篇从这个问题进入 DSH 的 **Agent 句柄（handle）、实例（instance）与运行驱动（driver）** 三个概念。前置知识是 Go 的接口、`context.Context` 与 `sync`。关联行为见 [M5 执行契约](../10-plans/dsh-go-replication-m05/plans.md)；当前实现范围是 `agent` 的公共契约与 `agentloop` 的默认驱动。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/core/agent/src/types.ts`、`packages/core/agent-loop/src/agent.ts` 与 `docs/agent-lifecycle.zh.md`。

## 从熟悉的 Go 写法出发

最直白的表示是一个结构体带全部方法：

```go fragment
type Agent struct {
	Inbox chan Input
}

func (a *Agent) Send(input Input) { a.Inbox <- input }
func (a *Agent) Stop()            { close(a.Inbox) }
func (a *Agent) State() string    { /* ... */ }
```

它清楚、能跑。边界在于**能力没有区分**：任何拿到 `*Agent` 的代码都能 `Stop`。当父 Agent 需要知道“哪些子 Agent 还在跑”时，它只能拿到完整指针——于是“查询”和“销毁”变成同一个权限。第二个边界是**驱动写死在类型里**：想换一套循环就得改这个结构体，扩展插件无法提供自己的实现。

参考实现把三者分开：公共契约（`agent`）只描述能力，默认驱动（`agent-loop`）实现它，实例同时具备多种能力但**按需发放**。

## 最小实验

准备条件：仓库根目录，Go 1.22。这个实验不需要密钥、不需要网络——全程使用 fake Provider。

```sh
go run ./examples/harness
```

[examples/harness](../../examples/harness/README.md) 用 fake Provider 跑完一次完整轮次：模型先请求一次 `read_file` 工具，工具返回内容，模型再给出最终回答。程序打印事件顺序（`turn/start`、`step/start`、`request/header`、`assistant/attempt`、`tool/call`、`tool/result`、…、`turn/end`）以及从日志重建的两次模型请求。

观察重点：**两次请求的第二个携带了工具结果**。那不是内存里累加出来的，而是驱动每次都从 `session.Log` 重新派生历史——所以“请求可由日志重建”是构造上的事实，不是额外保证。

## 先预测，再观察

先写下预期：如果 `Lookup` 按 id 找到一个 Agent，拿到的东西能不能 `Dispose` 它？

不能。`Lookup` 返回的是只转发 `Handle` 的包装类型，没有 `Dispose`。可以断言：

```go runnable=agent-capabilities
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/gosomea/deepseek-harness-go/agent"
)

// stub 是同时实现 Handle 与 Owner 的最小值，用来观察契约的形状。
type stub struct{}

func (stub) Info() agent.Info                       { return agent.Info{} }
func (stub) State() agent.State                     { return agent.StateIdle }
func (stub) Send(context.Context, agent.Input) error { return nil }
func (stub) Wait(context.Context) error              { return nil }
func (stub) Done() <-chan struct{}                   { return nil }
func (stub) Dispose(context.Context) error           { return nil }
func (stub) Handle() agent.Handle                    { return stub{} }

func main() {
	value := stub{}
	_, isHandle := any(value).(agent.Handle)
	_, isOwner := any(value).(agent.Owner)
	_, isCanceler := any(value).(agent.Canceler)
	fmt.Printf("handle=%v owner=%v canceler=%v\n", isHandle, isOwner, isCanceler)

	// 一个只实现 Handle 的值不能通过 AssertOwner 的双向检查。
	err := agent.AssertOwner(handleOnly{})
	fmt.Println("assertOwnerRejectsHandleOnly=" + fmt.Sprint(strings.Contains(fmt.Sprint(err), "does not implement Owner")))
}

type handleOnly struct{}

func (handleOnly) Info() agent.Info                       { return agent.Info{} }
func (handleOnly) State() agent.State                     { return agent.StateIdle }
func (handleOnly) Send(context.Context, agent.Input) error { return nil }
func (handleOnly) Wait(context.Context) error              { return nil }
func (handleOnly) Done() <-chan struct{}                   { return nil }
```

```text output=agent-capabilities
handle=true owner=true canceler=false
assertOwnerRejectsHandleOnly=true
```

三条能力彼此独立：一个值可以同时具备多个，但消费者只拿到它该有的那个。要恢复到正常场景，把 `stub` 的实现换成驱动提供的实例即可。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | Agent 句柄、Agent 实例、运行驱动、inbox、工厂所有权 |
| TypeScript 入口 | `packages/core/agent/src/types.ts`、`packages/core/agent-loop/src/agent.ts`、`packages/core/agent-loop/src/inbox.ts` |
| Go 入口 | [agent/handle.go](../../agent/handle.go)、[agent/factory.go](../../agent/factory.go)、[agentloop/agent.go](../../agentloop/agent.go) |
| 保持的行为 | 句柄无销毁能力；工厂拥有它创建的实例；创建失败不发布半成品；同一 Agent 的活动串行；取消保留 inbox |
| Go 表达 | 三种能力拆成 `Handle`／`Owner`／`Canceler` 三个接口，由工厂按需发放；`Lookup` 返回包装类型而非实例，使权限分离在类型上成立 |
| 不等价边界 | 未实现参考的 hook waterfall（`agent/pre-step`、`agent/request`）与模型选择；驱动不提供 `agent/inbox/*` 的细粒度实时事件，只写权威日志 |

## 一次操作的时序与所有权

1. 调用方构造 `Spec`，交给 `Registry.Create`。
2. 注册表委托已安装的 `Factory`；工厂构建实例、**先初始化成功再发布**，失败则释放并返回错误。
3. 调用方拿到 `Owner`，通过 `owner.Handle()` 投递输入；`Handle` 上没有 `Dispose`。
4. 驱动 claim inbox 中的全部输入，写 `turn/start`，把它们记为 user 消息。
5. 每个 step 从日志派生请求、写 `request/header`、发送、结算、把工具结果写回日志。
6. `Dispose` 关闭 inbox、取消活动轮次，并**等待**该轮次释放资源后才返回。

所有权要点：工厂拥有实例；调用方拥有输入；`agent` 不拥有任何运行状态。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| `agent: input rejected` | 实例已销毁，或输入为空 | `TestSendAfterDisposalIsRefused` | 投递前检查 `State`，或改用仍在运行的实例 |
| 查询到的句柄无法销毁 | 这是设计：`Lookup` 不授予销毁权限 | `TestLookupDoesNotGrantDisposal` | 用创建它的 `Owner` 销毁 |
| `agent: a turn is already running` | 同一实例并发驱动 | `TestOverlappingTurnIsRefused` | 一个 Agent 的活动是串行的；等待结算或另建实例 |
| 创建失败但好像留下了东西 | 不应当发生：失败路径会释放实例 | `TestFactoryFailurePublishesNothing` | 确认用的是本包的工厂；创建是事务性的 |

## 回到源码与下一步

阅读顺序：`agent/types.go` 的能力与状态定义 → `agent/handle.go` 的三个接口 → `agent/factory.go` 的注册表与事务性创建 → `agentloop/agent.go` 的实例与所有权。

对应测试：`agent/factory_test.go`、`agentloop/agentloop_test.go`、`agentloop/inbox_test.go`、`agentloop/cancel_test.go`。

读者应能回答：机制解决什么问题（把“能找到”和“能销毁”分开，并让驱动可替换）、谁拥有资源（工厂拥有实例，调用方拥有输入）、Go 与 DSH 的差异在哪里（无 hook waterfall，实时事件只记权威日志）。

下一步：[turn 与 step](../agentloop/turns.md)——一次轮次里到底写了哪些事件。
