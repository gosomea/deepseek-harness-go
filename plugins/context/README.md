---
description: "把「模型看到什么」记录成日志事实，再由事实渲染出可重建的请求。"
kind: "package-library"
---

# plugins/context

## 概述

这个包让「模型看到的上下文」成为**可重建的事实**，而不是插件运行顺序的副产品。每条进入请求的内容都先作为日志事实写入 Session，渲染是这些事实的纯函数：同一份日志加同一版代码，渲染出同一份请求。

读者需要 Go 的结构体与排序基础。当前实现范围是 M8.1 的贡献记录与渲染；设置与 preset、技能、附件、预算与压缩属后续切片。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Contribution` 描述一条被记录的贡献：`ID`、`Kind`、`Source`、`Text` 或 `Reference`、时间与可选 `Scopes`。
- `Set` 拥有贡献集合，拒绝重复 `ID`，并提供 `Ordered(scope)`（先按 scope 过滤，再按来源顺序与插入顺序排序）。
- `Render` 是纯函数：把有序贡献渲染成文本与引用清单，并逐条给出被引用的 `ID`。
- `Recorder` 把贡献先写入日志再返回集合，使「未记录」的内容无法到达渲染。

三类东西在包里分开维护：**日志事实**（`Contribution`）、**渲染**（`Render` 的输出）、**请求瞬时访问路径**（`Reference`，只在渲染时转成一行引用，内容不在渲染期读取）。

## 使用

```go fragment
log := /* 任何满足 context.MemoryLog 的记录面 */
recorder := context.NewRecorder(log)
set, err := recorder.Record(
	context.Contribution{
		ID: "base", Kind: context.KindInstruction, Source: context.SourceBase,
		Text: "You are a harness.",
	},
	context.Contribution{
		ID: "spec", Kind: context.KindReference, Source: context.SourceEnvironment,
		Reference: &context.Reference{Path: "docs/context/contributions.md"},
	},
)
request, err := context.Render(set, "")
```

`Render` 返回的 `Request.CitedIDs` 与文本行一一对应；把引用写成纯文本而不是读取文件内容，是为了让渲染不依赖渲染时刻的磁盘状态。

## 理解实现

- **来源顺序是写死的事实**：`renderOrder` 是切片而不是 map。用 map 会让顺序取决于遍历运气，而「两次渲染一致」正是本包要保证的性质。
- **重复 ID 被拒绝而不是覆盖**：静默覆盖会让「模型到底看到了哪一条」取决于调用顺序。
- **空 `Scopes` 表示对所有 Agent 可见**：把「没有声明 scope」读成「哪都不可见」会静默丢掉指导内容。
- **引用不带内联正文**：`Reference` 的校验拒绝同时携带 `Text`。带正文会让请求的内容取决于文件何时被读，而这正是要消除的不确定性。
- **未知 kind 响亮失败**：`Render` 对没有渲染规则的 kind 返回 `ErrNotRecorded`。静默跳过会让内容从请求里消失且没有诊断。
- **先记录再渲染是构造保证**：`Recorder.Record` 只把日志接受的贡献放进返回的集合，所以 `Render` 不可能引用日志从未见过的东西。

## 进一步阅读

- [贡献与可重建请求](../../docs/context/contributions.md)：日志事实、渲染与请求访问路径三者为何要分开。
- [提示词装配](../../docs/systemprompt/assembly.md)：片段如何装配成模型请求的另一半。
- [Session 事件日志](../../session/README.md)：本包写入的记录面归它所有。

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/context/...
```

覆盖：渲染的纯函数性、逐行可引用、重复 ID 拒绝、scope 过滤与空 scope 语义、引用拒绝内联正文、未知 kind 响亮失败、日志拒绝时集合不建立，以及 `Rebuild` 的一致性检查。

## 限制

- **不读取引用内容**。`Reference` 只渲染成引用行；把路径解析成内容属消费方与后续切片。
- **不写 Session 日志**。本包只依赖 `MemoryLog` 这个小接口，真正的日志归 `session` 所有。
- **不含预算与压缩**。token 计量、spill 与 compaction 分别属 M8.5 与 M8.6。
- **不处理技能与附件**。技能发现属 M8.3，附件身份与不可变存储属 M8.4。
