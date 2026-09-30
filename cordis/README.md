---
description: "用 Go 接口组合插件，使服务、监听器和清理随插件依赖变化而释放或重新建立。"
kind: "package-library"
---

# Cordis Go 核心

## 概述

你可以声明插件需要的服务，让插件在依赖就绪时启动，并在依赖失效时释放本次启动获得的资源。服务实现可以替换，消费者通过 Go 接口调用它。这个通用库适合学习或嵌入插件组合；应用代码负责服务内部的并发状态和自己启动的后台工作。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

本包管理插件实例、服务注入、服务作用域、事件监听器和资源释放。`Plugin` 声明启动行为，`Fiber` 是注册返回的实例，`Context` 提供本次激活的服务访问与资源注册；[Go 概念对照](../docs/cordis/go-primer.md)解释它们与常见 Go 用法的关系。模型请求、工具执行与 Session 规则归上层模块。

## 使用

在仓库根目录保持终端位置，将下方完整程序保存为 `bin/hello.go`（先创建 `bin` 目录），执行 `go run ./bin/hello.go`。它注册一个插件，成功启动后输出 started，宿主关闭根容器时输出 stopped；错误会写到 stderr 并以非零码退出。

```go runnable=hello
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (result error) {
	root := cordis.New()
	defer func() { result = errors.Join(result, root.Close(context.Background())) }()

	_, err := root.Plugin(cordis.Plugin{
		Name: "hello",
		Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
			fmt.Println("hello started")
			return func() error {
				fmt.Println("hello stopped")
				return nil
			}, nil
		},
	}, nil)
	return err
}
```

预期输出：

```text output=hello
hello started
hello stopped
```

`root.Plugin` 返回 Fiber 和 error。缺少必需服务会保持 Pending；配置或启动失败需要检查返回错误及实例状态。[教程](../docs/cordis/tutorial.md)演示带服务的消费者；[生命周期](../docs/cordis/lifecycle.md#并发与等待)说明何时需要从回调外调用 Wait，避免在 Apply/Cleanup 中等待自身。

## 理解实现

<details>
<summary>依赖变化和资源所有权</summary>

注册创建一个 Fiber。它的依赖可用时，运行时调用 Apply，并把服务、监听器、子插件与清理函数归到本次激活。依赖失效后，消费者先释放资源；依赖恢复后，同一个 Fiber 使用新的激活 Context 再次运行 Apply。

[context.go](context.go)提供注册与等待入口，[lifecycle.go](lifecycle.go)协调变化，[service.go](service.go)管理服务绑定，[events.go](events.go)分发事件。准确的时序与资源安全条件由[生命周期](../docs/cordis/lifecycle.md)维护，类型和方法从[生成 API](../docs/cordis/api.md)查阅。

</details>

## 进一步阅读

先看[Go 概念对照](../docs/cordis/go-primer.md)，再按任务查阅[服务与作用域](../docs/cordis/services.md)、[事件分发](../docs/cordis/events.md)。希望回到 DSH 实现时使用[源码对应表](../docs/cordis/source-map.md)，可运行的组合示例在[examples/cordis](../examples/cordis/README.md)。

## 验证

从项目根目录运行 `go test -race -count=1 ./cordis` 验证本包；阶段交付执行 `make check`。[测试说明](../docs/testing.md)维护检查要求。上述完整程序也由 `make doc-examples` 执行并比较输出；公开注释变化通过 `make api` 更新生成参考。

## 限制

本包不创建模型请求或模型可见文本，因此不单列模型体验章节。它没有 TypeScript 的动态属性代理或模块热更新；完整范围见[源码对应表](../docs/cordis/source-map.md)。生命周期回调串行执行；运行时只保护自己的注册表，服务与监听器负责其共享状态，安全条件见[并发与等待](../docs/cordis/lifecycle.md#并发与等待)。
