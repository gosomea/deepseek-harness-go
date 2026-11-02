---
description: "Agent 触达进程外世界的五个 seam：工作区、文件系统、子进程、沙箱与审批，以及贯穿它们的规则——副作用之前先过策略。"
kind: "package-library"
---

# capability

## 概述

`capability` 是 Agent 触达**进程外世界**的接口集合：一个锚定路径的工作区、一个读写文件的文件系统、一个运行命令的子进程、一个限制范围的沙箱、一个询问副作用的审批渠道。

每个 seam 都是**消费者定义的小接口**：工具层说明自己要什么，Provider 满足它。方向很重要——正因如此，同一套工具可以在本机、远程主机或测试替身上运行，而工具本身不需要知道面对的是哪一个。

一条规则贯穿全部：**没有任何东西能在策略表态之前触及世界。** 一个无法判断动作是否被允许的能力，必须拒绝该动作，而不是假定它没问题。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- **定义宽度**：五个包各自声明自己的接口、错误与平台能力，不实现具体 Provider。
- **共享取舍**：审批的四种结果中只有 `allowed-once` 授权；沙箱只能严格更宽地升级；工作区身份不是路径。
- **拒绝而非降级**：平台能力缺失、策略无法强制、路径越界，全部是明确拒绝。

它不负责：本机实现（见 `plugins/local`）、远程 Provider（属后续里程碑）、以及任何 Agent 或工具语义。

## 使用

下面这个片段取一个世界并判读一条路径：落在世界之外的路径会被拒绝，而不是被「尽力」执行。

```go fragment
world, err := workspace.New(workspace.Options{ID: "w-1", Root: dir, ResolveSymlinks: true})
if err != nil {
	return err
}
// 一条落在世界之外的路径会被拒绝，而不是被"尽力"执行。
inside, err := world.Resolve("notes/a.txt")
if err != nil {
	return err
}
```

## 理解实现

**审批的安全答案是"什么都不做"。** 四种结果里只有 `allowed-once` 授权，`unavailable`（无人应答）、`rejected`（拒绝）、`cancelled`（取消）都不授权；headless 政策只有 `deny` 与 `ask-once`，**不存在 allow**。这样设计是为了让「忘记检查结果」的代价是拒绝，而不是放行。

**沙箱只升不降。** 阶梯为 `read-only` → `workspace-write` → `danger-full-access`，且校验在**执行时**而不是 tool schema 里——schema 是全局的，而有效模式属于单次调用。

**身份不是路径。** 工作区身份是生成值：路径会被规范化、可能经由符号链接抵达、在不同平台大小写规则不同；把会话锚在路径字符串上意味着锚点会随拼写漂移。

## 进一步阅读

- [能力与执行世界](../docs/execution/capabilities.md)：为什么读取路径与执行 cwd 必须属于同一世界。
- [审批与沙箱升级](../docs/execution/approval.md)：三种不授权结果与单向升级。
- [本机 Provider](../plugins/local/README.md)：这五个 seam 在本机上的实现。

## 验证

第一条命令编译五个包，第二条运行它们的行为测试（含工作区逃逸、审批结果、沙箱阶梯）。

```sh
go build ./capability/...
go test -race -count=1 ./capability/...
```

## 限制

- **只定义接口**：本包没有可运行的 Provider。
- **不实现真实沙箱**：`subprocess` 报告平台能力，但不承诺进程隔离。
- **无远程实现**：远程 Provider 属后续里程碑。
- **不判断业务语义**：什么算"需要审批"由工具层决定。
