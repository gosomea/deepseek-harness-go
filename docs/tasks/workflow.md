# Workflow 与委派

## 读完要解决的问题

一段「先规划、再执行」的多步委派怎样被描述并交给引擎运行，而不把业务逻辑硬编码进调用方？读完后你能：把编排描述为带 meta 与 steps 的脚本；在运行前校验参数；解释为什么 fake 引擎能证明编排形状。需要 Go 的接口与 `context` 基础。当前实现是 `capability/workflow` 与 `plugins/workflow`。

## 从熟悉的 Go 写法出发

普通 Go 里，你可能会写一个 `func orchestrate(ctx) error` 顺序调用几个函数。它直接、可读，但「编排描述」和「执行方式」混在一起：你没法在没有真实引擎时先验证脚本形状，也没法替换执行实现。DSH 把脚本当**数据**：seam 校验 meta 与 args，Provider 运行它。类比在「顺序调用」上成立，在「描述与执行分离、可替换」上不等价。

## 最小实验

在仓库根目录用一段小程序（`go fragment` 示意，等价可由 `plugins/workflow` 测试复现）：

```go fragment
engine := workflow.New(workflow.Options{})
result, _ := engine.Run(ctx, seam.Request{Parent: "p", Script: seam.Script{
    ID: "two-step", Capability: "two-step-delegation", Steps: []string{"plan", "exec"},
}})
// result.Trace == ["step:plan", "step:exec"]，result.Completed == true
```

观察重点：trace 按 steps 顺序产生 `step:` 前缀项；未知能力在执行前被拒绝。

## 先预测，再观察

先预测：给引擎一个它未声明能力的脚本会怎样？写出预期再对照实现——`Run` 在跑任何步骤前返回 `ErrUnknownCapability`，因此 trace 为空。这与「跑了一半才发现」不同。恢复方式：改用声明了该能力的引擎，或改脚本能力。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 编排脚本（meta／steps）与执行引擎分离、能力声明 |
| TypeScript 入口 | 固定提交 `packages/workflow` |
| Go 入口 | [capability/workflow](../../capability/workflow/README.md)、[plugins/workflow](../../plugins/workflow/README.md) |
| 保持的行为 | 校验先于执行；未知能力拒绝；多余 arg 拒绝；parent 必需 |
| Go 表达 | 数据化 Script + 小接口 Provider + 能力声明 |
| 不等价边界 | fake 引擎；生产 PTC Provider 属 M12.4 |

## 一次操作的时序与所有权

调用方构造 `Request`（含 parent 与 args），`engine.Run` 先 `Validate`（parent、能力、args），再逐 step 执行并在每步前检查 ctx；结果 `Result` 带 trace 与 Completed。引擎不拥有子 Agent，委派的副作用属生产引擎。

## 失败与定位

- **`ErrUnknownCapability`**：引擎未声明该能力。修复：换引擎或改脚本。
- **`ErrInvalidArgs`**：缺 arg 或多 arg。修复：对齐 meta。
- **`ErrParentRequired`**：无父。修复：提供 parent。
- **`ErrInvalidScript`**：脚本结构非法。修复：补齐 id／capability／steps。

## 回到源码与下一步

读 [capability/workflow](../../capability/workflow/README.md) 与 [plugins/workflow](../../plugins/workflow/README.md)。回顾：[子代理与生命周期树](subagents.md)。
