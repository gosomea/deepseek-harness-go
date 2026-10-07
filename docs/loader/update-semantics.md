# 从 Go 理解更新、启停与部分失败

## 读完要解决的问题

你已经有了一棵装配好的插件树。接下来的问题是：运行中改一条配置会发生什么？把提供方禁掉后，依赖它的消费者会怎样？以及为什么这里**不能**承诺「要么全成、要么全不动」。

这一篇进入 DSH 的 `Entry.update` 与 `EntryTree.update`。前置知识是前两篇的 Entry 与装配树，加上 Go 的错误包装与 `context` 取消。关联行为 ID 见 [M2 契约](../10-plans/dsh-go-replication-m02/plans.md)；读完你能解释：更新为什么分「先解析、后应用」两段，以及禁用一条条目时谁先停止。

## 从熟悉的 Go 写法出发

普通 Go 做法是把一批修改放进事务：

```go fragment
tx := db.Begin()
// 逐条修改
tx.Commit()  // 任何一步失败就 Rollback
```

它提供强承诺：要么全部生效，要么全部不生效。但插件树不是数据库。应用一条配置会**执行插件代码**——`Apply` 可能开文件、起 goroutine、注册服务；而已经在运行的实例不能「回退」成没运行过。所以 DSH 的 Loader 更新是**非事务性**的：逐条应用，失败的条目保留失败后的状态，其余条目照常生效。

这不是偷懒，而是对真实语义的如实描述。重要的是把这条边界**说清楚并且可观察**，而不是假装有事务。本包的做法是让 `Apply` 返回一份分项报告，谁成功、谁失败、谁被移除都按路径列出来。

哪里不等价：DSH 用 `Object.setPrototypeOf(this.ctx, this.parent.ctx)` 把 Context 原型链切到新父级，并按 `disabled` 先 `fiber.dispose()`；Go 侧用显式的 `Entry.group` 字段与 `Tree` 的挂载表表达同样的归属变化，没有原型链。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面这个完整程序就是 C05 的核心实验：配置 Consumer 与 Provider，按 ID 更新 Provider，再禁用它观察 Consumer 回到等待，最后恢复并校验条目 ID 与资源数回到原值。

```go runnable=loader-update
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

var providerLive, consumerLive int64

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
				atomic.AddInt64(&providerLive, 1)
				if _, err := ctx.Provide("store", "store-value"); err != nil {
					return nil, err
				}
				return func() error { atomic.AddInt64(&providerLive, -1); return nil }, nil
			},
		}
	}); err != nil {
		return err
	}
	if err := catalog.Register("cache", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "cache",
			Inject: []string{"store"},
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				atomic.AddInt64(&consumerLive, 1)
				return func() error { atomic.AddInt64(&consumerLive, -1); return nil }, nil
			},
		}
	}); err != nil {
		return err
	}

	tree := loader.NewTree(catalog)
	defer func() { _ = tree.Close(context.Background()) }()
	report := func(label string) {
		cache, _ := tree.View("cache")
		store, _ := tree.View("store")
		fmt.Printf("%-10s cache=%-7s store=%-7s provider=%d consumer=%d\n",
			label, cache.State, store.State, atomic.LoadInt64(&providerLive), atomic.LoadInt64(&consumerLive))
	}

	// 1. The consumer waits until its provider exists.
	document := []byte(`{"entries":[{"id":"cache","name":"cache"}]}`)
	if _, err := tree.Apply(document); err != nil {
		return err
	}
	report("consumer-only")

	// 2. Adding the provider activates the consumer.
	document = []byte(`{"entries":[{"id":"cache","name":"cache"},{"id":"store","name":"store"}]}`)
	if _, err := tree.Apply(document); err != nil {
		return err
	}
	report("provider-up")

	// 3. Updating the provider's config by id keeps its identity.
	config := json.RawMessage(`{"level":2}`)
	view, err := tree.Update("store", loader.Update{Config: &config})
	if err != nil {
		return err
	}
	fmt.Printf("after update: entry id=%s\n", view.ID)
	report("config-set")

	// 4. Disabling the provider returns the consumer to pending.
	if _, err := tree.Disable("store"); err != nil {
		return err
	}
	report("disabled")

	// 5. Enabling it restores both, with the same entry id.
	restored, err := tree.Enable("store")
	if err != nil {
		return err
	}
	fmt.Printf("restored: entry id=%s\n", restored.ID)
	report("enabled")
	return nil
}
```

预期输出：

```text output=loader-update
consumer-only cache=pending store=        provider=0 consumer=0
provider-up cache=active  store=active  provider=1 consumer=1
after update: entry id=store
config-set cache=active  store=active  provider=1 consumer=1
disabled   cache=pending store=        provider=0 consumer=0
restored: entry id=store
enabled    cache=active  store=active  provider=1 consumer=1
```

退出状态为 0。观察重点：

- 第 1 步 Provider 还不存在，Consumer 处于 `pending` 且资源数为 0——**依赖未满足不是错误**。
- 第 3 步按 ID 更新配置后，`entry id` 仍是 `store`，资源数没有变化：同一插件换配置是**原地重载**，不重新挂载。
- 第 4 步禁用 Provider 后它自己没有实例状态（空白），Consumer 回到 `pending` 且两者资源数都归零。
- 第 5 步恢复后条目 ID 仍是 `store`，资源数回到 1——**身份未变，数量回到原值**。

## 先预测，再观察

**预测**：如果把第 2 步的 `Apply` 换成一份含两条互不相关插件的文档，其中第二条的 `Apply` 返回错误，会发生什么？第一条会保留还是被撤销？

观测方法：构造两份声明，让第二条在 `Apply` 里 `return nil, errors.New("apply failed")`，然后检查 `Apply` 的返回值和 `tree.Entry` 的结果。

结果与解释：`Apply` 返回**非 nil 错误**且 `result.Failed` 里只有失败的那条路径；**第一条保留在树里**，它的资源没有被释放。这就是「非事务」：失败只影响失败的那一条。`result.Err()` 用 `errors.Join` 汇总所有失败，`errors.Is` 仍能匹配底层哨兵。**恢复到正常场景**：让第二条不再返回错误。

再预测一个：如果文档本身解析失败（比如末尾多个逗号），已经运行的第一条会被移除吗？答案是**不会**——解析与结构校验都在任何改动之前完成，被拒文档是 no-op。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | `Entry.update` 先合并 options，`disabled` 时 `fiber.dispose()` 并返回，否则比较新旧配置决定是否重挂；`EntryTree.update` 按 id 增删并把条目移入目标分组；`EntryTree.update` 的失败由各分组 `catch` 后记日志，不整树回滚 |
| TypeScript 入口 | [vendor/loader/src/config/entry.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/entry.ts) 的 `update`/`refresh`/`_patchContext`；[config/tree.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/tree.ts) 的 `update`/`create`/`remove`/`resolveGroup`；[config/diff.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/diff.ts) 的 `equalExceptVolatile` |
| Go 入口 | [update.go](../../loader/update.go) 的 `Update`/`SetDisabled`/`Enable`/`Disable`/`Move`/`Remove`/`Apply`（与 `ApplyResult`）、`View`；[tree.go](../../loader/tree.go) 的 `Paths`/`Entry` |
| 保持的行为 | 同一 `name` 不同 `id` 各自独立；重复 id 被拒；更新不改变条目身份；配置变化原地重载、名称变化替换实例；禁用条目保留声明但无实例；依赖未满足是 pending 而非错误；解析失败不破坏运行树 |
| Go 表达 | 非事务性用 `ApplyResult` 的分项列表如实报告，而不是伪事务；`Update` 先 `resolveOne` 再改动，使「解析后声明」成为候选值的边界；原地重载用 `Fiber.Update`，替换实例用 `Dispose` + `Plugin`；跨组移动用 `rekeyLocked` 同时改写多张按路径索引的表 |
| 不等价边界 | 没有 `!!js` 惰性表达式与 volatile-only 快路径（`equalExceptVolatile` 依赖 schemastery schema，Go 侧没有对应元数据）；没有 Node 模块 HMR 触发的自卸载回写；没有 `Context.intercept` 分层配置合并 |

## 一次操作的时序与所有权

`tree.Update(id, update)` 的步骤：

1. 取旧声明，按 `Update` 里非 nil 的字段构造候选声明。
2. **先解析候选**：查工厂、建插件、让插件校验配置。这一步失败就返回，运行树完全没动。
3. 比较新旧声明。相同即为 no-op（不重挂、不重置资源）。
4. 发布新声明，然后按情况处理实例：同插件换配置 -> `Fiber.Update`（原地重载，身份不变）；改名或禁用 -> 旧实例 `Dispose`，需要时挂新实例。
5. 返回该路径的 `EntryView`，同时把实例错误一起 `errors.Join` 出来。

禁用时的顺序由 `cordis` 决定：提供方失效后，**消费者先卸载**，然后才轮到提供方释放资源——`TestDisableKeepsIdentityAndGatesConsumer` 固定的就是这条顺序带来的可观察结果：两者资源数最终都归零，但消费者不会在依赖已经消失后还继续运行。

谁拥有并释放什么：`Tree` 拥有根上下文与挂载表；每个实例拥有自己的资源并在卸载时释放。更新过程中被替换下来的旧实例由 `reconcileEntry` 负责 `Dispose`，不会因为「换了新的」而泄漏。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复操作 |
| --- | --- | --- | --- |
| `loader: invalid config: ...`，而树看起来没变 | 候选声明没通过解析，更新被拒 | 错误文本里的字段名 | 修正 `Update` 的入参或文档字段 |
| `loader: entry not found: "x"` | 路径不存在，或移动后路径已变 | `tree.Paths()` | 用全路径重新定位；移动会改变路径 |
| `loader: target is not a group` / `cannot move an entry into its own subtree` | 移动目标不是分组，或目标在被移动子树内部 | 错误哨兵 | 换成真正的分组路径，且不要在自身子树内移动 |
| `loader: a group entry is updated through its children` | 对分组条目调用 `Update` | 错误哨兵 | 改它的子条目，而不是分组本身 |
| `Apply` 返回错误但树里多了条目 | 这是**部分成功**，不是回滚失败 | `result.Failed` 与 `result.Added` | 按路径修正失败项；成功项保留是有意行为 |

关于「失败时保留哪些条目」：`Apply` 的 `Result` 明确列出 `Added`、`Updated`、`Removed`、`Unchanged`、`Failed`。失败路径保持失败后的状态，**不承诺整树回滚**；调用方应当报告这份真实结果。

## 回到源码与下一步

建议阅读顺序：先看 [update.go](../../loader/update.go) 的 `Update`（为什么先解析后应用），再看 `Apply` 与 `ApplyResult`（非事务如何如实报告），最后看 `reconcileEntry`（原地重载与替换实例的分支）。对应测试是 [update_test.go](../../loader/update_test.go) 与 [update_more_test.go](../../loader/update_more_test.go)。

读完后你应该能回答：**更新为什么不做整树事务？谁拥有资源？** 因为应用配置会执行插件代码、不可回退；`Tree` 拥有根上下文，实例拥有自己的资源，被替换的旧实例由更新过程释放。

下一步是 M2.4：最小 bundle/profile、按序 overlay 与 required 规则。当前 M2.4 尚未实现，本页不引用还不存在的入口。
