# 目标、计划与续跑

## 读完要解决的问题

一个长期的 agent 任务怎样跨多个轮次记住「要做什么」？读完后你能：把 goal、plan 模式与 todo 列表区分为三类事件；用纯投影从日志重建当前意图；回答「什么时候应该继续跑」。需要 Go 的结构体与切片基础。当前实现是 `capability/taskstate`。

## 从熟悉的 Go 写法出发

普通 Go 里，你可能用一个 `struct{ goal string; todos []Todo }` 保存状态，并在若干地方修改它。问题出现在多处修改之后：没有一个地方能回答「当前意图是怎么来的」，也无法从两份日志对比出哪里不一致。DSH 把意图记为**事件**，状态是事件的纯函数；这与可变结构体的类比在「保存当前值」上成立，在「可重建、可对比」上不等价。

## 最小实验

在仓库根目录用一段小程序（`go fragment` 示意，等价可由 `capability/taskstate` 测试复现）：

```go fragment
state, _ := taskstate.Rebuild([]taskstate.Event{
    {Kind: taskstate.KindGoalSet, Text: "ship M9"},
    {Kind: taskstate.KindTodoSet, Text: "tests", Done: false},
})
// state.ShouldContinue() == true
```

观察重点：同一事件序列两次 `Rebuild` 结果相同；`goal-done` 在 goal 为空时被拒绝。

## 先预测，再观察

先预测：给一个没有 goal 的事件序列追加 `goal-done` 会怎样？写出预期再对照实现——`Append` 与 `Rebuild` 两处都以 `ErrInvalidEvent` 拒绝，因此不会出现「完成了不存在的目标」这种状态。恢复方式：先追加 `goal-set`。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | goal／plan／todo 作为版本化意图 |
| TypeScript 入口 | 固定提交 `packages/goal`、`packages/plan`、`packages/todo` |
| Go 入口 | [capability/taskstate](../../capability/taskstate/README.md) |
| 保持的行为 | 纯投影；goal-done 需要 goal；todo 按键更新 |
| Go 表达 | 事件切片 + 纯函数 Rebuild + 唯一续跑规则 ShouldContinue |
| 不等价边界 | 面向模型的工具与续跑驱动属后续 |

## 一次操作的时序与所有权

调用方持有事件切片，`Append` 返回新切片（不改输入）；`Rebuild` 不持有状态，是纯函数。续跑与否只由 `State.ShouldContinue` 决定（goal 已设且未完成）。持久化由 session／jobs 层承担，本包不落盘。

## 失败与定位

- **`ErrInvalidEvent`**：非法 kind、goal-set 缺文本、goal-done 无 goal。修复：核对事件种类与前置。

## 回到源码与下一步

读 [capability/taskstate](../../capability/taskstate/README.md)。下一步：[持久任务](jobs.md) 解释怎样把这些意图变成可恢复的后台任务。
