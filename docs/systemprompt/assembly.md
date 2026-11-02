# 提示词装配：为什么顺序必须是显式的

## 读完要解决的问题

你已经会用切片拼字符串。现在的问题是：当系统提示由多个来源拼成时，**谁决定它们的顺序**？

最直接的想法是按注册先后拼接。它会崩在两处：两个独立注册的贡献者无法在不互相协调的情况下约定位置；而一次注册顺序的变化（插件加载顺序、map 遍历）会让同一份输入渲染出不同的提示词，于是“请求可比较”这条性质消失。

这一篇说明 `systemprompt` 如何用显式 `Order` 与确定的并列规则，让装配成为**纯函数**。关联行为见 [M4 执行契约](../10-plans/dsh-go-replication-m04/plans.md)。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/core/system-prompt/src/index.ts`。

## 从熟悉的 Go 写法出发

```go fragment
var fragments []string
func Add(text string) { fragments = append(fragments, text) }
```

问题在于顺序 = 调用顺序。参照实现用显式顺序字段，并在并列时用稳定键（id）打破平局，使结果与遍历顺序无关。这正是 Go 侧 `Set.Ordered(scope)` 做的事：先按 `Scope` 过滤，再按 `Order` 升序、`ID` 升序排序。

## 最小实验

```go runnable=systemprompt-order
package main

import (
	"fmt"

	"github.com/gosomea/deepseek-harness-go/systemprompt"
)

func main() {
	set := systemprompt.NewSet()
	// Registered out of order on purpose: order is a field, not the call sequence.
	_ = set.Add(systemprompt.Fragment{ID: "c", Source: systemprompt.SourceBase, Order: 30, Text: "third"})
	_ = set.Add(systemprompt.Fragment{ID: "a", Source: systemprompt.SourceBase, Order: 10, Text: "first"})
	_ = set.Add(systemprompt.Fragment{ID: "b", Source: systemprompt.SourceBase, Order: 20, Text: "second"})

	assembly, _ := systemprompt.Assemble(systemprompt.AssemblyInput{Fragments: set})
	fmt.Println(assembly.Text)

	// Equal orders are broken by id, so the result never depends on insertion.
	tie := systemprompt.NewSet()
	_ = tie.Add(systemprompt.Fragment{ID: "zulu", Source: systemprompt.SourceBase, Order: 5, Text: "zulu"})
	_ = tie.Add(systemprompt.Fragment{ID: "alpha", Source: systemprompt.SourceBase, Order: 5, Text: "alpha"})
	tied, _ := systemprompt.Assemble(systemprompt.AssemblyInput{Fragments: tie})
	fmt.Println(tied.Text)
}
```

```text output=systemprompt-order
first

second

third
alpha

zulu
```

## 先预测，再观察

先写下预期：给一个片段设置 `Scopes = ["agent-a"]`，在 `Scope="agent-b"` 的装配里它会出现吗？

不会。过滤发生在排序**之前**，不在结果里体现为“被忽略”，而是根本不出现。可用 `TestScopeFilteringSelectsOnlyVisibleFragments` 验证。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 有序片段集合 → 渲染文本；工具 schema 随装配一起产出 |
| TypeScript 入口 | `packages/core/system-prompt/src/index.ts` |
| Go 入口 | [systemprompt/fragment.go](../../systemprompt/fragment.go)、[systemprompt/assembly.go](../../systemprompt/assembly.go) |
| 保持的行为 | 顺序由显式 `Order` 决定；并列按 `ID` 稳定排序；范围过滤先于排序；重复 `ID` 被拒绝；空装配不产生 system 消息 |
| Go 表达 | `Set` 拥有自己的切片与索引，拒绝重复 id；`Assemble` 是纯函数，`Assembly.Equal` 让“同一输入渲染同一结果”成为一次可调用断言 |
| 不等价边界 | 工具 schema 以 JSON 文本随装配产出，但**不解析回来**用于校验——管线校验的是 `tools.InputSchema` 结构，因此两者不会各说各话；未实现参考的多段缓存与增量改写 |

## 一次操作的时序与所有权

1. 贡献者把 `Fragment`（含 `ID`、`Source`、`Order`、`Text`、可选 `Scopes`）加入 `Set`；重复 id 或空文本被拒绝。
2. `Assemble` 按 scope 过滤、排序、用分隔符连接；没有片段则文本为空。
3. 工具定义同时传入，由 `tools.SchemaText` 渲染为模型可见 schema；无效定义在此报错。
4. 装配结果的 `Text` 用 `SystemMessage` 转成 `llm.Message`；**空文本返回错误**，因为“没有指令”与“指令为空”是两件事。

所有权：`Set` 由贡献者注入方拥有；`Assembly` 是值，可安全比较与传阅；`SystemMessage` 铸造的消息归 session 词汇所有。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| `systemprompt: fragment "x" is already present` | 重复 id | `TestSetRefusesDuplicateAndInvalidFragments` | 换 id 或先 `Remove` |
| `systemprompt: assembly has no prompt text` | 装配为空却要转成 system 消息 | `TestSystemMessageRefusesEmptyText` | 确认片段已加入且 scope 匹配 |
| 提示词顺序不稳定 | 依赖插入顺序或 map 遍历 | `TestAssemblyIsDeterministicForEqualOrders` | 设置 `Order`；并列会自动按 `ID` 稳定 |
| 工具 schema 与校验不一致 | 手写 schema 文本 | `TestAssemblyToolSchemasMatchTheEnforcedSchema` | 由 `tools.Definition` 装配，不要手写 |

## 回到源码与下一步

阅读顺序：`systemprompt/fragment.go` 的 `Set.Ordered` → `systemprompt/assembly.go` 的 `Assemble` → `SystemMessage`。对应测试：`systemprompt/assembly_test.go`。

读者应能回答：机制解决什么问题（让“模型看到的提示词”可重建、可比较）、谁拥有资源（贡献者拥有片段集合，`Assembly` 是值）、Go 与 DSH 的差异在哪里（schema 不反解析回校验、未实现缓存与增量改写）。
