# 从配置到运行中的插件树

## 读完要解决的问题

你已经读过 [概念对照](go-primer.md)、[装配树](entry-tree.md) 与 [更新语义](update-semantics.md)。这一篇把它们串成一个可以照着敲的流程：写一份 JSON 文档，用命令行检查它，再亲手制造几个错误看诊断。需要 Go 已安装、当前目录是仓库根目录；不要求你先读 TypeScript。

读完你能解释：一遍完整装配经过哪些步骤、每一步失败会看到什么，以及本阶段**不支持**什么。

## 从熟悉的 Go 写法出发

普通 Go 做法是让程序在代码里直接构造依赖：

```go fragment
plugin := greeter.New(store, "world")
```

它编译期就检查完，换一个插件要改代码并重新编译。装配层要解决的是「部署时决定运行哪些插件」：文档只写**名称**，宿主提供名称到构造函数的表。于是配置条目（文档里的一个节点，有稳定的 `id`）和插件实例（按名称构造出来的实现）必须是两个东西——否则「禁用」无法表达：被禁用的条目仍然是有效条目，但它不该构造插件。

本阶段的注册方式有一个明确边界：bundle 与插件都是宿主**显式编译期注册**的 Go 值。本包只支持显式编译期注册：不提供 npm 动态安装、模块 HMR 或原 profile 兼容。

## 最小实验

准备条件：Go 1.22+，仓库根目录。第一步，写一份配置：

```sh fragment
cat > /tmp/cordis.json <<'JSON'
{"entries":[
  {"id":"store","name":"store"},
  {"id":"cache","name":"cache","inject":"store"},
  {"id":"off","name":"store","disabled":true}
]}
JSON
```

三个条目各演示一件事：`store` 发布服务；`cache` 依赖它（`inject`）；`off` 被禁用，因此存在但不应运行。

第二步，检查它：

```sh fragment
go run ./cmd/dsh-go -config /tmp/cordis.json -plugin store,cache
```

预期 stdout：

```text
active cache
active store
unmounted off disabled
summary active=2 pending=0 unmounted=1
```

退出码为 0。观察三点：输出按条目路径排序，因此**可重复**；`off` 出现在 `unmounted` 而不是 `active`；`cache` 是 active，说明它所需的服务确实由 `store` 提供了。命令在返回前关闭了整棵树，所以进程退出时没有遗留资源。

第三步，看声明树：

```sh fragment
go run ./cmd/dsh-go -config /tmp/cordis.json -plugin store,cache -mode dump
```

预期 stdout：

```text
store [plugin]
cache [plugin]
off [plugin] disabled
```

这是**声明树**：只有文档写了什么，没有运行状态。要同时看运行状态，运行示例程序：

```sh fragment
go run ./examples/loader
```

它打印两种树与逐条目状态，输出与 [expected.txt](../../examples/loader/expected.txt) 逐字一致。

第四步，把 C05 的核心实验跑成一个完整程序。它按顺序做四件事：只声明 Consumer、加上 Provider、禁用 Provider、再启用；每一步都打印两个条目的 id 与状态，因此可以直接看到 Consumer 在等待与运行之间切换、而条目 id 始终不变。

```go runnable=loader-c05
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
	if err := catalog.Register("store", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "store",
			Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
				if _, err := ctx.Provide("store", "value"); err != nil {
					return nil, err
				}
				return func() error { return nil }, nil
			},
		}
	}); err != nil {
		return err
	}
	if err := catalog.Register("cache", func() cordis.Plugin {
		return cordis.Plugin{
			Name:   "cache",
			Inject: []string{"store"},
			Apply:  func(*cordis.Context, any) (cordis.Cleanup, error) { return func() error { return nil }, nil },
		}
	}); err != nil {
		return err
	}

	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()

	// Step 1: only the consumer is declared, so it waits for its dependency.
	if _, err := tree.Apply([]byte(`{"entries":[{"id":"cache","name":"cache"}]}`)); err != nil {
		return err
	}
	report(tree, "consumer-only")

	// Step 2: adding the provider activates the consumer.
	if _, err := tree.Apply([]byte(`{"entries":[{"id":"cache","name":"cache"},{"id":"store","name":"store"}]}`)); err != nil {
		return err
	}
	report(tree, "provider-up")

	// Step 3: disabling the provider returns the consumer to waiting.
	if _, err := tree.Disable("store"); err != nil {
		return err
	}
	report(tree, "disabled")

	// Step 4: enabling it again restores both entries under the same ids.
	if _, err := tree.Enable("store"); err != nil {
		return err
	}
	report(tree, "enabled")
	return nil
}

// report prints the id and lifecycle state of both entries, so each step shows
// which one is running and which one is waiting.
func report(tree *loader.Tree, label string) {
	for _, id := range []string{"cache", "store"} {
		view, _ := tree.View(id)
		fmt.Printf("%-13s %-5s state=%s\n", label, id, view.State)
	}
}
```

预期输出：

```text output=loader-c05
consumer-only cache state=pending
consumer-only store state=
provider-up   cache state=active
provider-up   store state=active
disabled      cache state=pending
disabled      store state=
enabled       cache state=active
enabled       store state=active
```

观察四点：第 1 步只有 Consumer 时它是 `pending`（依赖不存在不是错误）；第 2 步 Provider 出现后两者都变成 `active`；第 3 步禁用 Provider 后 Consumer 回到 `pending`，而 Provider 自己没有实例状态；第 4 步恢复后两者重新 `active`，**两个条目的 id 从头到尾没有改变**。资源数由 `loader` 与 `app` 的释放测试覆盖（见[装配树](entry-tree.md)与[更新语义](update-semantics.md)）。

逐步增加一个因素：把 `off` 的 `"disabled":true` 去掉再检查一次，它会从 `unmounted` 移到 `active`——**同一份文档，只因一个字段不同，运行结果就不同**。改回原样即可恢复。

## 先预测，再观察

**预测一**：如果删掉 `-config`，退出码是 1 还是 2？

观测方法：直接运行 `go run ./cmd/dsh-go`，然后 `echo $?`。

结果与解释：退出码是 **2**。命令行不完整属于用法错误，与「文档被拒绝」分开——后者是 1。这条区分让脚本能分别处理「调用方式错了」和「配置有问题」。**恢复到正常场景**：带上 `-config` 重新运行。

**预测二**：这五个错误分别属于哪一类？先写下答案再逐条试。

| 改法 | 你会看到 | 为什么 |
| --- | --- | --- |
| 把某个 `name` 改成没在 `-plugin` 里列出的名字 | 退出码 1，stderr 报未知插件 | 文档本身没错，是宿主没声明这个名字 |
| 给条目加一个不存在的字段（如 `nope`） | 退出码 1，报未知字段 | 拼写错误必须在加载时失败，不能静默丢配置 |
| 两个条目共用同一个 `id` | 退出码 1，报重复 id | `id` 是条目在树里的身份，重复后无法定位 |
| 删掉 `-config` | 退出码 2 | 用法错误，见预测一 |
| 把 `-mode` 改成 `bogus` | 退出码 2 | 未知模式同样是用法错误 |

**预测三（C05 核心）**：按 ID 更新 Provider 配置、再禁用它时，Consumer 会怎样？完整可运行程序在 [更新语义](update-semantics.md)，步骤是：只配置 Consumer 观察它停在 `pending`；加上 Provider 让它变为 `active`；按 ID 改配置后条目 **id 不变**、资源数不变（原地重载）；禁用 Provider 后 Consumer 回到 `pending`、两者资源数归零；恢复后条目 id 仍未变、资源数回到原值。先预测第 4 步里是 Consumer 先停止还是 Provider 先释放——答案是提供方失效后消费者先卸载，随后提供方释放自己的资源。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | Loader 是提供 `loader` 服务的插件，持有 EntryTree；Entry 有自己的 id、name 与 options；条目按名称导入模块并启动 Fiber |
| TypeScript 入口 | [vendor/loader/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/index.ts)（Loader 的 `builtins`、`unwrapExports`、`locate`、`internal/plugin` 挂接）；[vendor/loader/src/config/entry.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/entry.ts)（`EntryOptions`、`Entry.update`）；[vendor/loader/src/config/tree.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/tree.ts)（`EntryTree.resolve`、`create`、`update`） |
| Go 入口 | [cmd/dsh-go/main.go](../../cmd/dsh-go/main.go) 的 `run`/`check`/`dump`；[examples/loader/main.go](../../examples/loader/main.go) 的 `run`；[loader/entry.go](../../loader/entry.go) 的 `Load`/`Resolve` |
| 保持的行为 | 文档按名称引用插件；条目身份是 `id`；禁用条目存在但不运行；解析失败先于挂载；关闭释放全部条目；未知名称、重复 id、非法字段、类型不符各有不同诊断 |
| Go 表达 | 显式注册表（`Catalog`）替代运行时模块导入；`json.RawMessage` 保留配置原文，由插件自己 `Validate`；`Entry` 与 `cordis.Fiber` 通过 `Tree.owners` 双向绑定 |
| 不等价边界 | 没有 npm 动态安装、模块 HMR、`!!js` 惰性表达式、YAML include、原 profile 兼容、`Context.intercept` 与 per-entry isolate；这些属 M12.2 |

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| 退出码 2 且提示缺 `-config` | 命令行不完整 | stderr | 补上 `-config` |
| 退出码 1，未知插件 | `-plugin` 没列出该名字 | stderr 里的名字 | 补进 `-plugin`，或改文档 |
| 退出码 1，未知字段 | 文档字段拼错 | stderr 里的字段名 | 修正字段 |
| 条目出现在 `unmounted` 且标着 disabled | 该条目被禁用 | `-mode check` 输出 | 去掉 `disabled` |
| 条目停在 `pending` | 它 inject 的服务没有提供方 | 输出里的 pending 行 | 加上提供方，或检查 inject 名称 |

## 回到源码与下一步

建议顺序：先读 [cmd/dsh-go/main.go](../../cmd/dsh-go/main.go) 的 `run`，再读 [loader/entry.go](../../loader/entry.go) 的 `Load`，最后读 [loader/tree.go](../../loader/tree.go) 的 `mountEntry`。对应测试是 [cmd/dsh-go/main_test.go](../../cmd/dsh-go/main_test.go)、[loader/entry_test.go](../../loader/entry_test.go) 与 [loader/tree_test.go](../../loader/tree_test.go)。

**本阶段明确不支持**：npm 动态安装、模块 HMR、原 DSH profile 兼容、YAML include、`!!js` 惰性表达式与配置热重载。这些不是遗漏，而是按 [M2 契约](../10-plans/dsh-go-replication-m02/plans.md) 归属后续阶段（YAML 与惰性表达式属 M12.2）。

下一步是 M3：对话词汇、Agent Scope 与内存 Session。当前 M3 尚未实现，本页不引用还不存在的入口。
