# 从 Go 理解 Bundle、Profile 与就绪判定

## 读完要解决的问题

你已经有一棵能装配、能更新、能关闭的插件树。接下来的问题是：**同一份配置要分很多来源时，谁覆盖谁？** 以及启动之后，怎么判断这棵树可用——是所有插件都必须成功，还是只有一部分必须成功？

这一篇进入 DSH 的 bundle / profile 分层与 required 语义。前置知识是前三篇的 Entry、装配树与更新。关联行为 ID 见 [M2 契约](../10-plans/dsh-go-replication-m02/plans.md)；读完你能解释：层顺序为什么等于优先级，以及必需和可选在启动判定上的差别。

## 从熟悉的 Go 写法出发

普通 Go 做法是写一份配置结构体，谁需要就传进去：

```go fragment
type Config struct {
	Level int
	Label string
}
```

它简单直接。边界在于：同一份配置往往来自多个地方——基础发行版提供默认值，某个功能包补充字段，用户再按自己环境改。如果只有一份结构体，就得在代码里手写「如果用户没填就用默认」的合并逻辑，而且每加一个来源就要改一次。

DSH 的做法是把每一层声明成**补丁列表**，按顺序叠加：基础 bundle 先放条目，功能 bundle 改其中几个字段，profile 自己最后覆盖。**后应用的层覆盖先应用的层**，所以「层的顺序」就是「优先级」。Go 侧对应 [app/bundle.go](../../app/bundle.go)（`Bundle`、`Patch`、`Compose`）与 [app/profile.go](../../app/profile.go)（`Profile`、`Resolve`、`Mount`）。

哪里不等价：DSH 的 bundle 是 npm 包，通过 `package.json` 的 `dsh.bundle.patch` 字段声明补丁文件路径，支持 YAML 与 `!!js` 惰性表达式；Go 侧 bundle 是宿主注册的普通值，只支持 JSON，不支持动态安装。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面这个完整程序用三层补丁组合出一棵树，并展示必需与兄弟条目的差别。

```go runnable=profile-layers
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/app"
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
	plugins := loader.NewCatalog()
	if err := plugins.Register("ok", func() cordis.Plugin {
		return cordis.Plugin{
			Name:  "ok",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, nil },
		}
	}); err != nil {
		return err
	}
	if err := plugins.Register("boom", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "boom",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				return nil, errors.New("apply failed")
			},
		}
	}); err != nil {
		return err
	}

	bundles := app.NewCatalog()
	// Layer 1: the base bundle declares the entries.
	err := bundles.Register(app.Bundle{
		Name: "base",
		Patches: []app.Patch{
			{Insert: []loader.Options{{ID: "healthy", Name: "ok"}, {ID: "broken", Name: "boom"}}},
		},
	})
	if err != nil {
		return err
	}
	// Layer 2: a feature bundle targets a missing id.
	err = bundles.Register(app.Bundle{
		Name: "feature",
		Patches: []app.Patch{
			{ID: "never-declared"},
		},
	})
	if err != nil {
		return err
	}

	// The profile is the last layer, so it wins over both bundles.
	profile, err := app.ParseProfile([]byte(`{"name":"demo","bundles":["base","feature"],"required":["healthy"]}`))
	if err != nil {
		return err
	}

	ready, err := app.Mount(context.Background(), profile, bundles, plugins)
	if err != nil {
		return err
	}
	defer func() { _ = ready.Close(context.Background()) }()

	fmt.Println("active:", ready.Active)
	fmt.Println("siblings:", len(ready.Siblings))
	fmt.Println("skipped:", ready.Composition.Skipped[0].ID)

	// Marking the failing entry required blocks readiness instead.
	profile, err = app.ParseProfile([]byte(`{"name":"demo","bundles":["base","feature"],"required":["healthy","broken"]}`))
	if err != nil {
		return err
	}
	blocked, err := app.Mount(context.Background(), profile, bundles, plugins)
	fmt.Println("blocked:", errors.Is(err, app.ErrRequiredEntryFailed))
	_ = blocked.Close(context.Background())
	return nil
}
```

预期输出：

```text output=profile-layers
active: [healthy]
siblings: 1
skipped: never-declared
blocked: true
```

退出状态为 0。观察重点：

- 两个 bundle 与 profile 依次应用，后一层可以改到前一层插入的条目。
- `never-declared` 这一层没有匹配到任何条目，出现在 `Skipped` 里——**错误配置不会被静默忽略**。
- `broken` 的 `Apply` 失败，但它不是必需的，所以进 `Siblings` 而 `Mount` 返回 nil；把 `broken` 加进 `required` 后，`Mount` 返回包装 `ErrRequiredEntryFailed` 的错误。

## 先预测，再观察

**预测**：如果把 `bundles` 的顺序从 `["base","feature"]` 改成 `["feature","base"]`，会发生什么？

观测方法：改掉 profile 声明后重跑，看 `Skipped` 的长度与内容。

结果与解释：`feature` 层先应用时，它要修改的 id 还不存在——`base` 还没插入任何条目。于是 `feature` 的补丁会进 `Skipped`。这说明**层顺序既是优先级也是依赖顺序**：一个层只能修改它之前层已经声明的条目，不能提前声明。**恢复到正常场景**：把顺序改回 `["base","feature"]`。

再预测一个：如果同一个 id 被两层先后设置 `config`，最终生效的是哪一层？答案是列表中靠后的那一层，也就是**后应用的层覆盖先应用的层**；profile 自己的 `patches` 更在最后，所以它优先级最高。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | `dsh.profile.bundles` 给出 bundle 的有序列表；每个 bundle 通过 `dsh.bundle.patch` 声明补丁文件；补丁按 id 定位条目并覆盖字段；`insert` 把条目追加到分组或根；模板条目与 installer 管理的条目分别保留或自动激活 |
| TypeScript 入口 | [packages/boot/app-boot/src/profile.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/boot/app-boot/src/profile.ts)（`ProfileLayer`、`bundlePatchPaths`、bundle 顺序）与 [vendor/include/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/include/src/index.ts)（`applyEntryPatches`、`buildMap` 索引时机） |
| Go 入口 | [app/bundle.go](../../app/bundle.go) 的 `Bundle`/`Patch`/`Catalog`/`Compose`；[app/profile.go](../../app/profile.go) 的 `Profile`/`Resolve`/`ComposeProfile`/`Mount`/`Ready` |
| 保持的行为 | 层按声明顺序应用，后层覆盖前层；补丁按 id 定位（全路径或本地 id）；`insert` 追加到分组或根；名称不匹配或找不到目标时跳过并诊断；插入的条目对同一层后续补丁可见；输入不被修改 |
| Go 表达 | 组合在真实内存节点树上进行、结束时才重编码回 JSON 载荷（避免嵌套补丁写进副本）；`ComposeResult.Skipped` 用 `errors.Join` 汇总为可 `errors.Is` 判断的诊断；`Ready` 用 `Failed`（必需）与 `Siblings`（非必需）两个列表分离两种失败 |
| 不等价边界 | 没有 YAML 补丁文件、`!!js` 惰性表达式、配置热重载、npm 安装与 pnpm 管理的依赖激活；没有 `intercept`、`isolate` 等 Loader 扩展字段；这些属 M12.2 |

## 一次操作的时序与所有权

`app.Mount(ctx, profile, bundles, plugins)` 的步骤：

1. `Resolve` 按 `bundles` 顺序查出每层的补丁；任何未注册的 bundle 名称立刻返回 `ErrUnknownBundle`。
2. `Compose` 从空条目表开始逐层应用。每层从上一层的副本继续，结束后把节点树重编码为条目声明。
3. 在挂载前，对每个声明调用 `loader.Resolve` 做一次解析校验：**未知插件名或插件拒绝的配置属于组合失败**，此时不会挂载任何东西。
4. `loader.NewTree` 建立树，逐条挂载。**激活失败不中断挂载**，而是按路径记下错误。
5. 判定就绪：必需条目未就绪记入 `Failed`（阻断）；非必需条目未就绪记入 `Siblings`（只报告）；从未组合出来的必需条目记入 `Missing`。

谁拥有并释放什么：`Ready` 拥有它启动的 `loader.Tree`，用 `Ready.Close(ctx)` 释放；未挂载成功的组合不产生 `Tree`（字段为 nil），因此没有需要释放的资源。补丁本身不拥有资源——它只改声明。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复操作 |
| --- | --- | --- | --- |
| `app: unknown bundle: "x"` | profile 列了宿主没注册的 bundle | `Catalog.Names()` | 补注册，或改 profile 的 `bundles` |
| `app: patch skipped` 且原因是 entry not found | 补丁的目标 id 在任何更早的层里都不存在 | `Composition.Skipped` | 确认层顺序；目标必须由更早的层声明 |
| `app: patch skipped` 且原因是 name mismatch | 补丁声明的 `name` 与目标当前名称不符 | 同上 | 修正 `name`，或确认目标确实是想要的条目 |
| `app: required entry failed: x` | 必需条目的插件启动失败 | `Ready.Failed` | 修插件或配置；若不致命，把它从 `required` 移除 |
| `Mount` 返回 nil 但某个条目没运行 | 该条目不是必需的，报告在 `Ready.Siblings` | `Ready.Siblings` | 按需把它加入 `required`，或接受部分可用 |

关于兄弟条目策略：必需与可选的差别就在 `Failed` 与 `Siblings` 这两个列表，`Ready.Err()` 只汇总 `Failed`。宿主因此可以决定部分可用是记录一条告警还是拒绝启动，而不需要重新检查整棵树。

## 回到源码与下一步

建议阅读顺序：先看 [app/profile.go](../../app/profile.go) 的 `Resolve`（层顺序从哪里来），再看 [app/bundle.go](../../app/bundle.go) 的 `Compose`/`composeLayer`（为什么嵌套补丁必须写进同一棵树），最后看 `Mount` 的 `Failed`/`Siblings` 分类。对应测试是 [app/profile_test.go](../../app/profile_test.go)（fixture 驱动）、[app/core_test.go](../../app/core_test.go) 与 [app/coverage_test.go](../../app/coverage_test.go)。

读完后你应该能回答：**为什么层顺序就是优先级？谁拥有资源？** 因为补丁按应用顺序覆盖；`Ready` 拥有它启动的树，补丁本身不拥有资源。

下一步是 M2.5：把配置检查入口与管理装配路径接到 CLI，并交付可运行示例。当前 M2.5 尚未实现，本页不引用还不存在的入口。
