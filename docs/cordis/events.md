# 事件与分发

## 注册与载荷

`ctx.On(name, listener, options)` 返回 Disposer，并随所属激活卸载而移除。Listener 接受 Event 和 Next：Event.Context 是发起分发的 Context，闭包中的 ctx 是登记监听器的 Context。Event.Args 是调用者提供的载荷，`Parallel` 期间必须作为只读值使用。

`Prepend` 把监听器置前；`Once` 在调用前原子认领并移除，递归/并发分发最多执行一次；`Global` 绕过分发器的 Filter。每次分发复制监听器列表；已被移除或失效的监听器跳过，新加监听器在后续分发中生效。

## 五种模式

| 方法 | 顺序 | 返回与错误 |
| --- | --- | --- |
| Emit | 顺序同步调用全部监听器 | 忽略值；遇到第一个错误停止 |
| Parallel | 每个监听器一个 goroutine | 等待全部结束，errors.Join 汇总全部错误 |
| Serial | 顺序调用 | 首个 bail 值或错误停止 |
| Bail | 与 Serial 共用同步 Go 实现 | 首个 bail 值或错误停止 |
| Waterfall | 监听器包裹剩余链与默认行为 | 调用 next 才继续；可包装、替换或否决结果 |

`nil` 和 `false` 表示继续，`0`、`""`、`true` 以及其他非 nil 值表示 bail。Go 不区分同步返回值和 Promise，因此 Serial/Bail 在这里共用实现。回调 panic 转换为错误，与相应模式的 error 规则一致。

## Waterfall

以下片段假设 ctx 为当前激活、root 为宿主根容器；result 和 request 由业务定义。完整事件宿主见[组合示例](../../examples/cordis/main.go)。

```go fragment
ctx.On("tools/pre-execute", func(e cordis.Event, next cordis.Next) (any, error) {
    // 此处可检查请求，或返回一个否决结果。
    value, err := next()
    // 此处可处理后续链的结果。
    return value, err
}, cordis.EventOptions{})

root.Events().Waterfall("tools/pre-execute", func() (any, error) {
    // 默认执行行为。
    return result, nil
}, request)
```

这是接口片段，`result`、`request` 由调用方定义。若监听器不调用 next，后续监听器和默认行为都不执行。每次调用 next 向链后方推进一次；业务监听器通常只委派一次。Next 只用于当前同步调用栈，不应保存并在回调返回后调用。

## 显式过滤

`ctx.Events()` 默认到达根容器中的所有监听器，服务隔离不会自动隔离事件。需要按服务 Scope 分发时使用：

以下片段假设 view 是已创建的 Context 服务视图；它与上面的默认广播使用同一个运行时。

```go fragment
view.Filter(func(listener *cordis.Context) bool {
    return view.SameScope(listener, "model")
}).Emit("model/ready")
```

过滤函数在锁外执行，应为只读谓词。Global 监听器无视该谓词。TypeScript 的 thisArg/filter 与 Go 之间的差异见 [范围对照](source-map.md)。

## 并发与验证

Parallel、并发 Emit 与事件发起方自行管理的 goroutine 都可能重叠；运行时只保护监听器登记、认领和移除。已开始的回调可能在 Fiber 卸载后结束，资源共享的安全条件见 [生命周期](lifecycle.md#并发与等待)。当前载荷没有事件 schema 或泛型 EventKey，业务模块需要在定义扩展点时声明载荷类型。

五种模式、过滤、监听器移除与 panic 的行为测试见 [events_test.go](../../cordis/events_test.go)，由 `make check` 执行。
