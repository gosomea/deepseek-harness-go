# 服务与作用域

## 类型与名称

`Key[T]` 描述消费者期待的类型，通常是小接口。`NewKey[T]("model")` 的名称用于 Inject、Provide 和 Isolate；同名不同 T 不构成两个服务，解析时会返回 ErrServiceType。注册表内部用 `any` 保存开放的服务集合，只有 Resolve 在消费点进行类型断言。

Provider 显式注册，Consumer 显式声明必需服务。除 Inject 外，Get/Resolve 也可以查询当前可用服务；如果 Consumer 希望随着某个服务更换而重新运行，必须把它列入 Inject。未声明的可选读取不会触发依赖重启。

## 所有权与发布

Provide 的服务归当前激活所有，在其 Fiber 成功进入 Active 后提供给新的消费者。Provider 在 Loading 时可读取自己的服务。名字不能为空，值不能是 nil 或 typed nil，同一个服务命名空间不允许两个 Provider。

Provide 返回可手动取消注册的 Disposer，同时自动参与 Fiber 清理。依赖消费者先于 Provider 的资源销毁退出，在 Cleanup 中仍可读取被注入的旧绑定；停止完成后清空旧快照。[生命周期](lifecycle.md) 说明取消和释放顺序。

替换 Provider 的方式是卸载旧实例、注册新实例。Restart/Update Provider 会释放旧绑定并建立新绑定，因此会重新激活注入消费者。v0.1 没有原地 Set API，服务对象内部数据更新不会触发重新激活。

## 按服务名隔离

`root.Isolate("model", nil)` 为 model 创建新的 Scope，返回不修改父视图的新 Context。这个视图继承其他服务的名称空间；如果没有在新 Scope 提供 model，读取返回 ErrServiceNotFound，不会回退到全局 model。

```go
scope := cordis.NewScope()
a := root.Isolate("model", scope)
b := root.Isolate("model", scope)
// a 与 b 的 model 使用同一命名空间，root 的 model 独立。
// 在 a 注册的插件会继承这个作用域。
```

Scope 是对象身份，允许把同一个 Scope 复用到多个视图。各视图仍由原 Fiber 激活拥有；Isolate 只建立服务视图，不创建新的 Fiber 或独立的资源生命周期。需要独立释放时，应在视图下注册插件。

## 错误与限制

ErrDuplicateService 表示同名同 Scope 冲突；ErrServiceNotFound 表示尚不可用；ErrServiceType 表示消费类型不匹配；ErrInactive 表示 Context 激活已过期或不允许操作。用 `errors.Is` 判断，错误文本用于定位名称。

当前没有服务代理、访问器、别名、方法装饰器、availability check 或 intercept 配置合并。服务对象直接共享；锁和线程安全由实现方负责。范围对照见 [源码对应表](source-map.md)。
