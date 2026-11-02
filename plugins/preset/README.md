---
description: "注册命名配置包，拒绝未知名称，并回报每个值由哪个包贡献。"
kind: "package-library"
---

# plugins/preset

## 概述

这个包回答 Agent 启动时总会遇到的三个问题：**要的是哪个命名包**、**它实际贡献了什么**、**名字没注册时怎么办**。最后一个如果靠"静默用空配置启动"来回答，就会把配置错误变成一个安静的 Agent。

因此注册表拒绝未知名称，按声明的顺序记录每个包的贡献，并回报每个键的来源。读者需要 Go 的结构体与切片基础。当前实现范围是 M8.2 的 preset 注册与合并；配置解析见 [plugins/settings](../settings/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Preset` 描述一个命名包：名称、说明、贡献列表与可选 persona 文本。
- `Registry` 拥有包集合，拒绝重复名称与含重复键或无键贡献的包。
- `Apply(name)` 解析一个包，按 key 排序返回贡献并带上包名；未知名称返回 `ErrUnknownPreset`。
- `Merge(names...)` 按顺序合并，后面的包胜出；`Owners(names...)` 回报每个键由哪个包贡献。

## 使用

注册包之后按名称使用。`Register` 会拒绝重复名称与含重复键的包，`Merge` 的后一个包胜出；下面的片段省略了逐处错误检查。

```go fragment
registry := preset.NewRegistry()
err := registry.Register(preset.Preset{
	Name: "default", Description: "the base bundle",
	Settings: []preset.Contribution{{Key: "model", Value: "base-model"}},
	Persona:  "You are a careful assistant.",
})
err = registry.Register(preset.Preset{
	Name: "terse", Settings: []preset.Contribution{{Key: "verbosity", Value: "0"}},
})

merged, err := registry.Merge("default", "terse")   // later bundle wins
owners, err := registry.Owners("default", "terse")  // owners["verbosity"] == "terse"
```

## 理解实现

- **未知名称是错误，不是空结果**：静默用空配置启动会把配置错误伪装成能用的 Agent。
- **重复名称被拒绝而不是覆盖**：静默替换会让"哪个包生效"取决于注册顺序。
- **`Apply` 拷贝贡献切片**：调用方改返回值不会污染注册表。
- **合并保留来源**：两个包贡献同一个键是合法的（这正是层叠的含义），但 `Owners` 记录了胜出者。
- **多包合并时 `Name` 为空**：合并了多个包却报一个名字会误导；调用方用 `Owners` 看拆分。

## 进一步阅读

- [设置的可见性与作用域](../../docs/context/settings-scope.md)：命名包与作用域层如何共同决定 Agent 看到的配置。

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/preset/...
```

覆盖：解析已知包、未知名称与 nil 注册表拒绝、重复名称与非法包拒绝、合并的后包胜出与 owner 记录、单包合并保留名称、空合并、返回值不可反向修改注册表，以及名称顺序。

## 限制

- **不解析值类型**。本包的贡献是字符串键值；带类型的校验归 [plugins/settings](../settings/README.md)。
- **不含 persona 的模型请求装配**。persona 文本如何进入请求属 `plugins/context`。
- **不持久化**。注册只在内存中。
