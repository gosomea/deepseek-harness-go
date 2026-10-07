# 从 Go 理解装配树与分组

## 读完要解决的问题

你已经能把一份 JSON 配置解析成条目。接下来的问题是：这些条目怎么组织成一棵树，谁持有子条目，以及为什么需要分别看「配置文件写了什么」和「实际跑起来了什么」。

这一篇进入 DSH 的 **EntryTree** 与 **EntryGroup**。前置知识是上一篇的 Entry 与工厂目录，加上 Go 的互斥锁与 `context.Context`。关联行为 ID 见 [M2 契约](../10-plans/dsh-go-replication-m02/plans.md)；当前实现范围是父子树、分组、Entry 到 Fiber 的绑定与 dump，按 ID 更新与启停属于 M2.3。读完你能解释：声明树和运行树为什么必须分开看，以及关闭根树时谁负责释放什么。

## 从熟悉的 Go 写法出发

普通 Go 做法是把子对象直接放进父结构体：

```go fragment
type Tree struct {
	Children []*Node
}
```

它清楚、无锁、易读。边界在于：配置里的树是**声明**，运行时的插件是**实例**，两者并不一一对应。被禁用的条目在配置里存在但没有实例；一个分组条目自己不是插件，却拥有子条目；一个插件启动失败时条目仍在树里但没有可用的运行状态。用同一个 `Children` 表示两者，就再也分不清「配置要求」和「运行时事实」。

DSH 的做法是把两者都保留：`EntryTree` 持有配置条目，`EntryGroup` 表达父子归属，每个条目的 `fiber` 字段指向它启动出来的实例。Go 侧对应 [Tree](../../loader/tree.go)（持有条目、实例绑定与根上下文）、[Group](../../loader/group.go)（父子归属）与 [dump.go](../../loader/dump.go)（把两种状态都渲染出来）。

哪里不等价：DSH 用 `ctx.fiber.entry` 在 Context 上反向挂载所属条目，并支持模块 HMR 时的自卸载处理；Go 侧用 `Tree.owners` 这张显式的 `Fiber.ID` 到路径的表做双向绑定，没有动态属性。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面的完整程序用一份嵌套配置装配一棵树，并分别打印声明树与运行树。

```go runnable=loader-tree
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	catalog := loader.NewCatalog()
	err := catalog.Register("worker", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "worker",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				return nil, nil
			},
		}
	})
	if err != nil {
		return err
	}

	tree := loader.NewTree(catalog)
	document := []byte(`{"entries":[` +
		`{"id":"g","group":true,"config":[` +
		`{"id":"live","name":"worker"},` +
		`{"id":"off","name":"worker","disabled":true}]}]}`)
	if err := tree.Load(document); err != nil {
		return err
	}

	declared, err := tree.Dump(loader.Declared)
	if err != nil {
		return err
	}
	running, err := tree.Dump(loader.Running)
	if err != nil {
		return err
	}
	fmt.Print("--- declared ---\n", declared)
	fmt.Print("--- running ---\n", running)

	fiber, ok := tree.Fiber("g:live")
	if !ok {
		return fmt.Errorf("g:live has no instance")
	}
	owner, _ := tree.Locate(fiber)
	fmt.Printf("Locate(g:live instance) = %s\n", owner)
	return tree.Close(context.Background())
}
```

预期输出：

```text output=loader-tree
--- declared ---
g [group]
  g:live [plugin]
  g:off [plugin] disabled
--- running ---
g [group] state=not-mounted
  g:live [plugin] state=active
  g:off [plugin] disabled state=not-mounted
Locate(g:live instance) = g:live
```

退出状态为 0。观察重点：同一份配置有两份输出。`g:off` 在声明树里是 `disabled`，在运行树里是 `state=not-mounted`；分组条目 `g` 两边都有，但运行树明确它没有实例。嵌套条目报告的是全路径 `g:live`，不是本地 id `live`。

## 先预测，再观察

**预测**：把配置里 `g` 这一层的 `"group":true` 去掉，保留 `config`，然后运行。会发生什么？

观测方法：改掉文档后重跑，看两件事——`tree.Load` 是否返回错误，以及 `tree.Fiber("g")` 是否有值。

结果与解释：去掉 `group` 后，`g` 变成一个普通插件条目，而 `worker` 是它的 `name`，于是 `g` 会去构造一个插件，此时 `config` 里那串子条目数组就成了这个插件的配置。结果取决于该插件是否接受数组配置；`worker` 没有 `Validate`，因此接受原样并由 `Tree` 启动。此时 `tree.Fiber("g")` **有**值——它现在是插件而不是分组。这正是条目身份决定它如何被处理的可观察后果。**恢复到正常场景**：把 `"group":true` 加回去。

再预测一个：如果 `g:live` 的 `Apply` 一直阻塞，而此时调用 `tree.Close`，`g:live` 会出现在运行树里吗？答案是**不会**——见「失败与定位」一节。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | `EntryTree` 持有条目表与嵌套子树，`EntryGroup` 持有子条目列表；`Entry.fiber` 指向它启动的实例；`EntryTree.sep` 是把嵌套 id 连成全路径的分隔符 |
| TypeScript 入口 | [vendor/loader/src/config/tree.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/tree.ts) 的 `EntryTree`（`store`、`root`、`entries()`、`resolve`、`import`）与 [config/group.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/group.ts) 的 `EntryGroup`（`data`、`create`、`remove`、`update`、`stop`） |
| Go 入口 | [Tree](../../loader/tree.go) 的 `Load`/`Mount`/`Entry`/`Group`/`Fiber`/`Locate`/`Entries`/`Close`；[Group](../../loader/group.go) 的 `Children`/`Entries`/`Len`；[dump.go](../../loader/dump.go) 的 `Dump`/`Diagnose` |
| 保持的行为 | 嵌套条目报告全路径；分组条目自身不运行但拥有子条目；被禁用条目在声明树中存在而运行树中无实例；关闭根树释放全部条目；加载中被关闭的条目不得发布 |
| Go 表达 | `EntrySeparator`（冒号）显式拼全路径；`Tree.owners` 用 `Fiber.ID` 到路径的表实现双向绑定（对应 `fiber.entry`）；`loadMu` 串行化多次加载、`mu` 保护挂载表；条目的发布与实例绑定在同一把锁下完成 |
| 不等价边界 | 没有 Node 动态 `import`、模块 HMR 与自卸载后的 `disabled` 回写；没有 `Entry.context` 的原型链 Context；per-entry isolate 与 `Context.intercept` 分层配置属后续切片 |

## 一次操作的时序与所有权

`tree.Load(data)` 的步骤：

1. `ParseDocument` 解析文档，得到声明条目。
2. `validateEntries` 先对整棵树做结构预校验：本地 id、路径分隔符、嵌套 group 的子条目。**被拒绝的文档是 no-op**——不会留下前几个条目已经装进去的半棵树。
3. `mountInto` 逐层装配。每个条目先把全路径写成身份（`outer:mid:leaf`），再经 `Resolve` 查出插件并让插件自己校验配置。
4. 可运行条目调用 `ctx.Plugin(...)` 启动实例。此时 `Apply` 执行，实例获得自己的资源所有权。
5. **条目与实例在同一把锁下一起发布**。若期间 `Close` 已把树标记为关闭，实例会被 `Dispose` 而不是留下无主的资源。
6. 分组条目改为建立嵌套 `Group` 并递归装配其子条目。

谁拥有并释放什么：`Tree` 拥有自己的 `cordis` 根上下文；每个条目启动的实例的资源由该实例持有，并在卸载时按注册倒序释放。`Tree.Close` 关闭根上下文并等待清理，因此**关闭根树等于释放全部条目**。`Group` 本身不持有资源，只表达归属。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复操作 |
| --- | --- | --- | --- |
| `loader: duplicate entry id: "g:x"` | 同一分组下两个条目共用本地 id | 错误文本里的全路径 | 给其中一个改唯一 id |
| `loader: invalid config: entry id` 提到路径分隔符 | 本地 id 里写了冒号，会与嵌套路径冲突 | 同上 | 去掉本地 id 中的冒号 |
| `loader: tree is closed` | 在已关闭的树上继续 `Load` | `tree.Closed()` | 用新树重新装配；关闭后不接受新条目 |
| `Diagnose()` 报某条目的状态是 pending | 插件声明的依赖服务不可用 | `Diagnose()` 输出 | 补上提供方后 `Refresh()`；这不是加载失败 |
| `Diagnose()` 说某条目声明了插件但没有实例 | 条目可运行却没有绑定实例，说明发布没发生 | `Diagnose()` 与 `tree.Fiber(path)` | 视为不变量被破坏，检查加载是否被 `Close` 中断 |

关于「加载中的过时结果不发布」：如果某个 `Apply` 一直阻塞，而 `Close` 在这期间把树标记为关闭，那么该条目与它的实例都**不会**进入运行树，实例会被 `Dispose` 释放。这是 [TestCloseDuringLoadDoesNotPublishStaleEntry](../../loader/tree_test.go) 固定的行为；否则一个已经没人拥有的实例会带着资源泄漏下去。

`Diagnose` 对已关闭的树返回空结果：关闭后所有实例都被释放是设计行为，把它们报成故障会让一次正常关闭看起来像出事。

## 回到源码与下一步

建议阅读顺序：先看 [tree.go](../../loader/tree.go) 的 `validateEntries`（结构为什么先校验）、再看 `mountEntry`（发布与绑定为什么在同一把锁下）、最后看 [dump.go](../../loader/dump.go) 的 `formatRow`（两种形态为什么共享路径与缩进）。对应测试是 [tree_test.go](../../loader/tree_test.go) 与 [group_test.go](../../loader/group_test.go)。

读完后你应该能回答：**为什么声明树和运行树要分开看？谁拥有资源？** `Tree` 拥有根上下文，实例拥有自己的资源，`Group` 什么都不拥有。

下一步是 M2.3：按 ID 增删改、启停与跨组移动。当前 M2.3 尚未实现，本页不引用还不存在的入口。
