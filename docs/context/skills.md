# 技能清单与实际加载的 token 差异

## 读完要解决的问题

一个 Agent 手头可能有几十个技能：写周报的、查数据库的、做代码审查的。它们全都"可用"，但**没有任何一个请求应该带上全部正文**——那会直接把上下文撑爆，而且模型多半也只需要其中一个。

于是出现两个必须分开的事实：**清单**（有什么可用，便宜、常驻）与**加载**（哪些正文真的进了请求，昂贵、按需）。把它们混成一个，就会出现两种坏事：Agent 拿到自己从未启用的指令，或者为从不使用的技能付 token 代价。

需要的 Go 知识：结构体、map、切片。不需要 TypeScript 或 Cordis 基础。当前实现范围是 M8.3 的发现、按需启用与卸载清理；**token 预算与截断属 M8.5，本文不声称支持**。完成实验后你能做到：**预测一次 `Load` 会带进哪些正文、一次 `Unload` 会带走哪些，并解释为什么清单不能直接当请求用。**

## 从熟悉的 Go 写法出发

普通做法是把技能都读进一个 `map[string]string`，需要时拼起来。这能工作，但有三个边界：

1. **没有清单与正文的区分**。map 里存着正文，遍历它就等于把全部技能塞进请求。
2. **依赖靠约定**。技能 A 依赖技能 B 这件事只写在文档里，运行时没人检查。
3. **卸载会残留**。删掉 map 里的一项，之前拼过的文本还在别处。

于是引入两个概念：

- **清单（Catalogue / Entry）**：技能的登记与列举。`Entry` **不含 Body 字段**——类型层面就杜绝了"拿清单当请求"。
- **加载器（Loader）**：记录哪些技能已启用，并负责传递地处理依赖。

类比之后要说清哪里不等价：这与"惰性初始化"不同。惰性初始化省的是**初始化开销**，而这里省的是**进入模型请求的内容**，并且它是可撤销的——`Unload` 之后渲染结果必须回到没有它。

## 最小实验

准备条件：仓库根目录，Go 1.22+。不需要网络与密钥。

```go runnable=skills-loading
package main

import (
	"fmt"

	"github.com/gosomea/deepseek-harness-go/plugins/skills"
)

func main() {
	catalogue := skills.NewCatalogue()
	for _, s := range []skills.Skill{
		{Name: "base", Description: "the base skill", Body: "BASE"},
		{Name: "mid", Description: "depends on base", Body: "MID", DependsOn: []string{"base"}},
		{Name: "lone", Description: "no dependencies", Body: "LONE"},
	} {
		if err := catalogue.Register(s); err != nil {
			panic(err)
		}
	}

	// The catalogue lists descriptions only; no body is reachable from it.
	fmt.Println("catalogue:")
	for _, e := range catalogue.Discover() {
		fmt.Printf("  %-5s %s\n", e.Name, e.Description)
	}

	loader := skills.NewLoader(catalogue)
	if err := loader.Load("mid"); err != nil {
		panic(err)
	}
	fmt.Println("enabled:", loader.EnabledNames())
	fmt.Printf("render: %q\n", loader.Render())

	// Unloading a prerequisite also removes what depended on it.
	if err := loader.Unload("base"); err != nil {
		panic(err)
	}
	fmt.Println("after unloading base:", loader.EnabledNames())
	fmt.Printf("render: %q\n", loader.Render())

	// Loading an unknown skill is an error, not an empty load.
	fmt.Println("unknown load error:", loader.Load("ghost") != nil)
}
```

```text output=skills-loading
catalogue:
  base  the base skill
  lone  no dependencies
  mid   depends on base
enabled: [base mid]
render: "BASE\n\nMID"
after unloading base: []
render: ""
unknown load error: true
```

观察重点：**清单里没有任何正文**（只有名称与描述）；`Load("mid")` 把 `base` 也带进来了（依赖被启用而非假定存在）；`Unload("base")` 之后 `mid` 也消失了（依赖者连带卸载），`render` 回到空串——**没有残留**。

## 先预测，再观察

**先写下你的预期**：在 `Loader` 上先 `Load("base")`，再 `Load("base")` 一次，`EnabledNames()` 是 `[base base]` 还是 `[base]`？再加一句 `Unload("lone")`（从未启用），返回错误还是成功？

观察方法：改几行后重跑。

结果：`EnabledNames()` 是 `[base]` —— **重复 `Load` 是幂等的**，不会把内容放两遍；`Unload("lone")` 返回 `skills: skill is not enabled` —— **未启用的卸载是错误而非静默成功**，否则调用方会以为移除了它从未添加的内容。恢复：删掉多余的行即可。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 技能发现、按需读取／启用与依赖声明；卸载清理贡献 |
| TypeScript 入口 | 固定提交 `00102833` 的 [packages/skill](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/skill) |
| Go 入口 | [discover.go](../../plugins/skills/discover.go) 的 `Catalogue`/`Entry`；[load.go](../../plugins/skills/load.go) 的 `Loader`/`Loaded`；测试见 [skills_test.go](../../plugins/skills/skills_test.go) |
| 保持的行为 | 清单只列描述；依赖被传递启用；重复加载幂等；卸载连带依赖者且不误伤无关技能；未启用卸载报错 |
| Go 表达 | `Entry` 刻意不带 Body 字段，用类型杜绝"目录当请求"；`Load` 用闭包递归并带 `visiting` 集合检测依赖环 |
| 不等价边界 | 本包**不解析技能文件**（只管理已登记的技能值）、**不含 token 预算与截断**（属 M8.5）、**不写 Session 日志** |

## 一次操作的时序与所有权

1. `Catalogue.Register` 校验并登记技能；重复名、缺描述/正文、自依赖与重复依赖在此被拒。
2. `Discover` 按名称排序返回 `Entry` 清单——**不含正文**。
3. `Loader.Load(name)` 先查目录（未知即 `ErrUnknownSkill`），再闭包递归：对每个依赖先递归启用，然后启用自身并追加到 `order`。
4. 递归中 `visiting` 集合检测环：再次进入正在访问的节点即 `ErrInvalidSkill`。
5. `Unload(name)` 先查存在性与启用状态，再迭代求「依赖它的已启用集合」，最后从 `order` 与 `enabled` 中一并移除。
6. `Loaded`/`Render` 在渲染时**从目录读取正文**，所以卸载后不可能读到已删内容。

所有权要点：**`Catalogue` 拥有技能内容，`Loader` 只拥有启用状态**。因为正文不在 `Loader` 里缓存，`Unload` 不需要「清理缓存」这一步——`enabled` 里没有它，渲染就不会看到它。这也是卸载不留残留的机制。

## 失败与定位

**失败一：未知技能。** 症状是 `skills: unknown skill: "ghost"`。原因：名称拼错或未登记。诊断入口：`Loader.Load` 的目录查询。恢复：改正名称或补登记——静默当成空加载会让调用方以为启用了什么。

**失败二：缺失依赖。** 症状是 `skills: missing dependency: "absent" is required by "needy"`。原因：依赖名未登记。诊断入口：`enable` 闭包内的目录查询。恢复：先登记依赖——否则请求会引用一个前提从未加载的技能。

**失败三：依赖环。** 症状是 `skills: invalid skill: "a" is part of a dependency cycle`。原因：A 依赖 B、B 又依赖 A。诊断入口：`visiting` 集合。恢复：打破环——递归会无限展开。

**失败四（容易混淆的边界）：卸载未启用的技能。** 症状是 `skills: skill is not enabled: "base"`。它与"未知技能"是两回事：前者是**存在但没启用**，后者是**根本不存在**。区分它们才能判断该补登记还是该改调用顺序。

## 回到源码与下一步

建议阅读顺序：

1. [discover.go](../../plugins/skills/discover.go) 的 `Skill.Validate` 与 `Entry` 的字段 —— 先看"清单为什么没有正文"。
2. 同文件的 `Catalogue.Discover` —— 再看排序为何是内容的函数。
3. [load.go](../../plugins/skills/load.go) 的 `Loader.Load` —— 确认依赖传递与环检测。
4. 同文件的 `Loader.Unload` —— 最后看连带卸载与不误伤。

对应测试名：`TestLoadPullsInDependencies`、`TestLoadedContentOnlyCoversEnabledSkills`、`TestUnloadRemovesTheSkillAndItsDependents`、`TestUnloadRefusesASkillThatWasNeverEnabled`、`TestLoadRefusesADependencyCycle`。

读完应能回答：**机制解决什么问题**（清单与请求分开，卸载不留残留）；**谁拥有资源**（`Catalogue` 拥有内容，`Loader` 只拥有启用状态）；**Go 与 DSH 的差异在哪**（本包不含预算与截断、不解析技能文件、不写日志）。

可运行实验见 [examples/context](../../examples/context/README.md)。下一篇：持久附件与本次请求可访问路径（`docs/context/attachments.md`，属 M8.4）。
