---
description: "用一份 JSON 配置按名称装配插件，让配置条目的身份与它构造出的插件分开管理。"
kind: "package-library"
---

# loader

## 概述

你可以用一份 JSON 配置文件列出要运行哪些插件，让宿主在编译期注册的插件里按名称找到对应实现。配置条目（`Entry`）有自己的稳定身份，和它构造出来的插件是两件事：条目记录文档写了什么，插件是被构造出来的具体实现。当前切片覆盖条目身份、工厂目录和 JSON 编解码；把条目挂进父子树并应用属于后续的 M2 切片。

本包只支持显式编译期注册：不提供 npm 动态安装、模块 HMR 或原 profile 兼容。命令行入口见 [cmd/dsh-go](../cmd/dsh-go/README.md)，完整操作教程见 [从配置到运行中的插件树](../docs/loader/tutorial.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

本包负责三件事：把 JSON 文档解析成原始配置条目；用`Catalog`按名称解析出插件的工厂；让插件自己校验它的配置。它不负责挂载条目、启动插件或管理生命周期资源——那些属于 `cordis` 运行时，由后续切片接入。配置的原始值与校验结果是两个概念：`Options` 保存文档原文，`Entry.Config` 保存插件校验后的值。

## 使用

在仓库根目录保持终端位置，将下方完整程序保存为 `bin/loader-demo.go`（先创建 `bin` 目录），执行 `go run ./bin/loader-demo.go`。它注册一个插件，用 JSON 文档装配两个条目，并打印解析结果。

```go runnable=loader-load
package main

import (
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
	err := catalog.Register("greeter", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "greeter",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				return nil, nil
			},
			Validate: func(config any) (any, error) {
				// The plugin owns its own configuration rules.
				if config == nil {
					return "guests", nil
				}
				return config, nil
			},
		}
	})
	if err != nil {
		return err
	}

	document := []byte(`{"entries":[
	  {"id":"main","name":"greeter","config":"world"},
	  {"id":"off","name":"greeter","disabled":true}
	]}`)

	entries, err := loader.Load(document, catalog)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		fmt.Printf("entry %s name=%s runnable=%t disabled=%t config=%v\n",
			entry.ID(), entry.Name(), entry.Runnable(), entry.Disabled(), entry.Config())
	}
	fmt.Println("registered plugins:", catalog.Names())
	return nil
}
```

预期输出：

```text output=loader-load
entry main name=greeter runnable=true disabled=false config=world
entry off name=greeter runnable=false disabled=true config=<nil>
registered plugins: [greeter]
```

`catalog.Register` 拒绝空名称和 nil 工厂；`loader.Load` 对未知插件名返回包装 `ErrUnknownPlugin` 的错误，对重复 id 返回 `ErrDuplicateID`，对未知字段或类型不符返回 `ErrInvalidConfig`。三类失败互不混淆，宿主可以分别报告。[Go 概念对照](../docs/loader/go-primer.md)解释这些错误为什么必须分开。

## 理解实现

<details>
<summary>为什么配置条目不是插件</summary>

文档里的 `id` 是条目的身份，`name` 只是查工厂用的键。同一个 `name` 可以出现在多个条目里，各自有自己的 `id`、配置和禁用状态；而插件实例是解析时才构造出来的。把两者混成一个类型，就无法表达"禁用"——被禁用的条目仍然有效，但它不该构造插件。

解析分三步：[config.go](config.go) 解析文档并拒绝未知字段与重复 id；[catalog.go](catalog.go) 按名称给出工厂；[entry.go](entry.go) 构造插件、把原始配置交给插件自己的 `Validate`，再记录校验结果。禁用条目和分组条目跳过工厂解析，因为它们本来就不运行。

</details>

## 进一步阅读

先看 [Go 概念对照](../docs/loader/go-primer.md)，它从"为什么需要配置"进入 Entry 与工厂。要回到 DSH 的实现，看固定提交的 [vendor/loader/src/config/entry.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/entry.ts) 与 [vendor/loader/src/config/tree.ts](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src/config/tree.ts)。

## 验证

从项目根目录运行 `go test -race -count=1 ./loader` 验证本包；阶段交付执行 `make check`。`loader` 已登记在[覆盖门禁](../scripts/doccheck/main.go)的受控清单中，语句覆盖率下限为 90%；上述完整程序也由 `make doc-examples` 执行并比较输出。[测试说明](../docs/testing.md)维护检查要求。

## 限制

本包只支持显式编译期注册：不提供 npm 动态安装、模块 HMR 或原 profile 兼容。它没有实现 YAML include、`!!js` 惰性表达式、per-entry isolate 或 `Context.intercept` 分层配置合并；那些按归属在后续切片处理，YAML、惰性表达与原 profile 兼容属 M12.2。

当前切片不挂载条目：`Load` 只解析和构造，不调用 `Apply`、不注册服务、不产生任何副作用。父子树、分组、按 id 更新与启停属于 M2.2–M2.3。配置输入形式目前只有 JSON；patch 替换与合并、默认值语义尚未定义。
