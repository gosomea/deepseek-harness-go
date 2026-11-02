---
description: "定义 Agent 的公共契约：句柄、状态、工厂 seam 与所有权分离。"
kind: "package-library"
---

# agent

## 概述

这个包定义消费者与一个 Agent 交互所需的**全部公共契约**：`Handle`（读取、投递输入、等待结算）、`State`（生命周期状态）、`Spec` 与 `Factory`（创建 seam）。它有意不包含任何驱动逻辑——循环、轮次与步骤在 `agentloop` 中实现。

这样的方向是强制的：`agent` **不得** import `agentloop`，扩展插件可以依赖 `agent` 提供自己的驱动，而不会被绑定到默认循环。该边界由契约里的 `agent-boundary` 检查断言，不靠约定。

读者需要 Go 的接口与 `context.Context` 基础。当前实现范围是 M5.1 的契约定型；真实的模型端点与本机工具属 M7。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Handle`：消费者看到的句柄，只有 `Info`／`State`／`Send`／`Wait`／`Done`。它**没有**销毁方法。
- `Owner`：销毁一个实例的能力，与 `Handle` 分开。能「找到」一个 Agent 不等于能「关掉」它。
- `Canceler`：请求取消当前工作但**不**销毁实例。
- `Spec` 与 `Factory`：创建 Agent 的输入与 seam；`Factory` 拥有它创建的实例。
- `Registry`：同一时刻只安装一个驱动，避免「哪辆车跑了这一轮」无法归属。

## 使用

```go fragment
registry := agent.NewRegistry()
if err := factory.Install(registry); err != nil {   // 默认驱动来自 agentloop
	return err
}
owner, err := registry.Create(ctx, agent.Spec{Scope: "main", SessionID: "s-1"})
```

`registry.Create` 返回 `Owner`；读取与投递走 `owner.Handle()`。创建的失败是事务性的：错误返回时没有实例被发布，也不会泄漏 Agent id。

## 理解实现

- **三种能力分开**：`Handle`、`Owner`、`Canceler` 是三个接口。工厂把 `Owner` 交给创建者、把 `Handle` 交给查询者，因此权限是类型上的事实而不是口头约定。
- **取消 ≠ 销毁**：`Cancel` 让当前轮次停止，实例仍然存活、inbox 仍然保留；`Dispose` 才结束它的生命。
- **`WaitAll`**：等待一组句柄结算，遇到 context 取消时提前返回并汇报已完成的部分。
- **`Registry.Close`**：先释放实例再清空驱动，因此并发的 `Create` 不会掉进正在拆除的工厂。

## 进一步阅读

- [从 Go 理解 Agent 轮次](../docs/agent/go-primer.md)：句柄、实例与运行驱动的区别。
- [agentloop 包](../agentloop/README.md)：默认驱动如何实现这份契约。
- [M5 执行契约](../docs/10-plans/dsh-go-replication-m05/plans.md)：本阶段的裁决与产物归属。

## 验证

```sh
go test -race -count=1 ./agent/...
```

覆盖：`Spec`／`Info`／`Input`／`State` 的校验、注册表只装一个驱动、创建失败不发布任何实例、`Close` 释放实例并拒绝后续使用、`AssertOwner` 双向断言、`WaitAll` 的错误汇总与取消短路。

## 限制

- **没有驱动逻辑**。`agent` 不知道 turn 或 step 是什么；那是 `agentloop` 的事。
- **没有持久化**。Agent 身份由 `agentloop` 铸造，跨进程恢复属 M6。
- **没有工具可见性规则**。`Spec.Scope` 只是一个作用域键；它如何决定可见工具由 scope 层与驱动决定。
