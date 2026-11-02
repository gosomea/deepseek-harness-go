---
description: "按显式顺序装配提示词片段与工具 schema，让同一份输入渲染出同一份请求。"
kind: "package-library"
---

# systemprompt

## 概述

这个包把“模型看到的提示词”变成一个**可重建、可比较的值**。片段带有显式 `Order`，装配是纯函数：同样的输入两次渲染成同样的字节，因此请求可以逐字比较，提示词回归测试才有意义。

读者需要 Go 的切片与排序基础。当前实现范围是 M4.5 的片段集合与装配；多段缓存与增量改写属后续阶段。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Fragment` 描述一段提示词：`ID`、`Source`、`Order`、`Text` 与可选 `Scopes`。
- `Set` 拥有片段集合，拒绝重复 `ID` 与空文本，并提供 `Ordered(scope)`（先按 scope 过滤，再按 `Order`、`ID` 排序）。
- `Assemble` 是纯函数：渲染文本并按名称排序工具 schema，schema 由已校验的 `tools.Definition` 生成。
- `Assembly.SystemMessage` 把文本转成 `llm.Message`；空文本返回错误，因为“没有指令”与“指令为空”是两件事。

## 使用

```go fragment
set := systemprompt.NewSet()
err := set.Add(systemprompt.Fragment{
	ID: "base", Source: systemprompt.SourceBase, Order: 10,
	Text: "You are a harness.",
})
assembly, err := systemprompt.Assemble(systemprompt.AssemblyInput{
	Fragments: set,
	Tools:     registry.List(),   // tools.Definition 切片
})
message, err := assembly.SystemMessage()
```

`Scope` 为空时只渲染全局片段（`Scopes` 为空的片段）。

## 理解实现

- **顺序是字段，不是插入次序**：两个独立贡献者可以约定位置而无需协调注册顺序。
- **并列按 `ID` 稳定排序**：结果永远不依赖 map 遍历或加载顺序，`Assembly.Equal` 把“同一输入同一结果”变成一次可调用断言。
- **scope 过滤先于排序**：不可见片段不会出现在结果里，而不是“被忽略”。
- **schema 与校验同源**：工具 schema 由 `tools.SchemaText` 从 `tools.Definition` 渲染，管线的参数校验用的是同一份 `InputSchema`，因此模型看到的约束就是被强制的约束。

## 进一步阅读

- [提示词装配笔记](../docs/systemprompt/assembly.md)：为什么顺序必须显式，含可运行实验。
- [从 Go 理解工具](../docs/tools/go-primer.md)：`Definition` 与 `InputSchema` 的来源。
- [tools 包](../tools/README.md)：schema 校验与准入管线。

## 验证

```sh
go test -race -count=1 ./systemprompt/...
```

覆盖：显式顺序、并列稳定排序、scope 过滤、重复与非法片段拒绝、工具 schema 与强制约束一致、空装配拒绝铸造 system 消息。

## 限制

- **未实现**参考的提示词分段缓存与增量改写；每次装配都是完整渲染。
- **未实现**技能、附件与环境描述的专门来源语义：它们通过 `Source` 标注来源，但各自的加载与裁剪属 M8。
- 工具 schema 以 JSON 文本随装配产出，但**不**解析回来用于校验；校验始终针对 `tools.InputSchema` 结构。
