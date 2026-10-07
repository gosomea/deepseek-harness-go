# 从 Go 的理解配置与装配

## 读完要解决的问题

你已经会用构造函数把依赖传给一个结构体，也知道 Go 没有动态导入：编译进二进制的类型不会在运行时按字符串出现。那么一个宿主怎么让用户用配置文件决定"运行哪些插件"？

这一篇从这个问题进入 DSH 的 **Entry** 与 **Loader**。需要的前置知识是 Go 的函数值、接口、错误包装（`errors.Is`）和 JSON 编码。关联行为 ID 见 [M2 契约](../10-plans/dsh-go-replication-m02/plans.md)；当前实现范围是条目身份、工厂目录与 JSON 编解码，挂载与启动还没有实现。读完你能解释：为什么配置条目和插件实例必须是两个东西。

## 从熟悉的 Go 写法出发

普通 Go 做法是让宿主直接调用构造函数：

```go fragment
plugin := greeter.New(store, "world")
```

它确定、可读、编译期就检查完。边界也很清楚：调用关系写死在代码里，换一个插件要改代码并重新编译；谁能运行由源码决定，不由部署时的配置决定。

DSH 的 Loader 要解决的是"配置决定组合"。它的做法是让文档只写下**名称**，由宿主提供一张名称到构造函数的表。于是出现两个必须区分的概念：

- **条目（Entry）**：配置文档里的一个节点，有稳定的 `id` 和它表示的名称、配置、禁用状态。
- **插件（Plugin）**：按名称查到的工厂构造出来的具体实现，属于 `cordis`。

这个区分不是命名偏好，而是"禁用"这个功能的前提。被禁用的条目**仍然是一个有效条目**——它要被解析出来、要能显示在诊断里、要能重新启用——但它不该构造插件。如果两者是同一个类型，就没法同时表达"我在配置里存在"和"我不运行"。

哪里不等价：DSH 按字符串解析模块并支持 npm 动态安装与 HMR；Go 没有运行时模块。本包用`Catalog`显式注册替代动态解析，因此**只能运行宿主编译进来的插件**。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面的完整程序打印两个条目的解析结果。

```go runnable=loader-primer
package main

import (
	"errors"
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
	err := catalog.Register("echo", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "echo",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				return nil, nil
			},
			Validate: func(config any) (any, error) {
				text, ok := config.(string)
				if !ok {
					return nil, errors.New("echo needs a string config")
				}
				return text, nil
			},
		}
	})
	if err != nil {
		return err
	}

	document := []byte(`{"entries":[{"id":"a","name":"echo","config":"hello"}]}`)
	entries, err := loader.Load(document, catalog)
	if err != nil {
		return err
	}
	fmt.Printf("%s -> %v (runnable=%t)\n", entries[0].ID(), entries[0].Config(), entries[0].Runnable())
	return nil
}
```

预期 stdout：

```text output=loader-primer
a -> hello (runnable=true)
```

退出状态为 0。观察重点：配置里的 `"hello"` 是字符串，插件把它校验后返回同类型值；`Entry.Config()` 拿到的是**校验后**的值，而 `Entry.Options().Config` 是文档原文。

逐步增加一个因素：把 `"config":"hello"` 改成 `"config":123`，插件自己的 `Validate` 会拒绝，`Load` 返回包装 `ErrInvalidConfig` 的错误而不是静默接受。

## 先预测，再观察

先写下你的预期，再运行。

**预测**：把文档改成下面这份，`loader.Load` 会返回错误吗？如果返回，是哪一类？

```json
{"entries":[{"id":"a","name":"echo","config":"hello"},{"id":"a","name":"echo","config":"bye"}]}
```

观测方法：把 `document` 换成这一段后重跑，并在 `loader.Load` 失败时用 `errors.Is` 判断：

```go fragment
if errors.Is(err, loader.ErrDuplicateID) {
	fmt.Println("duplicate id:", err)
}
```

结果与解释：两个条目共用一个 `id`，`id` 是条目在树里的身份，重复后无法定位，所以解析阶段就拒绝，返回 `ErrDuplicateID`。它和"未知插件名"（`ErrUnknownPlugin`）是不同失败：前者是文档结构问题，后者是宿主没有注册那个名称。**恢复到正常场景**：把第二个条目的 `id` 改成 `b`。

再预测一个：把 `name` 改成 `"missing"`（没有注册），错误是 `ErrUnknownPlugin` 还是 `ErrInvalidConfig`？答案是前者——文档本身没问题，是宿主的能力不足。练习答案都能在 [config_test.go](../../loader/config_test.go) 与 [entry_test.go](../../loader/entry_test.go) 找到。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | Entry 是配置条目，EntryTree 持有条目表，EntryGroup 持有条目的父子关系；Loader 是提供 `loader` 服务的插件，负责导入模块并启动 |
| TypeScript 入口 | [vendor/loader/src/config/entry.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/entry.ts) 的 `EntryOptions` 与 `Entry`；[config/tree.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/tree.ts) 的 `EntryTree` |
| Go 入口 | [Options](../../loader/config.go) 对应 `EntryOptions`；[Entry](../../loader/entry.go) 对应 `Entry`；[Catalog](../../loader/catalog.go) 对应 `EntryTree.import` 的名称解析 |
| 保持的行为 | 条目身份与名称分开；禁用条目不运行；配置原始值与校验结果分开；未知名称、重复 id、非法字段可观察且互不混淆 |
| Go 表达 | `Catalog` 用显式注册表加读写锁替代运行时模块解析；`Options` 用 `json.RawMessage` 保留配置原文，让插件自己校验；`Inject` 自定义 `UnmarshalJSON` 接受字符串或数组两种文档写法 |
| 不等价边界 | Go 没有动态 `import`、模块 HMR、`!!js` 表达式求值；未实现父子树挂载、按 id 更新、启停、per-entry isolate 与 `Context.intercept` 分层配置合并 |

## 一次操作的时序与所有权

`loader.Load(data, catalog)` 的步骤与所有权：

1. `ParseDocument` 解析文档，建立条目列表。此时还没有插件，配置只是原文。
2. 逐个条目判断：禁用或分组条目直接成为 `Entry`，不查目录、不构造插件。
3. 其余条目用 `Catalog.Lookup(name)` 取工厂。目录持锁只在查表期间，不覆盖后续构造。
4. 工厂被调用，构造出 `cordis.Plugin`。插件由调用者拥有，本包不注册它。
5. 原始配置交给插件自己的 `Validate`。校验失败则整次 `Load` 失败，之前构造的 Entry 不返回给调用者。
6. 返回 `[]*Entry`。**没有任何资源需要释放**：本包不调用 `Apply`、不注册服务、不启动 goroutine。

谁释放什么：这一层不产生资源所有权，所以没有清理职责。真正需要 `OnDispose` 的是后续切片里把条目挂进 `cordis` 时的插件激活。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复操作 |
| --- | --- | --- | --- |
| `loader: unknown plugin: "x"` | 宿主没有注册 `x` | `Catalog.Names()` 列出已注册名称 | 用 `Register` 补注册，或修正文档中的 `name` |
| `loader: duplicate entry id: "a"` | 同一文档两个条目共用 `id` | 文档的 `id` 字段 | 给其中一个改成唯一 id |
| `loader: invalid config: field ...` | 未知字段或类型不符 | 错误文本里的字段名 | 修正字段名或值类型 |
| `Loader` 解析成功但插件不运行 | 条目 `disabled: true`，或它是 `group` | `Entry.Disabled()` 与 `Entry.IsGroup()` | 去掉 `disabled` 后重新加载 |

后两种都属于"看起来成功但没有运行"，必须靠查询条目状态而不是看解析是否返回错误来区分——这也是为什么 `Runnable()`、`Disabled()`、`IsGroup()` 三个查询要分开提供。

## 回到源码与下一步

建议阅读顺序：先看 [config.go](../../loader/config.go) 的 `ParseDocument`，再看 [catalog.go](../../loader/catalog.go) 的 `Lookup`，最后看 [entry.go](../../loader/entry.go) 的 `Resolve`。对应测试是 [config_test.go](../../loader/config_test.go)、[entry_test.go](../../loader/entry_test.go) 与 [catalog 相关用例](../../loader/config_test.go)。

读完后你应该能回答：**配置条目的身份为什么不能和插件实例合并？谁拥有资源？** 本层不拥有资源；所有权在下一层（条目挂进插件树）才出现。

下一步是 M2.2：把条目挂进父子树、把 Entry 绑定到 Fiber，并观察声明树与运行树的区别。当前 M2.2 尚未实现，本页不引用还不存在的入口。
