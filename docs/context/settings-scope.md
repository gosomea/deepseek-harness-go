# 设置的可见性与作用域

## 读完要解决的问题

同一个 Agent 在不同场合要表现不同：根作用域用大模型，某个子作用域用便宜模型，而这一次会话临时改成另一个。于是"这个 Agent 用的到底是哪个值"有了三个可能的答案——schema 默认值、作用域覆盖、会话覆盖。

如果合并顺序是隐含的，那么**取错值和取对值从外面看起来一模一样**：都只是一个字符串。本包要解决的就是让"哪一层给的"成为可查询的事实。

需要的 Go 知识：结构体、map、切片排序。不需要 Cordis 或 TypeScript 基础。当前实现范围是 M8.2 的解析与作用域隔离；命名包与 persona 见 `plugins/preset`。完成实验后你能做到：**预测三层覆盖的结果，并用 `Source` 字段解释为什么**。

## 从熟悉的 Go 写法出发

普通做法是读配置 map，一层层覆盖：`for k, v := range overrides { cfg[k] = v }`。这能工作，但有两个边界：

1. **顺序由 map 决定**。如果"根覆盖"和"子覆盖"放在同一个 map 里合并，谁最后写入取决于遍历顺序。
2. **没有来源**。合并完只剩最终值，答不出"这个值是默认的还是被覆盖过"。

于是引入三个概念：

- **声明（Declaration）**：一个键的类型与默认值，由 `Schema` 拥有。
- **层（Layer）**：某个作用域的覆盖集合，由 `Registry` 按作用域持有。
- **解析结果（Resolved）**：最终值加上 `Source`（`default`/`scope`/`session`），作用域层还带上来源作用域名。

类比之后要说清哪里不等价：这与"多层 `map` 覆盖"不同。这里的合并顺序是**写在 `Resolve` 里的**（默认 → 根到目标的作用域链 → 会话），不是由遍历顺序决定。

## 最小实验

准备条件：仓库根目录，Go 1.22+。不需要网络与密钥。

```go runnable=settings-scope
package main

import (
	"fmt"

	"github.com/gosomea/deepseek-harness-go/plugins/settings"
)

func main() {
	schema := settings.NewSchema()
	for _, d := range []settings.Declaration{
		{Key: "model", Kind: settings.KindString, Default: settings.Value{String: "base-model"}},
		{Key: "verbosity", Kind: settings.KindInt, Default: settings.Value{Int: 1}},
	} {
		if err := schema.Declare(d); err != nil {
			panic(err)
		}
	}

	registry := settings.NewRegistry(schema, "root")
	if err := registry.SetParent("child", "root"); err != nil {
		panic(err)
	}
	registry.Layer("root").Set("model", settings.Value{String: "root-model"})
	registry.Layer("child").Set("model", settings.Value{String: "child-model"})

	for _, scope := range []string{"root", "child"} {
		model, err := registry.Effective(scope, "", "model")
		if err != nil {
			panic(err)
		}
		fmt.Printf("scope=%-5s model=%-11s source=%s via=%s\n", scope, model.Value.String, model.Source, model.Scope)
	}

	// A session override beats every scope, however deep.
	registry.Session("s-1")["model"] = settings.Value{String: "session-model"}
	model, err := registry.Effective("child", "s-1", "model")
	if err != nil {
		panic(err)
	}
	fmt.Printf("session override: model=%s source=%s\n", model.Value.String, model.Source)

	// An unset key keeps its default, and says so.
	verbosity, err := registry.Effective("child", "s-1", "verbosity")
	if err != nil {
		panic(err)
	}
	fmt.Printf("untouched: verbosity=%d source=%s\n", verbosity.Value.Int, verbosity.Source)
}
```

```text output=settings-scope
scope=root  model=root-model  source=scope via=root
scope=child model=child-model source=scope via=child
session override: model=session-model source=session
untouched: verbosity=1 source=default
```

观察重点：**每一行都带 `source`**。`root` 看自己时不会看到 `child` 的值（覆盖不向上泄漏），`child` 看到的是自己的值（近的胜出），而会话覆盖压过了作用域。

## 先预测，再观察

**先写下你的预期**：在 `root` 上设 `verbosity=5`，然后在 `child` 上**不设任何值**，问 `child` 的 `verbosity` 是多少、`source` 是什么？

观察方法：加一行 `registry.Layer("root").Set("verbosity", ...)` 后重跑。

结果：`child` 看到 `verbosity=5`，`source=scope`，`via=root`。原因：解析沿作用域链从根到叶遍历，`child` 自己没有覆盖，于是根上的值保留下来——**来源记的是"哪一层的值最终生效"，不是"直接命中哪一层"**。

换个改法：把 `verbosity` 的键名故意写错成 `verbositiy`。此时 `Resolve` 返回 `settings: unknown key`，而不是静默取默认值。恢复：删掉那行即可。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | settings、preset/persona 与 Agent 独立能力配置；默认值／会话覆盖／作用域不串用 |
| TypeScript 入口 | 固定提交 `00102833` 的 [packages/settings](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/settings) 与 [packages/preset](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/preset) |
| Go 入口 | [settings.go](../../plugins/settings/settings.go) 的 `Schema`/`Resolve`；[scope.go](../../plugins/settings/scope.go) 的 `Registry`/`ScopePath`；[preset.go](../../plugins/preset/preset.go) 的 `Registry`/`Merge`；测试见 [settings_test.go](../../plugins/settings/settings_test.go) |
| 保持的行为 | 默认 → 作用域链（根到叶）→ 会话的优先级；未知键与类型错误被拒绝；每个结果带 `Source` |
| Go 表达 | `Resolve` 接收显式作用域链而非顺 parent 爬，使本包不依赖 `scope` 包的所有权规则；`Value` 用带标签联合而非 `any`，让类型错误在边界可见 |
| 不等价边界 | 本包**不写 Session 日志**、**不持久化**、**键类型仅 string/bool/int**；preset 不含 persona 的请求装配 |

## 一次操作的时序与所有权

1. 调用方构造 `Schema` 并 `Declare` 各键——重复键与坏默认值在这里被拒。
2. `Registry.SetParent` 建立父子关系；重复改指被拒。
3. `Layer(scope).Set` 写入覆盖；`Session(id)[key]` 写入会话覆盖。
4. `Effective(scope, session, key)` 先 `ScopePath` 构造链（断链与环在此被拒），再调 `Resolve`。
5. `Resolve` 先扫全部覆盖找出未知键，再逐键按三层解析，最后按键排序返回。
6. 返回的 `[]Resolved` 是普通值，没有后台 goroutine、没有需要释放的句柄。

所有权要点：**`Schema` 拥有键定义，`Registry` 拥有 layers 与父子关系**。`Resolve` 是纯函数，它不持有任何东西，所以同样的输入两次解析必然一致。

## 失败与定位

**失败一：未知键。** 症状是 `settings: unknown key: "verbositiy" in scope "child"`。原因：键名拼错或 schema 未声明。诊断入口：`Resolve` 的未知键预扫。恢复：改正键名或补声明——静默取默认值会让配置错误看起来像正常工作。

**失败二：类型不匹配。** 症状是 `settings: invalid value: "verbosity" in scope "root": an int key carries only int content`。原因：给 `int` 键赋了字符串。诊断入口：`validateValue`。恢复：按声明类型赋值。

**失败三：作用域断链或成环。** 症状是 `settings: invalid schema: scope "orphan" does not descend from root "root"` 或 `... is cyclic`。原因：`parents` 里缺一条边，或父子互相指向。诊断入口：`ScopePath`。恢复：补齐 parent 链——静默接受会让解析针对一个调用方没问过的作用域。

一个容易混淆的边界：**未知键与类型错误都在解析前被发现，但报告位置不同**。未知键会在扫覆盖时就报错，不管该键是否会被覆盖；类型错误则在具体那一层被应用时报错。

## 回到源码与下一步

建议阅读顺序：

1. [settings.go](../../plugins/settings/settings.go) 的 `validateValue` 与 `Schema.Declare` —— 先看值成立的条件。
2. 同文件的 `Resolve` —— 再看三层顺序与未知键预扫。
3. [scope.go](../../plugins/settings/scope.go) 的 `ScopePath` 与 `Registry.Effective` —— 最后看作用域链如何构造。
4. [preset.go](../../plugins/preset/preset.go) 的 `Merge` 与 `Owners` —— 命名包如何层叠并保留来源。

对应测试名：`TestSessionOverrideBeatsEveryScope`、`TestNearerScopeOverridesTheFartherOne`、`TestScopeOverrideDoesNotLeakToASibling`、`TestUnknownKeyIsRefusedNotDefaulted`、`TestMergeLetsTheLaterBundleWinAndRecordsTheOwner`。

读完应能回答：**机制解决什么问题**（让"这个 Agent 用了哪个值、谁给的"可查询）；**谁拥有资源**（`Schema` 拥有键定义，`Registry` 拥有层与父子关系，`Resolve` 无状态）；**Go 与 DSH 的差异在哪**（本包只到解析与注册，不写日志、不落盘、键类型有限）。

可运行实验见 [examples/context](../../examples/context/README.md)。下一篇：技能清单与实际加载的 token 差异（`docs/context/skills.md`，属 M8.3）。
