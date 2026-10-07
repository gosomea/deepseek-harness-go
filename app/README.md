---
description: "用命名 bundle 与 profile 组合出一棵插件树，并区分必要条目失败与兄弟条目失败。"
kind: "package-library"
---

# app

## 概述

你可以把一组配置补丁命名成一个 bundle，再让 profile 按顺序列出要应用的 bundle。组合从空条目表开始，逐层应用；后一层覆盖前一层对同一条目的字段，因此 profile 自己是最具体的一层，优先级最高。组合结果是一份普通的条目声明，交给 `loader` 挂载。

本包还负责就绪判定：profile 标记为必需的条目必须真正可用，未被标记的兄弟条目只报告不阻断。这样宿主可以区分「profile 不可用」和「profile 部分可用」，而不必把任何插件失败都当致命错误。

本包只支持显式编译期注册：bundle 是宿主注册的 Go 值，不提供 npm 动态安装、模块 HMR 或原 profile 兼容。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

本包负责三件事：把命名 bundle 按 profile 声明的顺序解析成层；按顺序应用每层的补丁，产生一份组合后的条目声明；组合并挂载之后报告哪些条目可用、哪些只是等待、哪些是必需而未就绪。它不负责解析插件名、构造插件或管理实例生命周期——那些属于 `loader`。

## 使用

在仓库根目录保持终端位置，将下方完整程序保存为 `bin/app-demo.go`（先创建 `bin` 目录），执行 `go run ./bin/app-demo.go`。它注册一个 bundle 和一个必需要失败的条目，打印组合结果与就绪判定。

```go runnable=app-profile
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
	err := bundles.Register(app.Bundle{
		Name: "base",
		Patches: []app.Patch{
			{Insert: []loader.Options{
				{ID: "healthy", Name: "ok"},
				{ID: "broken", Name: "boom"},
			}},
			// A patch for an id no layer declares is reported, not silently ignored.
			{ID: "typo"},
		},
	})
	if err != nil {
		return err
	}

	profile, err := app.ParseProfile([]byte(`{"name":"demo","bundles":["base"],"required":["healthy"]}`))
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
	return nil
}
```

预期输出：

```text output=app-profile
active: [healthy]
siblings: 1
skipped: typo
```

`registered` 的 bundle 是逐个注册的：空名称与重复名称都会被拒绝，`Lookup` 对未注册名称返回包装 `ErrUnknownBundle` 的错误。未匹配任何条目的补丁会出现在 `Composition.Skipped`，并用 `errors.Is(err, app.ErrPatchSkipped)` 判断。

## 理解实现

<details>
<summary>为什么嵌套条目必须一起重编码</summary>

组合过程中，一个分组条目的子条目以真实的内存节点存在，而不是每次查找都从 JSON 载荷里解出一份新切片。后者会让「补丁改到嵌套条目」写进一份马上被丢掉的副本——补丁看起来成功，实际什么都没变。组合结束时才把节点树重新编码回 JSON 载荷，因此每一层的覆盖都真正到达最终结果。

[bundle.go](bundle.go) 负责层与补丁的应用，[profile.go](profile.go) 负责层顺序与就绪判定。

</details>

## 进一步阅读

先看 [profile 的层与就绪](../docs/loader/profile.md)，它从「配置来源为什么要有优先级」进入。要回到 DSH 的实现，看固定提交的 [packages/boot/app-boot/src/profile.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/boot/app-boot/src/profile.ts) 与 [vendor/include/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/include/src/index.ts) 的 `applyEntryPatches`。

## 验证

从项目根目录运行 `go test -race -count=1 ./app` 验证本包；阶段交付执行 `make check`。`app` 已登记在[覆盖门禁](../scripts/doccheck/main.go)的受控清单中，语句覆盖率下限为 90%；上述完整程序也由 `make doc-examples` 执行并比较输出。覆盖层语义的固定 fixture 在 [testdata/parity/loader/profile-overlay.json](../testdata/parity/loader/profile-overlay.json)。

## 限制

本包只支持显式编译期注册：不提供 npm 动态安装、模块 HMR 或原 profile 兼容。YAML include、`!!js` 惰性表达式、配置热重载与原 DSH profile 文件兼容属 M12.2，不在本阶段声称。

补丁只做替换：文档无法区分「字段缺失」与「字段被清空」，因此更新不会把字段恢复成缺失。补丁按 id 定位条目，找不到匹配时跳过并记录诊断，不会新建条目——新增只能通过 `insert`。本包不执行插件代码，也不加载任何模块。
