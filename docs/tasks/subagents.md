# 子代理与生命周期树

## 读完要解决的问题

父 Agent 怎样创建另一个 Agent、让它干活、观察它、并在不需要时释放它？读完后你能：解释为什么子 Agent 不是一次工具调用；用命名 Provider 注册表创建子 Agent；从谱系记录回答「这个子从哪里来、继承到哪、深度多少」。需要 Go 的接口、`context` 与错误处理基础。当前实现范围是进程内 Provider（`plugins/subagent`），外部产品 Provider 属后续阶段。

## 从熟悉的 Go 写法出发

普通 Go 里，「再跑一个 goroutine」是很自然的做法：`go work()`。但它有三个说不清的地方。第一，goroutine 没有身份，你无法从一份记录回答「这个任务是哪个父创建的」。第二，goroutine 没有能力声明，你不能在启动前知道它能不能 fork 一份已有对话。第三，goroutine 没有所有权，进程退出它就不见了，谁负责它的清理无从谈起。

DSH 的 subagent 概念把这三件事显式化：子 Agent 是一个**有身份的实例**，由**命名 Provider** 创建，Provider**声明能力**，创建时记录**谱系**。这与 goroutine 的类比在「并发执行」上成立，在「身份、能力、所有权」上不等价——后者是 harness 为了可追溯而额外引入的。

## 最小实验

在仓库根目录：

```sh
go run ./examples/subagents
```

预期 stdout 逐字保存在 [examples/subagents/expected.txt](../../examples/subagents/expected.txt)，关键两行：

```text output-file=examples/subagents/expected.txt
spawn lineage: mode=spawn inherit=0
fork lineage: mode=fork inherit=7
children: 2
provider: in-process
```

观察重点：spawn 的 `inherit=0`（没有继承），fork 的 `inherit=7`（继承到父事件 7）。这说明 fork 记录的是**继承事实**，不是复制了一份父会话。

## 先预测，再观察

先预测：把 fork 请求发给只声明 `CapabilitySpawn` 的 Provider 会怎样？写出你的预期，再对照实现——注册表在 `Spawn` 之前就用 `ErrUnsupported` 拒绝，因此该 Provider 的 `Spawn` 根本不会被调用。这与「跑到一半才失败」不同，恢复方式是不改代码，改为向声明了 fork 的 Provider 发起请求。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 命名 subagent Provider、能力声明、父子所有权与谱系 |
| TypeScript 入口 | 固定提交 `packages/subagent` |
| Go 入口 | [capability/subagent](../../capability/subagent/README.md)、[plugins/subagent](../../plugins/subagent/README.md) |
| 保持的行为 | 能力不符在执行前拒绝；父必须已知；fork 记录继承切点 |
| Go 表达 | 小接口 + 命名注册表 + 显式 `Lineage` 记录；`Owner` 释放 |
| 不等价边界 | 仅进程内 Provider；外部产品桥接属后续阶段 |

## 一次操作的时序与所有权

`Registry.Spawn` 先校验深度与能力，再调用 `Provider.Spawn`；Provider 校验父已登记，经 `agent.Factory.Create` 创建子（事务化，失败无孤儿），把 `Lineage` 与 `Owner` 记入 Provider。释放用 `DisposeChild`，只影响该子。注册表 `Close` 不释放子——子由持有 `Owner` 的一方负责。

## 失败与定位

- **`ErrParentMissing`**：父未 `RegisterRoot` 就发起 spawn／fork。修复：先登记根。
- **`ErrUnsupported`**：能力不符。修复：向声明了该能力的 Provider 发请求。
- **`ErrDepthExceeded`**：`Depth` 超过上限 4。修复：收敛委派层级。
- **`ErrDuplicateProvider`**：同名重复注册。修复：改用不同 id。

## 回到源码与下一步

先读 [capability/subagent](../../capability/subagent/README.md) 理解 seam 与能力声明，再读 [plugins/subagent](../../plugins/subagent/README.md) 看进程内实现。下一步：[目标与续跑](goals.md) 解释 goal／todo 怎样跨轮次存活；[持久任务](jobs.md) 解释 jobs/schedule 怎样在重启后保持状态。
