---
description: "subagent seam：命名 Provider 注册表、能力声明与谱系，父 Agent 创建子 Agent 的边界。"
kind: "package-library"
---

# capability/subagent

## 概述

这个包定义 **subagent seam**：父 Agent 通过命名 Provider 创建子 Agent。它把「运行另一个 Agent」背后的三个问题分开：**谁拥有子 Agent**（不是一次工具调用，而是一个有生命周期的实例）、**子 Agent 能做什么**（Provider 声明能力而不是探测）、**子 Agent 的事实从哪来**（fork 继承前缀是继承事实，不是新造事实）。

读者需要 Go 的接口与 `context` 基础。进程内实现见 [plugins/subagent](../../plugins/subagent/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `ProviderInfo` 声明路由能力（spawn／fork／continue）；`Provider.Spawn` 创建一个子 Agent。
- `Registry` 按 id 注册多个 Provider，并在 `Spawn` 前检查深度上限与声明能力。
- `Spec.Validate` 校验模式、继承切点与深度。
- `MaxDepth`（默认 4）限制嵌套深度，防跑飞。

## 使用

下面用一个注册表创建并 fork 一个子 Agent；省略错误检查。

```go fragment
registry := subagent.NewRegistry()
_ = registry.Register(provider)                     // provider 声明能力
owner, err := registry.Spawn(ctx, "in-process", subagent.Spec{
    Parent: agent.ID("root"), Mode: subagent.ModeFork, InheritThrough: 5,
})
```

## 理解实现

- **能力先声明再调用**：fork 请求发给只声明 spawn 的 Provider 会在 `Spawn` 前被 `ErrUnsupported` 拒绝，而不是跑到深处才失败。
- **深度上限在注册表**：`Spec.Depth > maxDepth` 直接拒绝，防止无界生成。
- **注册表不拥有子 Agent**：`Close` 只关闭注册表，子 Agent 的释放由持有 `Owner` 的一方负责。

## 进一步阅读

- [子代理与长任务](../../docs/tasks/subagents.md)：生命周期树与会话谱系。

## 验证

```sh
go test -race -count=1 -timeout=240s ./capability/subagent/...
```

覆盖：Spec 校验、注册／重复注册、能力拒绝、深度上限、缺失 Provider、Close 后注册。

## 限制

- **只定义 seam 与注册表**。具体创建属 Provider。
- **不含外部产品桥接**。进程内与外部 Provider 由 M10 与 M12 展开。
