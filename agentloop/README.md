---
description: "默认 Agent 驱动：作用域内创建、轮次与步骤执行、inbox、取消与关闭。"
kind: "package-library"
---

# agentloop

## 概述

这个包是默认的 Agent 驱动。它实现 `agent.Factory`，把 `agentloop.Config` 里的协作者（模型 Provider、session 日志、工具管线、提示词来源）组织成一次可运行的轮次：**turn → step → 请求 → 结算 → 工具批 → 下一步**。

它有意**不认识任何具体提供方**：模型是 `llm.Provider`、工具是 `tools.Pipeline`、日志是 `session.Log`，全部由调用方注入。因此同一套循环既能跑 fake Provider 的确定性测试，也能在后续阶段接真实端点，而不需要任何分支。契约里的 `loop-purity` 检查断言它不出现供应商、文件系统或网络专用代码。

当前实现范围是 M5.2–M5.5；真实端点、本机工具与审批属 M7，持久化属 M6。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Factory`：创建、持有并释放实例；创建是事务性的，失败不发布半成品。
- `instance`：同时实现 `agent.Handle`、`agent.Owner` 与 `agent.Canceler`，由工厂按需发放较窄的那个。
- `inbox`：FIFO 队列，**claim 是原子的**——一次 claim 取走全部待处理输入并在同一临界区清空队列，因此输入不会被领取两次。
- `Run`：开轮次、写 `turn/start`、把输入记成 user 消息、跑 step 直到自然停止或命中步数上限、在每个出口都写 `turn/end`。

## 使用

创建一个工厂，注入协作者，然后给实例投递输入并驱动一轮。工厂持有实例的 `Owner` 能力，消费者只拿到 `Handle`；`AsRunner` 取回驱动能力，驱动不等于销毁，因此查询句柄也能推动轮次。

```go fragment
factory, err := agentloop.NewFactory(agentloop.Config{
	Provider: fakeProvider,   // 任意 llm.Provider
	Log:      log,            // session.Log，时钟可替换
	Pipeline: pipeline,       // tools.Pipeline
})
owner, err := factory.Create(ctx, agent.Spec{Scope: "main", SessionID: "s-1"})
handle := owner.Handle()
err = handle.Send(ctx, agent.Input{Text: "read a.txt"})
runner, _ := agentloop.AsRunner(handle)
err = runner.RunTurn(ctx)
```

## 理解实现

- **事件形态按 surface 规则区分**：`user/message` 的载荷是消息对象本身，其余四类（`system`／`developer`／`assistant`／`tool/result`）把消息放在 `"message"` 下。用错形态会让 surface 节点派生不出任何消息，或直接被日志拒绝。
- **请求从日志重建**：`buildRequest` 每次都调用 `Log.DeriveMessages()`，而不是在内存里累加。日志是权威，因此请求天然可由日志重建。
- **工具调用先记录后执行**：`tool/call` 写在执行之前，所以进程在工具运行中崩溃也留下「模型请求过它」的事实；`tool/result` 随后写入并带上规范化结果（含拒绝）。
- **续跑必须伴随 claim**：模型自然停止后若 inbox 还有 steering，驱动**先 claim 再继续**；只检查队列而不取走会让循环空转。
- **取消以原因为准**：context 已取消时，即使流是被截断的，也报 `context.Canceled` 而不是 `ErrIncompleteStream`——症状不该盖过原因。

## 进一步阅读

- [turn 与 step](../docs/agentloop/turns.md)：事件顺序与两条时间线。
- [inbox 与追加输入](../docs/agentloop/inbox.md)：claim 语义与 steering。
- [取消与关闭](../docs/agentloop/cancellation.md)：取消、流中断与释放等待。
- [agent 包](../agent/README.md)：本包实现的公共契约。

## 验证

```sh
go test -race -count=1 ./agentloop/...
```

覆盖：两次模型请求加一次工具调用的完整轮次与事件顺序、请求可由日志重建、工具调用先于结果、queued input 恰好被领取一次、空 inbox 不开轮次、步数上限、不完整流与失败流都被记为失败、取消不派发新工具且实例存活、活动轮次中的 `Dispose` 会等待释放、`Lookup` 不授予销毁权限、并发 `Send` 与读取。

## 限制

- **没有真实模型**。全部测试使用 fake Provider 与可控时钟；真实端点、凭据与联网 smoke 属 M7。
- **没有持久化**：Session 是内存日志，磁盘格式与恢复属 M6。
- **工具是 fake**：本包不读写文件、不起进程；审批与沙箱属 M7。
- **步数上限是驱动策略**（默认 16），命中即把轮次记为失败，而不是静默截断。
- **一个 Agent 的活动是串行的**：同一实例同时只有一个活动轮次；多个实例的作用域与取消相互独立。
