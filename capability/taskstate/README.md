---
description: "taskstate：goal、plan 模式与 todo 列表的事件与纯投影，区分意图与消息。"
kind: "package-library"
---

# capability/taskstate

## 概述

这个包记录长期工作的持久意图：**goal、plan 模式与 todo 列表**。它们常被误当成聊天消息，这种混淆丢掉了真正重要的事：goal 跨轮次存活、plan 是一种模式而非一条消息、todo 是事件的投影而非事件本身。三者都记为事件，`Rebuild` 是事件的纯函数，因此当前意图可从日志重建。

读者需要 Go 结构体与切片基础。使用方见 [docs/tasks/goals.md](../../docs/tasks/goals.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Event`／`Kind` 表达 goal-set、goal-done、plan-enter、plan-exit、todo-set 五类事实。
- `Rebuild` 从事件序列投影出 `State`，非法事件返回错误而非跳过。
- `Append` 校验后追加，不修改输入切片。
- `State.ShouldContinue` 是唯一的续跑规则：goal 已设且未完成才继续。

## 使用

下面用两条事件投影出 goal 与 todo，并判断是否继续。

```go fragment
state, err := taskstate.Rebuild([]taskstate.Event{
    {Kind: taskstate.KindGoalSet, Text: "ship M9"},
    {Kind: taskstate.KindTodoSet, Text: "tests", Done: false},
})
if state.ShouldContinue() { /* work remains */ }
```

## 理解实现

- **投影是纯函数**：同一事件序列永远得到同一 State，当前意图因此可重建。
- **goal-done 需要 goal**：没有 goal 时 done 在 append 与 rebuild 两处都被拒绝，不变量不被绕过。
- **todo 按键更新**：同名 todo 再次 set 只更新完成度，不重复插入。

## 进一步阅读

- [目标与续跑](../../docs/tasks/goals.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./capability/taskstate/...
```

覆盖：顺序投影、非法 kind、goal-done 无 goal、todo 更新、Append 不改输入、ShouldContinue。

## 限制

- **不含工具与驱动**。面向模型的 goal/todo 工具与续跑驱动属后续。
- **不落盘**。持久化由 jobs／session 层承担。
