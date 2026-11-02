---
description: "进程内 subagent Provider：通过 agent.Factory 创建子 Agent，记录谱系与继承切点。"
kind: "package-library"
---

# plugins/subagent

## 概述

这个包提供**进程内 subagent Provider**：用 `agent.Factory` 在同进程创建子 Agent，记录父子谱系与 fork 继承切点。它把「子 Agent 怎样运行」从 `agent` 包里隔离出来——seam 说明调用方需要什么，这个包是其中一种满足方式。

fork 被表达为**继承事实**而非重放：Provider 记录该 fork 继承到父事件序列的某处，而不是把父会话复制成子事实，因此两份日志各自可解释。

读者需要 Go 接口与 `agent` 包基础。seam 见 [capability/subagent](../../capability/subagent/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Provider` 实现 `subagent.Provider`；声明 spawn／fork／continue 三项能力。
- `Spawn` 校验父存在后经 `agent.Factory.Create` 创建子，并记录 `Lineage`。
- `RegisterRoot` 登记根 Agent，使子能命名父。
- `LineageOf`／`Children` 回答「子从哪来、有哪些」；`DisposeChild` 释放一个子。

## 使用

下面登记根 Agent 并 spawn 一个子，读出其谱系。

```go fragment
provider, _ := subagent.New(subagent.Options{Factory: factory})
_ = provider.RegisterRoot(agent.ID("root"))
owner, err := provider.Spawn(ctx, subagent.Spec{Parent: agent.ID("root"), Mode: subagent.ModeSpawn})
```

## 理解实现

- **父必须已知**：未登记的父使 fork 无法记录继承，因此 `Spawn` 以 `ErrParentMissing` 拒绝。
- **谱系是记录的事实**：`Lineage` 由写入时记录，不从时序推断。
- **创建失败无孤儿**：Factory.Create 事务化，失败不发布半成品。

## 进一步阅读

- [子代理与长任务](../../docs/tasks/subagents.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/subagent/...
```

覆盖：无父拒绝、spawn／fork 谱系、RegisterRoot、Children、DisposeChild、无 Factory 拒绝。

## 限制

- **进程内**。外部产品 Provider 属 M10／M12。
- **不负责子 Agent 的轮次驱动**。子 Agent 由 `agentloop` 驱动，本包只创建与记录。
