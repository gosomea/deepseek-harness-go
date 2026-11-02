---
description: "按默认值、作用域层、会话覆盖三段解析配置，并回报每个值来自哪一层。"
kind: "package-library"
---

# plugins/settings

## 概述

这个包回答一个容易被含混过去的问题：**某个 Agent 到底用了哪个值？** 答案可能来自三处——schema 默认值、某个作用域层的覆盖、会话覆盖。如果合并顺序是隐含的，那么"取错了值"和"取对了值"从外面看是一样的。

这个包把解析写成显式的三层：默认值 → 从根到目标作用域的逐层覆盖 → 会话覆盖。每个结果都带上来源，因此调用方不仅能问"值是多少"，还能问"是哪一层给的"。

读者需要 Go 的结构体与 map 基础。当前实现范围是 M8.2 的配置解析与作用域隔离；preset/persona 注册见 [plugins/preset](../preset/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Schema` 拥有键声明（`Declaration`：键、类型、默认值），拒绝重复键与**与自身类型冲突的默认值**。
- `Layer` 持有某个作用域的覆盖；`Registry` 持有 layers、父子关系与会话覆盖。
- `Registry.Resolve(scope, session)` 按默认值 → 作用域链 → 会话的顺序解析，返回带 `Source` 的结果。
- `ScopePath(root, parents, target)` 把作用域链构造成可打印的值，拒绝断链与环。

## 使用

先声明键，再建注册表，然后写入各层覆盖。下面的片段假定 `err` 会被检查；实际调用时每一步都可能失败，尤其是 `Declare`（重复键、坏默认值）与 `SetParent`（改指父亲）。

```go fragment
schema := settings.NewSchema()
err := schema.Declare(settings.Declaration{
	Key: "model", Kind: settings.KindString, Default: settings.Value{String: "base-model"},
})
registry := settings.NewRegistry(schema, "root")
err = registry.SetParent("child", "root")
registry.Layer("root").Set("model", settings.Value{String: "root-model"})
registry.Layer("child").Set("model", settings.Value{String: "child-model"})
registry.Session("s-1")["model"] = settings.Value{String: "session-model"}

effective, err := registry.Effective("child", "s-1", "model")   // Source == settings.SourceSession
```

## 理解实现

- **作用域链是显式参数**：`Resolve` 接收从根到目标的链，而不是顺着 parent 指针爬。这样本包不需要知道 `scope` 包的所有权规则，同时让"解析顺序"成为调用方能打印的值。
- **近的作用域胜出**：链是从根向叶遍历并逐层覆盖，所以叶子上的值最后写入并生效。
- **会话覆盖最后应用**：显式的每会话选择总是压过作用域默认，与作用域深浅无关。
- **未知键被拒绝而不是回落默认值**：一个拼错的键若静默取默认值，会让配置错误看起来像正常工作。
- **类型不匹配被拒绝**：`verbosity` 声明为 `int` 时，字符串值不会被接受。
- **schema 拒绝自己的坏默认值**：若某个键的默认值违反它自己声明的类型，之后所有校验信息都不可信。
- **`SetParent` 拒绝改指父亲**：一个作用域有两个父亲会让解析链有歧义，而歧义要到很晚才以"值不对"的形式暴露。

## 进一步阅读

- [设置的可见性与作用域](../../docs/context/settings-scope.md)：为什么默认值、作用域覆盖与会话覆盖必须分开记账。

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/settings/...
```

覆盖：默认值、作用域覆盖、近作用域胜出、会话覆盖压过所有作用域、兄弟作用域不串用、作用域链构造与断链/环拒绝、未知键与类型错误拒绝、重复键与坏默认值拒绝，以及解析结果按键排序。

## 限制

- **不含 preset/persona**。命名配置包与 persona 文本归 [plugins/preset](../preset/README.md)。
- **不写 Session 日志**。本包只解析值，不负责把这些值记录成日志事实。
- **不持久化**。覆盖只存在于内存中的注册表，落盘属 `persistence`。
- **键类型仅 string/bool/int**。更复杂的值类型需要先扩展 `Kind`。
