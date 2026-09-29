# Cordis Go 核心

## 职责

提供以插件为所有者的服务注入、作用域、生命周期、资源清理和事件分发。Context 是容器视图，Fiber 是插件实例，一次激活拥有一组资源。核心不含模型、工具或 Session 逻辑。

## 使用

```go
root := cordis.New()
key := cordis.NewKey[Model]("model") // Model 是消费者的小接口
// Provider 的 Apply 中调用 cordis.Provide[Model](ctx, key, implementation)。
// Consumer 在 Plugin.Inject 中填写 key.Name()，再用 cordis.Resolve(ctx, key)。
// 外部宿主退出时调用 root.Close(context.Background()) 并处理错误。
```

完整可运行程序见 [示例](../examples/cordis/main.go)。从 [教程](../docs/cordis/tutorial.md) 开始，再阅读 [生命周期](../docs/cordis/lifecycle.md)、[服务与作用域](../docs/cordis/services.md)、[事件](../docs/cordis/events.md) 与 [API](../docs/cordis/api.md)。

## 限制

当前实现范围和与 TypeScript 的区别见 [对应表](../docs/cordis/source-map.md)。生命周期回调串行执行；并发或回调内的变更需从外部等待。服务对象和监听器中的状态同步由应用负责。[并发与等待约定](../docs/cordis/lifecycle.md#并发与等待) 是安全使用条件。

## 验证

从项目根目录运行 `make check`。聚焦本包可运行 `go test -race -count=1 ./cordis`。各行为与测试的对应见 [门禁矩阵](../docs/testing.md#行为与证据)。公开 API 的注释变化通过 `make api` 更新，并由文档门禁检查新鲜度。
