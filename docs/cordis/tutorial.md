# 写第一个 Cordis 插件

## 概述

本教程面向掌握 Go 函数、接口和错误处理的读者。你会先启动和关闭一个插件，再增加必需服务，最后移除和恢复提供者，观察同一个消费者实例重新激活。先阅读[概念对照](go-primer.md)可以区分 Plugin、Fiber、激活与 Context。

## 目录

- [准备与执行](#准备与执行)
- [1. 启动一个插件](#1-启动一个插件)
- [2. 加入接口与必需依赖](#2-加入接口与必需依赖)
- [3. 移除并恢复提供者](#3-移除并恢复提供者)
- [4. 在激活中注册事件](#4-在激活中注册事件)
- [诊断与下一步](#诊断与下一步)

## 准备与执行

安装 Go 1.22 或更新版本，进入包含 `go.mod` 的项目根目录。先创建 `bin` 目录，再将每一步的完整程序保存为 `bin/tutorial.go`，下一步替换同一个文件。保持终端在项目根目录并执行：

```sh
go run ./bin/tutorial.go
```

`bin` 用于本机练习并被 Git 忽略。每个程序都是独立的 `package main`，可以直接运行；教程中的完整程序也由 `make doc-examples` 在临时目录执行并比较输出。

## 1. 启动一个插件

Apply 在插件激活时运行，返回的 Cleanup 在释放这次激活时运行。宿主创建根 Context 并负责 Close；这里用具名返回值把执行与清理错误合并，main 统一输出错误。插件定义中的 Name 用于诊断。

```go runnable=plugin
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

```text output=plugin
hello started
hello stopped
```

这里的 ctx 是 `*cordis.Context`；Go 标准库的 `context.Background()` 为 Close 提供等待参数。插件无需依赖其他服务，所以立即启动。Apply 和 Cleanup 内不要调用 Wait/Close，它们正是宿主要等待的工作，安全条件见[并发与等待](lifecycle.md#并发与等待)。

## 2. 加入接口与必需依赖

用一个 `counter` 接口描述消费者需要的能力，`memoryCounter` 提供内存实现。服务键把名称与预期接口关联起来；Inject 声明运行时依赖，Resolve 获得实现。先注册消费者会得到 Pending，提供者成功激活后消费者才运行 Apply。

用下方完整程序替换第 1 步的文件并运行：

```go runnable=dependency
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

type counter interface{ Next() int }
type memoryCounter struct{ value int }

func (c *memoryCounter) Next() int { c.value++; return c.value }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (result error) {
	root := cordis.New()
	defer func() { result = errors.Join(result, root.Close(context.Background())) }()
	key := cordis.NewKey[counter]("counter")

	consumer, err := root.Plugin(cordis.Plugin{
		Name:   "greeter",
		Inject: []string{key.Name()},
		Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
			service, err := cordis.Resolve(ctx, key)
			if err != nil {
				return nil, err
			}
			fmt.Println("counter value:", service.Next())
			return func() error {
				fmt.Println("greeter stopped")
				return nil
			}, nil
		},
	}, nil)
	if err != nil {
		return err
	}
	fmt.Println("before provider:", consumer.State())

	provider := cordis.Plugin{
		Name: "counter",
		Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
			_, err := cordis.Provide[counter](ctx, key, &memoryCounter{})
			return nil, err
		},
	}
	if _, err := root.Plugin(provider, nil); err != nil {
		return err
	}
	fmt.Println("after provider:", consumer.State())
	return nil
}
```

预期输出：

```text output=dependency
before provider: pending
counter value: 1
after provider: active
greeter stopped
```

`provider` 是可复用的插件定义。`root.Plugin(provider, nil)` 创建提供者实例；`consumer` 是另一次注册返回的 Fiber。Key 的泛型参数约束消费类型，但运行时使用服务名和 Scope 查找绑定；同名且不同类型不会成为两个独立服务。

尝试将 Inject 的列表改为 `[]string{"missing-counter"}`：程序只打印 `before provider: pending` 和 `after provider: pending`，greeter 的 Apply/Cleanup 都没有运行。改回 `[]string{key.Name()}` 后恢复上述输出。缺少服务是可恢复的等待状态；配置或 Apply 错误是另一类结果，见[启动与更新](lifecycle.md#启动与更新)。

## 3. 移除并恢复提供者

现在让提供者的注册结果保存在 `p` 中。调用 `p.Dispose()` 永久移除这个 Fiber；依赖它的消费者释放本次激活并回到 Pending。再次用 `provider` 定义注册，会创建新的提供者 Fiber，并使原 `consumer` 再次执行 Apply。

<details>
<summary>替换练习文件的完整程序</summary>

```go runnable=recovery
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

type counter interface{ Next() int }
type memoryCounter struct{ value int }

func (c *memoryCounter) Next() int { c.value++; return c.value }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (result error) {
	root := cordis.New()
	defer func() { result = errors.Join(result, root.Close(context.Background())) }()
	key := cordis.NewKey[counter]("counter")

	consumer, err := root.Plugin(cordis.Plugin{
		Name:   "greeter",
		Inject: []string{key.Name()},
		Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
			service, err := cordis.Resolve(ctx, key)
			if err != nil {
				return nil, err
			}
			fmt.Println("counter value:", service.Next())
			return func() error {
				fmt.Println("greeter stopped")
				return nil
			}, nil
		},
	}, nil)
	if err != nil {
		return err
	}
	fmt.Println("before provider:", consumer.State())

	provider := cordis.Plugin{
		Name: "counter",
		Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
			_, err := cordis.Provide[counter](ctx, key, &memoryCounter{})
			return nil, err
		},
	}
	p, err := root.Plugin(provider, nil)
	if err != nil {
		return err
	}
	fmt.Println("after provider:", consumer.State())

	if err := p.Dispose(); err != nil {
		return err
	}
	fmt.Println("after removal:", consumer.State())
	if _, err := root.Plugin(provider, nil); err != nil {
		return err
	}
	return nil
}
```

预期输出：

```text output=recovery
before provider: pending
counter value: 1
after provider: active
greeter stopped
after removal: pending
counter value: 1
greeter stopped
```

</details>

两次 counter value 都是 1，因为新的提供者创建了新的 `memoryCounter`。consumer 的 Fiber 保留，第二次 Apply 获得新的激活 Context；旧 Context 不应拿来注册新资源。最后一次 greeter stopped 来自宿主关闭根树。Provider 清理前，注入消费者先退出；[生命周期](lifecycle.md#清理顺序与所有者)维护准确顺序。

## 4. 在激活中注册事件

下一步运行仓库已有的[组合示例](../../examples/cordis/README.md)：

```sh
go run ./examples/cordis
```

它把消费行为放到 ready 事件监听器中，而不在 Apply 中直接增加计数。监听器捕获本次激活的 counter；提供者消失时，On 登记的监听器自动移除，“not delivered”不会打印。新提供者出现后，Apply 注册新的监听器。完整代码和输出由示例 README 维护；分发模式由[事件参考](events.md)定义。

## 诊断与下一步

在这些串行示例中，注册返回后即可观察结果。回调内或并发发生的变更可能排队，外部宿主应调用 `root.Wait(ctx)` 后查看状态与错误，不能把一次注册返回当作所有并发工作的结算。[开发说明](../development.md#调试生命周期)介绍 Pending、Failed 与错误定位。

计数器只用于串行访问。需要后台 goroutine 时，监听 `ctx.GoContext().Done()` 并在 Cleanup 中等待其退出；需要并发事件时，为共享状态增加同步。继续阅读[服务与作用域](services.md)、[生命周期](lifecycle.md)和[源码对应表](source-map.md)，学习隔离、失败回滚和 Go/TypeScript 差异。
