# 从 Go 理解 Agent 可见层：两个 Agent 同时运行，工具为什么不串味

## 读完要解决的问题

你在 Go 里写过注册表：一张 `map[string]Tool`，几个包往里 `Register`，执行时按名字查出来。单进程单 Agent 时它工作得很好。

现在同一个进程要跑两个 Agent。A 注册了 `fs.write` 到自己的工作区，B 没有。它们共用一张注册表，于是 B 也能查到 `fs.write`，而且查到的可能是 A 的那一份。你可能想“那就加个 owner 字段，查询时过滤”——

```go fragment
type Registry struct {
	mu     sync.Mutex
	tools  map[string]Tool
	owners map[string]string // 谁是属主
}
```

这个方向能跑，但它把三个问题混在一起：**可见性**（B 该不该看见 A 的工具）、**释放**（A 结束时释放哪一份）、**顺序**（同一个名字有多个定义时谁胜出、呈现顺序怎么定）。而且当 A 需要“看见全局工具 + 自己的工具”，B 需要“看见全局工具 + A 的共享工具 + 自己的”时，过滤条件会变成一张越写越长的表。

这一篇从这个问题进入 DSH 的 **scope**。前置知识是 Go 的 map、指针身份、闭包与 `context.Context` 的基本用法。关联行为见 [M3 执行契约](../10-plans/dsh-go-replication-m03/plans.md)；当前实现范围是身份、链、条目表、贡献层与事件路由。读完你能解释：为什么作用域不是“给注册加一个 owner 字段”，以及为什么它和 Cordis 的服务隔离是两件不同的事。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/core/scope/src/index.ts` 与 `packages/core/scope/src/store.ts`。

## 从熟悉的 Go 写法出发

“加一个 owner 字段再过滤”的第一个边界是**没有落脚点放“这就是 A 这个身份”**。用字符串会有真实的错误：同一个会话 id 的两代 Agent 必须各自路由，用字符串 `"sess-1"` 做 key 就分不开。所以身份必须是**值身份**——在 Go 里就是一个指针。

第二个边界是**释放**。你希望“释放 A 的一切”是一个动作，而不是记住 A 注册过哪些东西再逐个删。Go 里能做这件事的工具是闭包：每次注册返回一个撤销函数，并把它们挂到一个所有者上。`defer` 只管当前调用栈，跨调用就必须自己持有。

第三个边界是**继承**。B 要看得见 A 的工具，是因为 B 是 A 的子 Agent；C 是 B 的子 Agent，也要看得见 B 和 A 的。这已经不是过滤，是一条**链**。

DSH 的作用域把这三件事合成一个原语：

- **身份**：一个不透明的 key（对象的身份比较）。
- **所有权**：通过某个作用域注册的一切，随该作用域的 fiber 一起释放。
- **链**：一个 key 只有一个父，绑定一次；链只存一次，却支撑两个方向。

哪里不等价：DSH 用 JavaScript 的 `WeakMap` 存父链，并对上下文做**原型继承**来读“最近的作用域标记”。Go 既没有 weak map，也没有原型链，本包用显式表和 `ScopeOf` 替代，差异记在 [scope 包的限制](../../scope/README.md#限制)里。另外 DSH 用编译期 brand 让“用裸 subject 派发作用域事件”成为类型错误，Go 只能做运行期检查。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面的完整程序造出一棵父子链，看两层可见性和遮蔽，再看事件路由的方向。

```go runnable=scope-chain
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/scope"
)

// layer is one scope's aggregate contribution.
type layer struct {
	items *scope.NamedEntries[string]
}

// IsEmpty reports whether the layer holds nothing.
func (l *layer) IsEmpty() bool { return l.items.IsEmpty() }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root := cordis.New()
	defer func() { _ = root.Close(context.Background()) }()

	layers := scope.NewLayers[*layer](func(scope.Key) *layer {
		return &layer{items: scope.NewNamedEntries[string](nil)}
	}, nil)

	// One global contribution shared by everyone.
	globalUndo, err := layers.Effect(root, func(shared *layer) (func(), error) {
		return shared.items.Insert("read", "global")
	})
	if err != nil {
		return err
	}
	defer func() { _ = globalUndo() }()

	parent, child, sibling := new(int), new(int), new(int)
	parentScope, err := scope.Create(root, parent)
	if err != nil {
		return err
	}
	childScope, err := scope.Create(root, child, scope.WithParent(parent))
	if err != nil {
		return err
	}
	siblingScope, err := scope.Create(root, sibling, scope.WithParent(parent))
	if err != nil {
		return err
	}

	insert := func(target *scope.Scope, name, value string) error {
		_, err := layers.Effect(target.Ctx(), func(own *layer) (func(), error) {
			return own.items.Insert(name, value)
		})
		return err
	}
	if err := insert(parentScope, "read", "parent"); err != nil {
		return err
	}
	if err := insert(childScope, "write", "child"); err != nil {
		return err
	}
	if err := insert(siblingScope, "list", "sibling"); err != nil {
		return err
	}

	pick := func(l *layer) *scope.NamedEntries[any] {
		view := scope.NewNamedEntries[any](nil)
		for _, entry := range l.items.Entries() {
			_, _ = view.Insert(entry.Name, entry.Value)
		}
		return view
	}
	describe := func(label string, key scope.Key) {
		view := layers.Merge(key, pick)
		fmt.Printf("%-8s names=%v\n", label, view.Names())
		for _, name := range view.Names() {
			value, _ := view.Get(name)
			fmt.Printf("         %s=%v\n", name, value)
		}
	}
	describe("parent", parent)
	describe("child", child)
	describe("sibling", sibling)

	// Event admission: up the chain only.
	toChild := scope.NewCarrier(child)
	fmt.Printf("child dispatch -> parent listener=%t child listener=%t sibling listener=%t global=%t\n",
		toChild.Admit(parent), toChild.Admit(child), toChild.Admit(sibling), toChild.Admit(nil))
	toParent := scope.NewCarrier(parent)
	fmt.Printf("parent dispatch -> child listener=%t\n", toParent.Admit(child))

	// Peek is chain-blind: a table asking for its OWN layer must not see ancestors.
	_, ownOnly := layers.Peek(child)
	peeked, _ := layers.Peek(parent)
	fmt.Printf("peek(child) exists=%t peek(parent) has read=%t\n", ownOnly, peeked.items.Has("read"))
	return nil
}
```

预期输出：

```text output=scope-chain
parent   names=[read]
         read=parent
child    names=[read write]
         read=parent
         write=child
sibling  names=[read list]
         read=parent
         list=sibling
child dispatch -> parent listener=true child listener=true sibling listener=false global=true
parent dispatch -> child listener=false
peek(child) exists=true peek(parent) has read=true
```

几行值得注意：

- `child names=[read write]`：`child` 看得见自己的 `write`，也看得见 `parent` 的 `read`（`read` 遮蔽了全局的同名项，所以值显示 `parent`）。
- `sibling` 看不到 `child` 的 `write`：兄弟之间互不可见。
- `child dispatch -> sibling listener=false`：事件只向上。`parent` 听得见 `child` 的派发，`sibling` 听不见。
- `parent dispatch -> child listener=false`：反向不成立。
- `peek(child) exists=true`：`child` **确实**有自己的层（它注册了 `write`），但 `Peek` 只回答自己那一层。

## 先预测，再观察

先做一个预测：把 `child` 的注册改成插入一个**与 `parent` 同名**的项：

```go fragment
if err := insert(childScope, "read", "child-read"); err != nil {
	return err
}
```

`child names=...` 这一行会变成什么？`read` 的顺序会变吗？

预测之后，运行

```sh
go test -run TestLayersMergeShadowsInPlace ./scope
```

答案：名字顺序**不变**，值变成 `child-read`。也就是 `names=[read write]`，而 `read=child-read`。原因是遮蔽是**按名字覆盖值、保留全局的插入位置**，不是重新排列。

为什么这样规定？因为顺序是注册表对消费者呈现的事实。如果一个作用域的遮蔽会让条目跳到列表末尾，那么“全局注册顺序”这个承诺在子作用域里就失效了，消费者看到的两份列表顺序不一致。保留位置让“全局的先后关系”在任何作用域里都一样。

再预测一次：**删掉父链**会怎样？把 `scope.WithParent(parent)` 从 `child` 的创建里去掉，重新运行上面的程序。

`child names=[write]`：`child` 不再继承 `parent` 的 `read`，但**全局**的 `read` 仍然可见（因为全局层是所有视图的种子）。这说明一件事：全局层不是“无作用域的那一层”，它是**每个**视图都会看到的一层。恢复方式：把 `WithParent(parent)` 加回去。

最后一次预测：如果你对一个 key **绑两次父**会怎样？

```go fragment
binding, _ := scope.BindParent(child, parent)
_, err := scope.BindParent(child, sibling)
```

第二次返回 `ErrAlreadyBound`。用 `go test -run TestBindParentIsOnceAndCycleChecked ./scope` 验证：同一个测试还固定了“持有 `binding` 的人可以 `Rebind`”与“闭合环被拒绝”。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | `createScope(ctx, key)` 铸造带身份的注册期；`scopeOf(ctx)` 读最近的标记；`bindScopeParent`／`scopeChainOf` 维护链；`ScopedLayers`／`NamedEntries`／`AnonymousEntries` 保存分层贡献；`scopeTarget` 做事件过滤 |
| TypeScript 入口 | `packages/core/scope/src/index.ts`（身份、carrier、链）、`packages/core/scope/src/store.ts`（三张表与 `effect()` 编排）、`packages/core/scope/README.md` |
| Go 入口 | [scope/key.go](../../scope/key.go)（`Key`、`BindParent`、`ChainOf`、`Scope`）、[scope/entries.go](../../scope/entries.go)（两张表）、[scope/layers.go](../../scope/layers.go)（`Layers`、`Merge`、`Effect`、`Reclaim`）、[scope/carrier.go](../../scope/carrier.go)（`Carrier`、`Admit`、`CheckDispatch`） |
| 保持的行为 | 注册视图向下继承、最近者胜出且保留位置；事件准入向上扩展；一层内重名是冲突、跨层重名是遮蔽；读操作不创建层；回收以“整层聚合为空”为条件；`Effect` 立即执行并在失败时不留下空层 |
| Go 表达 | 身份用值身份（指针）而不是字符串；两张表用显式切片保序（Go 的 map 无序）；`Layers[L]` 泛型让调用方拥有层的语义；`Effect` 返回 `(undo, error)` 让失败落在调用点 |
| 不等价边界 | 见下表 |

### Go 比参考弱的三处（必读）

| 项目 | 固定参考 | 本包 | 影响 |
| --- | --- | --- | --- |
| 编译期 carrier brand | `Scoped<T>` 让裸 subject 派发成为类型错误 | 无 | 用错 carrier 只能由 `CheckDispatch` 运行期发现；这是唯一的防线，不能省 |
| 弱引用 | `WeakMap` 让不可达的 key 被回收 | 显式表，`Dispose` 时删除 | **未被 Dispose 的 key 会长期驻留**：真实的保留差异 |
| 上下文标记继承 | `scopeOf` 沿原型链读最近标记 | 显式 `contextKeys` 表 | 再嵌套一层插件得到的上下文不会自动带标记，需显式再 `Create` |

### 为什么不等于 Cordis 的服务隔离

`cordis.Context.Isolate(name, scope)` 做的是：让**某一个服务名**在解析时用另一套命名空间。它是一处服务绑定的分区，仅此而已——它不给注册打标记、不路由事件、不表达层次、不拥有释放边界。

作用域有这四样，却**没有**服务替换：把作用域上下文交出去，同时也交出了铸造它的插件的注入服务，所以作用域既不放宽也不收紧服务可见性。两者正交，可以叠加（参考实现里 `createScope` 从不调用 `isolate`）。

在 Go 里这个区分有一个直接的后果：**不要**把现有的 `Context.Isolate` 改名或复用成作用域。它的身份测试是 `SameScope`（比服务命名空间），而作用域的身份是 key 的值身份。

## 一次操作的时序与所有权

一次带继承的注册：

1. `Create(root, agent)` 在 root 下注册一个无依赖的子插件，并把该激活的上下文记入 `contextKeys`。返回一个 `*Scope`。
2. 要用自己的上下文时调 `scoped.Ctx()`，得到那个被标记的 `*cordis.Context`。
3. `Layers.Effect(ctx, action)` 用 `ScopeOf(ctx)` 决定两件事：这条贡献归哪个作用域、随谁释放。
4. 层在 `Effect` 内部即时取得（没有就创建），`action` **立即**执行并返回 undo。
5. `ctx.OnDispose(...)` 把“undo + 回收 + 通知”登记为这个激活的清理，返回 cordis 自己的 disposer。
6. `Merge(scope, pick)` 读时：先取全局层，再按链**从最远祖先到本作用域**逐层遮蔽。
7. `scoped.Dispose()` 触发清理：undo 这一次插入 → 整层为空则回收 → 通知；激活卸载后 `Scope` 的清理还会删除父链与标记。

谁持有状态：`parentOf`、`carriers`、`contextKeys` 是**包级表**，所以 key 必须全局唯一（这正是身份要求指针的原因）；每个作用域持有自己的 `*cordis.Context`；`Layers` 持有层映射。并发上，注册与释放都经过 cordis 的运行期锁与 `OnDispose`，本包不自己加锁；`contextKeys` 与 `parentOf` 由 cordis 的注册时序串行化（注册发生在激活期间，`Dispose` 在卸载期间）。

## 失败与定位

**一、`scope: scope parent link would form a cycle`。**
症状：`BindParent` 或 `Rebind` 返回这个错误。原因：新父链会绕回自己，而所有遍历都假定能走到根。诊断入口：[scope/key.go](../../scope/key.go) 的 `linkParent`。恢复：检查你是不是把祖先重新挂到了自己的后代下；改成一个不成环的父，或不要绑定。

**二、`scope: scope key is already bound to a parent`。**
症状：第二次绑定同一个 key 失败。原因：绑定是一次性的，只有第一次返回的 `ParentBinding` 可以 `Rebind`。恢复：保留第一次的返回值并调 `Rebind`；或者从一开始就用 `WithParent` 表达唯一的那次归属。

**三、`scope: effect action returned no undo for scope ...`。**
症状：`Effect` 的 action 返回了 `nil` undo。原因：一条贡献必须能被撤销，否则它无法随作用域释放。诊断入口：[scope/layers.go](../../scope/layers.go) 的 `Effect`。恢复：让 action 返回它插入时拿到的 undo（`NamedEntries.Insert` 与 `AnonymousEntries.Append` 都返回一个）。

区分四类：**预期等待**（作用域上下文在 ctx 不可写时为 nil，见 `Ctx()` 文档）、**无效输入**（上面的第一、二、三条）、**执行失败**（action 返回的错误会原样从 `Effect` 返回，且不留空层）、**释放失败**（undo 的错误由 cordis 汇总到 `Wait`／`Close`）。

## 回到源码与下一步

建议的阅读顺序：

1. [scope/key.go](../../scope/key.go) 的 `ChainOf` 与 `linkParent`——先建立“一个关系、两个方向”的认识。
2. [scope/entries.go](../../scope/entries.go) 的两张表——看顺序为什么必须显式保存。
3. [scope/layers.go](../../scope/layers.go) 的 `Merge`（遮蔽与位置）与 `Effect`（可见性＝所有权）。
4. [scope/carrier.go](../../scope/carrier.go) 的 `Admit`——向上准入的完整规则。
5. 对照 `packages/core/scope/src/store.ts` 的 `chainLayers`、`merge` 与 `effect`。

对应测试：`TestSiblingScopesDoNotLeak`（兄弟不串味）、`TestEventAdmissionFlowsUpTheChain`（方向）、`TestLayersMergeShadowsInPlace`（遮蔽与位置）、`TestLayersReadsNeverCreateLayers`（读不创建层）、`TestBindParentIsOnceAndCycleChecked`（单次绑定与环）、`TestLayersEffectOwnsContributionByRegistrationScope`（可见性＝所有权）、`TestLayersReclaimKeepsANonEmptyAggregate`（回收以聚合为空为条件）。

读完你应该能回答：**这个机制解决什么问题**——把“谁能看见我的注册”和“谁负责释放它”变成同一个决定；**谁拥有资源**——作用域的 fiber 拥有通过它上下文注册的一切，`Dispose` 一次释放；**为什么这样表达**——身份必须是值身份（否则两代同 id 分不开），顺序必须显式保存（否则全局顺序承诺失效），失败必须落在调用点（否则一条没登记的贡献会在清理时才报错）。

下一步是 [C06 消息模型](../llm/go-primer.md)：Agent 拥有可见层之后，它要读写的对话数据长什么样。
