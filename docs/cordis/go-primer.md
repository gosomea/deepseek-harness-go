# 从 Go 理解 Cordis 与 DSH

## 概述

本页帮助熟悉 Go 的读者理解 DSH 如何通过插件组合能力。你会把接口、注入、取消和清理与 Cordis 对应起来，再进入[可运行教程](tutorial.md)。本页解释当前 Go 实现；TypeScript 的具体差异由[源码对应表](source-map.md)维护。

## 目录

- [为什么从 Cordis 开始](#为什么从-cordis-开始)
- [五个概念](#五个概念)
- [从固定注入到依赖变化](#从固定注入到依赖变化)
- [服务调用与事件](#服务调用与事件)
- [回到 DSH](#回到-dsh)

## 为什么从 Cordis 开始

DSH 用 Cordis 管理插件与服务，再组合会话、工具、模型调用和 Agent Loop。先实现 Cordis，可以观察一个能力如何出现、被消费、消失和恢复，而无需真实模型或文件持久化。[当前架构](../architecture.md)描述 Go 运行时，[路线图](../roadmap.md)记录后续模块。

## 五个概念

概念按启动一个程序时的出现顺序排列。服务能力仍用普通 Go 接口表达，Cordis 管理它的注册与有效时间。

| 概念 | 当前 Go 表达 | 读者需要区分的事实 |
| --- | --- | --- |
| 插件定义 | `cordis.Plugin` 的 Name、Inject、Apply、Validate | 定义启动方式；同一定义可以多次注册 |
| 插件实例 | `root.Plugin` 返回的 `*cordis.Fiber` | 实例有状态，可更新、重启或永久卸载；Fiber 是实例句柄，不是 goroutine |
| 激活 | 一次 Apply 与其资源列表 | 同一 Fiber 可以多次激活；每次获得新的 Context 与取消信号 |
| 上下文 | `*cordis.Context` | 提供本次激活的服务访问与资源注册；标准库 `context.Context` 通过 GoContext 管理取消 |
| 服务能力 | 消费侧接口与 `Key[T]` | Provider 提供实现，Consumer 用 Inject 声明依赖并 Resolve；名称和 Scope 决定查找位置 |

例如，教程的 counter 接口是能力定义，提供 `memoryCounter` 的插件是 Provider，调用 Next 的 greeter 是 Consumer。它们使用服务名关联，不要求消费者导入提供者实现。DSH 将这三种角色称为 Service Definition、Service Provider 和 Consumer。

## 从固定注入到依赖变化

常见 Go 构造函数把接口实现交给一个对象，调用方负责启动和关闭它。这里消费者先声明必需服务，运行时在依赖可用时调用 Apply；服务不可用时消费者保持 Pending。提供者被移除或重新启动，会使消费者清理旧激活；依赖重新可用后，它再运行 Apply。

一次激活拥有自己提供的服务、监听器、子插件与清理函数。On 和 Provide 自动登记释放动作；自建资源用 OnDispose 或 Apply 返回的 Cleanup 交给运行时。你仍需让自己启动的 goroutine 响应取消并在 Cleanup 中等待退出。精确时序与失败结果见[生命周期](lifecycle.md)，类型和服务可见性见[服务](services.md)。

## 服务调用与事件

需要直接执行一个能力时，通过接口调用服务方法。需要观察、协作或拦截一个流程时，通过事件扩展。事件的不同分发方法具有不同顺序、返回和错误规则；教程先使用串行操作，组合示例再引入 Emit，其他模式从[事件参考](events.md)查阅。

服务 Scope 只隔离服务名称的查找位置，不自动隔离事件。事件需要显式 Filter。运行时保护注册表，但不会给业务接口或事件载荷自动加锁，这些是改写为 Go 时必须关注的条件。

## 回到 DSH

读完[第一个插件](tutorial.md)后，按[源码对应表](source-map.md)的问题顺序回到 TypeScript。固定参考版本的 [DSH Cordis 入门](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/cordis-primer.zh.md)介绍 ctx 服务属性和注入；[DSH 架构](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/architecture.zh.md)连接这些机制与能力组合。

Go 的 `Resolve(ctx, key)` 对应从容器读取服务的目的，Go 接口对应消费侧能力定义；它们没有复现 TypeScript 的动态属性代理和声明合并。学习时先比较行为和所有权，再比较语法，完整兼容范围仍以源码对应表为准。
