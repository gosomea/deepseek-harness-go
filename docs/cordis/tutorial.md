# 写第一个 Cordis 插件

## 先运行

在项目根目录执行：

```sh
go run ./examples/cordis
```

你会看到 greeter 先等待 counter，counter 出现后处理 hello，counter 被移除时 greeter 释放监听器，新的 counter 出现后 greeter 重新运行。逐字输出见 [expected.txt](../../examples/cordis/expected.txt)。

## 1. 定义消费者需要的能力

示例的 `counter` 接口只有 `Next() int`，`memoryCounter` 实现它。`cordis.NewKey[counter]("counter")` 把服务名称与消费侧类型关联起来。Provider 可以替换具体类型，只要满足这个接口。

## 2. 创建根容器

`cordis.New()` 建立根 Context。宿主拥有根容器，退出时调用 `root.Close(context.Background())` 并处理返回错误。示例在 defer 中汇总执行与清理错误。

## 3. 注册 Consumer

greeter 插件在 `Inject` 中声明 counter。此时 counter 尚未提供，Fiber 保持 Pending，Apply 尚未运行。它不会悄悄使用一个默认实现；注册本身成功，因为缺少服务是可恢复的等待状态。

## 4. 注册 Provider

counter 插件在 Apply 内创建内存计数器，并用 `cordis.Provide[counter](ctx, key, value)` 提供服务。服务归这个插件激活所有，成功激活后才能满足 greeter 的依赖。

## 5. 使用服务并监听事件

greeter 的 Apply 用 `cordis.Resolve(ctx, key)` 得到接口，再调用 `ctx.On("ready", ...)` 注册监听器。监听器捕获此次激活的服务。示例用同步 `Emit` 发出 hello，计数器返回 1。

Apply 返回一个 Cleanup，打印 stopped。On 登记的监听器已自动归该激活所有，不需要在 Cleanup 中再手工移除。

## 6. 移除与恢复

`provider.Dispose()` 永久删除这个 Provider 实例。greeter 所需服务失效后先取消、清理，再回到 Pending。此时再次 Emit 不会打印消息。注册新的 Provider 后，greeter 的 Apply 重新执行，创建新的监听器；新计数器再次从 1 开始。

完整实现与测试见 [main.go](../../examples/cordis/main.go) 和 [main_test.go](../../examples/cordis/main_test.go)。后续细节从 [生命周期](lifecycle.md)、[服务](services.md) 和 [事件](events.md) 继续阅读。
