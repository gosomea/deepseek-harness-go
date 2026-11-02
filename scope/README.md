---
description: "给一个 Agent 独立的注册世界：作用域身份、父子链、按名遮蔽的贡献层与事件路由。"
kind: "package-library"
---

# scope

## 概述

`scope` 回答一个问题：当同一个进程里跑着多个 Agent 时，怎么让 A 注册的工具和提示词**只对 A 可见**，并且在 A 结束时恰好释放 A 的那一份？

它给每个 Agent 一个**不透明的身份**（一个 key）和一段**属于该身份的注册期**。子作用域看得见祖先的贡献、最近的定义胜出；祖先能观察后代的派发，反向不行。通过作用域注册的一切都随它释放。

这里要强调一件事：`scope` **不是** Cordis 的服务隔离。`cordis.Context.Isolate(name, scope)` 划分的是**某一个服务**的解析命名空间；作用域给注册打标记、把身份连成链、路由事件并且拥有释放权。把作用域上下文交出去，同时也交出了铸造它的插件的注入服务，所以作用域既不放宽也不收紧服务可见性。两者正交，可以叠加。

作用域路由的是**受信任的进程内插件**。它不是沙箱，不是权限边界。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

本包负责四件事：用 `Create` 铸造带身份的作用域；用 `BindParent`／`ChainOf` 表达并读取父子链；用 `NamedEntries`／`AnonymousEntries`／`Layers` 保存按作用域分层的贡献；用 `Carrier` 让一次事件派发只到达该看到的监听器。

它不负责：定义贡献**是什么**（那是注册表自己的事）、调用模型、拥有任何领域对象，也不提供反向指向 Agent 的指针。`Layers` 的层类型 `L` 由调用方给出，本包只拥有链与生命周期。

## 使用

在仓库根目录保持终端位置，把下方完整程序保存为 `bin/scope-demo.go`（先创建 `bin` 目录），执行 `go run ./bin/scope-demo.go`。

```go runnable=scope-visibility
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/scope"
)

// agent stands in for a live agent handle. Scope identity is this value's
// identity, so two agents that share a name still route independently.
type agent struct{ name string }

// layer is one scope's aggregate contribution: a named tool table plus an
// anonymous guard table.
type layer struct {
	tools  *scope.NamedEntries[string]
	guards *scope.AnonymousEntries[string]
}

// newLayer builds an empty layer for one scope.
func newLayer(owner scope.Key) *layer {
	return &layer{
		tools:  scope.NewNamedEntries[string](nil),
		guards: scope.NewAnonymousEntries[string](),
	}
}

// IsEmpty reports whether every table in this layer is empty.
func (l *layer) IsEmpty() bool { return l.tools.IsEmpty() && l.guards.IsEmpty() }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root := cordis.New()
	defer func() { _ = root.Close(context.Background()) }()

	// One store of per-scope layers, plus one shared global tool.
	layers := scope.NewLayers[*layer](newLayer, nil)
	globalCleanup, err := layers.Effect(root, func(shared *layer) (func(), error) {
		return shared.tools.Insert("read", "global read")
	})
	if err != nil {
		return err
	}
	defer func() { _ = globalCleanup() }()

	alice, bob := &agent{name: "alice"}, &agent{name: "bob"}
	aliceScope, err := scope.Create(root, alice)
	if err != nil {
		return err
	}
	bobScope, err := scope.Create(root, bob, scope.WithParent(alice))
	if err != nil {
		return err
	}

	// Alice adds a tool that shadows the global one by name.
	if _, err := layers.Effect(aliceScope.Ctx(), func(own *layer) (func(), error) {
		return own.tools.Insert("read", "alice read")
	}); err != nil {
		return err
	}
	// Bob adds a tool of his own. He is Alice's child, so he also sees hers.
	if _, err := layers.Effect(bobScope.Ctx(), func(own *layer) (func(), error) {
		return own.tools.Insert("grep", "bob grep")
	}); err != nil {
		return err
	}

	pick := func(l *layer) *scope.NamedEntries[any] {
		converted := scope.NewNamedEntries[any](nil)
		for _, entry := range l.tools.Entries() {
			_, _ = converted.Insert(entry.Name, entry.Value)
		}
		return converted
	}

	fmt.Printf("alice sees: %v\n", namesOf(layers.Merge(alice, pick)))
	fmt.Printf("bob sees:   %v\n", namesOf(layers.Merge(bob, pick)))
	if value, ok := layers.Merge(bob, pick).Get("read"); ok {
		fmt.Printf("bob read resolves to: %v\n", value)
	}

	// Which listeners hear a dispatch aimed at Bob? A listener's tag is the scope
	// key it was registered under; nil is an untagged (global) listener.
	toBob := scope.NewCarrier(bob)
	fmt.Printf("bob dispatch reaches alice's listener: %t\n", toBob.Admit(alice))
	fmt.Printf("bob dispatch reaches bob's listener:   %t\n", toBob.Admit(bob))
	fmt.Printf("bob dispatch reaches a global listener: %t\n", toBob.Admit(nil))
	toAlice := scope.NewCarrier(alice)
	fmt.Printf("alice dispatch reaches bob's listener: %t\n", toAlice.Admit(bob))

	// Disposing Bob releases only Bob's contribution.
	if err := bobScope.Dispose(); err != nil {
		return err
	}
	fmt.Printf("after bob disposes, bob sees: %v\n", namesOf(layers.Merge(bob, pick)))
	fmt.Printf("alice's read survives:        %v\n", namesOf(layers.Merge(alice, pick)))
	if err := aliceScope.Dispose(); err != nil {
		return err
	}
	return nil
}

// namesOf lists the names of a merged view in presented order.
func namesOf(view *scope.NamedEntries[any]) []string { return view.Names() }
```

预期输出：

```text output=scope-visibility
alice sees: [read]
bob sees:   [read grep]
bob read resolves to: alice read
bob dispatch reaches alice's listener: true
bob dispatch reaches bob's listener:   true
bob dispatch reaches a global listener: true
alice dispatch reaches bob's listener: false
after bob disposes, bob sees: [read]
alice's read survives:        [read]
```

三行是重点：

- `bob sees: [read grep]`：Bob 是 Alice 的子作用域，所以看得见 Alice 的 `read`，又加上自己的 `grep`。
- `bob read resolves to: alice read`：`read` 这个名字在全局层也存在，但 Bob 的链上最近的 Alice 层遮蔽了它。遮蔽**保留全局的名字位置**，不改变呈现顺序。
- `alice dispatch reaches bob's listener: false`：事件只**向上**流动。Alice 听不见 Bob 的派发，而 Bob 听得见 Alice 的——因为 Bob 的链上包含 Alice，反向则不然。`nil` 标签表示全局监听器，任何派发都能到达它。

注意“监听器的标签”就是它注册时所属的**作用域 key**，不是任意指针。传一个与链无关的值给 `Admit` 会（正确地）被拒绝。

`Layers.Effect` 的 action **立即执行**，不是等到释放时才跑。所以一次被拒绝的注册在调用点就报错，而不是稍后从一个清理里冒出来。返回的 disposer 撤销这一次插入，并在整层聚合为空时回收该层。

## 理解实现

<details>
<summary>一个父子关系怎样同时支撑两个方向</summary>

参考实现用**一对** `WeakMap`：一个记录 key 的父链，一个记录 carrier 与 key 的对应。本包把两者都做成显式表，因为 Go 没有 weak map。

链只存一次，却支撑两个方向：

- **注册视图向下继承**：`Layers.Chain(scope)` 走 `ChainOf` 再反转，所以最远的祖先最先应用、本作用域最后应用，自然形成“最近的定义胜出”。
- **事件准入向上扩展**：`Carrier.Admit` 检查监听器的标签是否是派发 key **或它的祖先**。

所以 `ChainOf` 是唯一的关系来源，`Chain` 和 `Admit` 只是它的两个读取方向。这也解释了为什么 `Peek` 被刻意做成**不看链**的：一个注册表要问“我自己这一层有什么限制”时，绝不能顺带看到祖先的限制。

**绑定只能做一次。** `BindParent` 成功后返回一个 `ParentBinding`；只有持有它的人可以 `Rebind`。理由不是权限洁癖，而是可见性：没有任何人知道在旧父链下已经产生了什么，所以不能允许别人随便改链。每次写入都会走一遍父链检查环，否则所有遍历都不会终止。

</details>

<details>
<summary>Go 在哪里比参考更弱</summary>

三处必须如实记录，不能假装等价：

1. **编译期 brand 无法表达。** 参考用 `Scoped<T>` 让“用裸 subject 派发一个作用域事件”成为编译错误。Go 没有 `this` 类型态，所以本包用 `Carrier` 承载路由状态，并在 `CheckDispatch` 里做两项运行时检查（缺 carrier、carrier 的 key 与事件参数不符）。这两项检查在参考里由 invariant companion 完成，是唯一的防线，所以不能省。
2. **没有弱引用。** 参考的 `WeakMap` 让不再可达的 key 被 GC 回收。Go 的表会一直留着，所以 `Scope.Dispose` 显式删除父链与 carrier 条目。一个**从未被 Dispose 的** key 会长期驻留——这是真实的保留差异，不是可以声称补上的缺口。
3. **上下文没有原型继承。** 参考的 `scopeOf(ctx)` 沿 JS 原型链读到最近的标记。Go 的 Context 不继承属性，所以本包用一张显式的 `contextKeys` 表记录 `Create` 铸出的上下文。经过**再嵌套一层**插件得到的上下文不会自动带标记，需要显式再 `Create` 一次（用 `WithParent` 表达归属）。这一点写在[限制](#限制)里，不隐藏在实现注释中。

</details>

## 进一步阅读

从[从 Go 理解 Agent 可见层](../docs/scope/go-primer.md)开始：它从“两个 Agent 同时跑，怎么不让工具串味”这个问题进入，逐步走到链、遮蔽与释放。

要回到固定参考，看 `packages/core/scope/src/index.ts`（`createScope`、`scopeOf`、`scopeTarget`、`bindScopeParent`）、`packages/core/scope/src/store.ts`（`ScopedLayers`、`NamedEntries`、`AnonymousEntries`）与 `packages/core/scope/README.md`。固定提交为 `00102833dfaee1da9f48a3a8eae9d34005a75218`。

## 验证

从项目根目录运行 `go test -race -count=1 ./scope` 验证本包；阶段交付执行 `make check`。`scope` 已登记在[覆盖门禁](../scripts/doccheck/main.go)的受控清单中，语句覆盖率下限为 90%；上面的完整程序由 `make doc-examples` 执行并逐字比较输出。行为测试集中在兄弟作用域不串味、链的两个方向、遮蔽与位置、单次绑定与环拒绝、以及贡献随作用域释放。

## 限制

- **不是沙箱、不是权限边界。** 作用域路由受信任的进程内插件；把作用域上下文交出去，同时交出了铸造插件的注入服务。
- **不等于 Cordis 服务隔离。** 服务可见性由 `cordis.Context.Isolate` 决定，与本包无关。
- **没有弱引用。** 见上文；未被释放的 key 会长期驻留。
- **没有编译期 brand。** 用错 carrier 只能由 `CheckDispatch` 在运行期发现。
- **再嵌套的上下文不带标记。** 通过 `Create` 铸造的作用域上下文带标记；在它之上再注册一层插件得到的上下文需要显式再 `Create`。
- **`Layers` 不拥有层的语义。** `L` 由调用方定义，本包只调用 `IsEmpty` 并维护链与回收；层里放什么、怎么合并（`Merge` 的 `pick`）都是调用方的契约。
- **没有生成的 scoped-event 表。** 参考用生成器从源码推导“哪些事件是作用域过滤的、subject 在哪个参数”。本包不手抄那张表，也不提供生成器；需要时由调用方显式调用 `CheckDispatch`。谁需要生成器属于后续阶段。
- **`contextKeys`、`parentOf`、`carriers` 是包级表**，不是每实例状态。多个运行时共享它们，因此 key 必须全局唯一（正是这一点让指针身份成为要求）。
