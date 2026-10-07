# Cordis 行为差距审计（M0.2–M0.3）

本页是 cordis-audit 节点的交付物。它把每个 Cordis 行为同时定位到固定 TypeScript 源码、当前 Go 实现、对应测试和读者实验，并给出缺口分类与 M1 的有限范围。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`vendor/cordis` 版本 `@deepseek-ai/cordis` 4.0.4，九个源文件共 2,696 行。所有源码引用都从该 Git 对象读取，与本地工作树的未提交改动无关。Go 侧入口是 [cordis 包](../../../cordis/) 与 [Cordis 源码对应表](../../cordis/source-map.md)。

## 怎样读这张表

八类行为各自成节。每节列五列：Go 已有行为、TS 依据、对应测试、读者实验、差异或缺口。状态取值与[共同验收标准](acceptance.md)一致：

| 状态 | 含义 |
| --- | --- |
| 已验证 | Go 行为有测试证据，且差异已说明 |
| 已定义 | 契约明确，但缺少一条边界测试或读者实验 |
| 显式适配 | 目标相同、Go 机制不同，有测试与差异记录 |
| 未实现 | 尚无 Go 实现，已有归属阶段 |

“已验证”只针对该行写明的场景。同一类中未列出的边界仍是未审计项，不能从行状态外推。

## 1. 插件与实例（Plugin / Fiber / 激活）

| 项 | 内容 |
| --- | --- |
| Go 已有行为 | `Context.Plugin` 每次注册创建一个 Fiber；`Fiber.ID` 稳定且唯一（root 为 0）；`Fiber.State` 暴露同步状态快照；`Dispose` 级联释放后代 |
| TS 依据 | `registry.ts:316` `RegistryService.plugin()`；`registry.ts:207` `get counter()`；`fiber.ts:184` `class Fiber`，`fiber.ts:336` `get name()`，`fiber.ts:574` `_getState()` |
| 对应测试 | [`TestInvalidPluginAndCanceledContextRejectWork`](../../../cordis/runtime_test.go)、[`TestChildrenDisposedAndCleanupContinuesAfterPanic`](../../../cordis/lifecycle_test.go) |
| 读者实验 | [第一个插件](../../cordis/tutorial.md)：注册后预测状态，再 `Dispose` 观察后代一并释放 |
| 差异或缺口 | **已验证**。TS 按 plugin 函数身份复用 `Plugin.Runtime`（同一函数多次注册共享一次 `internal/plugin` 记录）；Go 没有函数身份，改为每次 `Plugin()` 调用独立登记、以名称作诊断标签。`Fiber` 不是 goroutine，激活不绑定 OS 线程 |

## 2. 运行时依赖与失效传播

| 项 | 内容 |
| --- | --- |
| Go 已有行为 | 缺少 `Inject` 服务时保持 `Pending`；服务失效时先失效 Active 消费者，再按倒序释放；恢复后重新激活；注入绑定快照供清理读取 |
| TS 依据 | `fiber.ts:611` `_refresh()`（epoch 由依赖 impl 的 `fiber.uid` 串成）；`fiber.ts:625` `_setEpoch()`；`fiber.ts:675` `_unload()`；`reflect.ts:314` `notify()`；`reflect.ts:237` `_getImpl()` |
| 对应测试 | [`TestDependencyLossRestartsAndKeepsCleanupAccess`](../../../cordis/lifecycle_test.go)、[`TestTransitiveConsumersUnloadBeforeProvider`](../../../cordis/lifecycle_test.go)、[`TestProviderRestartReactivatesConsumers`](../../../cordis/runtime_test.go) |
| 读者实验 | [教程的依赖与恢复](../../cordis/tutorial.md)：把依赖名改成不存在的名称，观察 Pending；恢复后观察激活 |
| 差异或缺口 | **已验证**。TS 的激活版本是字符串（`_refresh` 里逐依赖拼接 `:uid`），Go 用 `revision`/`loadedRevision` 计数加绑定切片比较，语义相同、表达不同。TS 支持 `internal/service` 通知，见第 6 类 |

## 3. 资源所有权与 effect 回滚

| 项 | 内容 |
| --- | --- |
| Go 已有行为 | 服务、监听器、子插件与 `OnDispose` 都挂在所属激活上；独立清理倒序执行；Cleanup panic 不阻止其余资源释放；启动失败仍持有 Apply 返回的清理；手动 disposer 被所有者等待 |
| TS 依据 | `fiber.ts:418` `effect()`（`_disposables` 逆序、`disposeAfter`、`setupBarrier`）；`fiber.ts:675` `_unload()` 用 `Promise.all` 并逐个 `composeError`；`utils.ts:5` `DisposableList`；`reflect.ts:297` provide 的清理先 `notify` 再 `await` 消费者 |
| 对应测试 | [`TestManualDisposerJoinedAndReentrantMutation`](../../../cordis/lifecycle_test.go)、[`TestSelfDisposalDuringStartupOwnsReturnedCleanup`](../../../cordis/lifecycle_test.go)、[`TestFailureRollsBackAndUpdateValidatesBeforeRestart`](../../../cordis/lifecycle_test.go) |
| 读者实验 | [生命周期](../../cordis/lifecycle.md) 与 [组合示例](../../../examples/cordis/README.md)：在 Apply 中注册监听器，卸载后确认监听器已移除 |
| 差异或缺口 | **显式适配**。TS 的一次性清理是并发启动的（`Promise.all` + 逐个错误包装），失败仍继续；Go 由单个 reconcile 驱动串行执行独立清理，顺序确定、便于观察。TS 支持生成器与异步生成器 Effect（`fiber.ts:377`、`fiber.ts:384`），Go 用函数返回值表达，见第 8 类 |

## 4. 服务查找与 Scope 隔离

| 项 | 内容 |
| --- | --- |
| Go 已有行为 | `Provide`/`Get` 与 `Key[T]`/`Resolve[T]`；重复提供、类型不符、缺名与 nil 值都有可观察错误；`Isolate(name, scope)` 只替换该服务的命名空间，其余服务仍共享 |
| TS 依据 | `reflect.ts:277` `provide()`；`reflect.ts:233` `get()`；`reflect.ts:254` `set()`；`context.ts:121` `isolate()`；`reflect.ts:133` `ReflectService.handler`（Proxy 读取注入属性） |
| 对应测试 | [`TestScopesJoinWithoutLeakingOtherServices`](../../../cordis/lifecycle_test.go) |
| 读者实验 | [服务与作用域](../../cordis/services.md)：用同一 Scope 让两个消费者共享服务，再换新 Scope 观察隔离 |
| 差异或缺口 | **已验证**。TS 通过 Proxy 让 `ctx.foo` 直接读注入服务，Go 用显式 `Get`/`Resolve`，不复制动态属性。TS 的 `set()` 允许属主覆盖已提供服务值，Go 无对应方法 |

## 5. 五种事件分发与显式过滤

| 项 | 内容 |
| --- | --- |
| Go 已有行为 | `Emit`、`Parallel`、`Serial`、`Bail`、`Waterfall`；`Once` 在首次调用前认领；`Prepend` 插到最前；`Global` 绕过 `Filter` 谓词；监听器随所属激活移除；Waterfall 可多次 `Next` 并跳过已移除监听器 |
| TS 依据 | `events.ts:165` `dispatch()`；`events.ts:183` `parallel`（`allSettled` + `AggregateError`）；`events.ts:194` `emit`；`events.ts:204` `serial`；`events.ts:217` `bail`；`events.ts:234` `waterfall`；`events.ts:13` `isBailed` |
| 对应测试 | [`TestEventOrderBailValuesAndOwnedRemoval`](../../../cordis/events_test.go)、[`TestParallelJoinsAllErrorsAndOnceIsAtomic`](../../../cordis/events_test.go)、[`TestWaterfallDelegationAndVeto`](../../../cordis/events_test.go)、[`TestWaterfallRepeatedNextAdvancesAndSkipsRemovedListener`](../../../cordis/events_test.go)、[`TestFilterIsExplicitAndGlobalBypassesIt`](../../../cordis/events_test.go)、[`TestDispatchSkipsRemovedListenersAndRecoversPanics`](../../../cordis/events_test.go) |
| 读者实验 | [事件参考](../../cordis/events.md)：同一个事件分别用 Emit 与 Waterfall 分发，观察短路与否决 |
| 差异或缺口 | **已验证**。TS 的 Emit 同步不等待返回的 Promise，Serial/Bail 是异步的；Go 没有 Promise，`Serial` 与 `Bail` 共用同一个同步实现，返回值语义保持一致。TS 的 `dispatch` 从参数里剥离 `this` 实参，Go 用 `Event.Context` 显式表达调度作用域 |

## 6. 配置校验与更新

| 项 | 内容 |
| --- | --- |
| Go 已有行为 | `Validate` 在注册与更新时都先校验；无效配置保留当前激活不变；加载中的更新使用最新配置并忽略过时启动结果；`Restart` 用当前配置重新激活；root 不能重启 |
| TS 依据 | `fiber.ts:50` `resolveConfig()`（走 Standard Schema）；`fiber.ts:736` `update()`；`fiber.ts:718` `restart()`；`fiber.ts:641` `_resolveConfig()`（`internal/config` waterfall） |
| 对应测试 | [`TestFailureRollsBackAndUpdateValidatesBeforeRestart`](../../../cordis/lifecycle_test.go)、[`TestUpdateDuringLoadingUsesLatestConfig`](../../../cordis/runtime_test.go) |
| 读者实验 | [教程](../../cordis/tutorial.md)：先传非法配置观察拒绝，再传合法配置观察重启前后的监听器数量 |
| 差异或缺口 | **显式适配**。TS 的 `update()` 先跑 `internal/update` waterfall，HMR 可以否决或替换重启；Go 的 `Update` 直接比较 revision。TS 的配置可经 `intercept` 分层合并（`context.ts:140`、`service.ts` 的 resolveConfig），Go 只保存一份已校验值，分层 patch 属 M2。TS 的配置校验用 Standard Schema，Go 用显式 `Validate` 函数 |

## 7. 取消、等待与关闭

| 项 | 内容 |
| --- | --- |
| Go 已有行为 | 每个激活持有 `GoContext()`，卸载前先取消；`Wait` 等待 reconcile 结算并汇总启动失败与清理错误；`Close` 永久结束容器并等待清理，可被 `context` 截止时间限界；关闭中拒绝新注册 |
| TS 依据 | `fiber.ts:704` `await()`（等待 `inertia` 并重抛启动错误）；`fiber.ts:718` `restart()`；`fiber.ts:293` 的 `while (this.inertia)` 等待 |
| 对应测试 | [`TestConcurrentPluginRegistrationAndCloseCancelsStartup`](../../../cordis/runtime_test.go)、[`TestCloseDeadlineBoundsWaitForUncooperativeCleanup`](../../../cordis/runtime_test.go) |
| 读者实验 | [生命周期](../../cordis/lifecycle.md#并发与等待)：在 Apply 中触发取消，观察过时激活不发布服务 |
| 差异或缺口 | **显式适配**。TS 用 Promise 链（`inertia`）表达“进行中的装载/卸载”，Go 用单驱动 `reconcile` 加 `idle` 通道。TS 的 `await()` 会重抛启动错误，Go 的 `Wait` 汇总错误而不是抛出。TS root 的 disposer 可重启，Go 的 `Root.Close` 永久结束 |

## 8. 诊断、日志与错误分类

| 项 | 内容 |
| --- | --- |
| Go 已有行为 | `Fibers()` 返回注册顺序的稳定快照（ID、父 ID、名称、状态、错误）；`WithLogger` 与 `Context.Logger()` 附插件名；哨兵错误 `ErrInactive`、`ErrDuplicateService`、`ErrServiceNotFound`、`ErrServiceType` 可用 `errors.Is` 判断 |
| TS 依据 | `fiber.ts:157` `CordisError` 与 `CordisError.Code`；`fiber.ts:568` `getEffects()`；`logger.ts:83` `Logger` 与 `LoggerService`；`events.ts:288` `on()` 生成的监听标签 |
| 对应测试 | [`TestInvalidPluginAndCanceledContextRejectWork`](../../../cordis/runtime_test.go)、[`TestProviderRestartReactivatesConsumers`](../../../cordis/runtime_test.go)（含日志配置） |
| 读者实验 | [源码对应表](../../cordis/source-map.md)：按问题在 TS 与 Go 之间来回定位，再用 `root.Fibers()` 检查状态 |
| 差异或缺口 | **显式适配**。TS 有 `internal/status`、`internal/plugin`、`internal/dispatch`、`internal/service`、`internal/update`、`internal/config`、`internal/listener` 七个内部事件与 `getEffects()` 的 effect 树，Go 均未实现，见下节分类。TS 的 `CordisError` 只有 `INACTIVE_EFFECT` 一个码，Go 用四种哨兵错误覆盖更多注册期条件 |

## 缺口分类

下表把[源码对应表](../../cordis/source-map.md)列出的未实现项逐条归类。分类依据是固定提交里查到的真实消费者，而不是 API 名称是否出现在导出清单中。

| 缺口 | 分类 | 消费者证据（固定提交） | 归属 |
| --- | --- | --- | --- |
| 生成器／异步生成器 Effect | **下游阻塞（M1 未关闭）** | 生产代码有 205 个文件使用 `ctx.effect(`，其中 38 处以 `function*`／`async function*` 注册，例如 `packages/core/agent/src/index.ts:276`、`packages/core/session/src/index.ts:974`、`packages/llm/llm/src/index.ts:398` | M1 已判定为**表达方式差异**而非功能缺口：`OnDispose` 的倒序所有权已覆盖资源登记语义，生成器只是 TS 的书写形式。M2 展开 Loader 时再确认是否需要更贴近的 API |
| `Service` 基类与 `[Service.check]` 可用性谓词 | **下游阻塞（M1 已关闭）** | `vendor/loader/src/index.ts:102` 用 `ctx.reflect.provide('loader', this, this[Service.check])`，`index.ts:175` 实现该谓词；全仓 80 个包 `extends Service` | M1 已用 `ProvideWhen`/`Refresh` 关闭，见下节“M1 执行结果” |
| `Context.intercept` 与分层配置合并 | **下游阻塞** | `vendor/loader/src/config/isolate.ts:126` 按 `EntryOptions.intercept` 写 `entry.ctx[Context.intercept]`，该选项由 `packages/boot/app-boot/src/config-schema/document.ts:19` 声明；`vendor/cordis/src/service.ts:87` 的 `resolveConfig` 读取它，而 `packages/settings/settings/src/index.ts:215`、`packages/boot/config-editor/src/index.ts:92` 调用 `resolveConfig` | M1 最小子集，完整合并进 M2 |
| 服务隔离符号表（per-entry isolate realm） | **下游阻塞** | `vendor/loader/src/config/isolate.ts:92–152` 直接读写 `ctx[Context.isolate]` 与 `reflect.store` | M1 决定边界，M2 实现 |
| `internal/plugin` 事件 | **下游阻塞** | `vendor/loader/src/index.ts:129` 是 loader 的核心挂接点（设置 `fiber.entry`、处理自卸载）；`packages/client/modules/src/index.ts:624` 等 8 个生产文件 | M1 |
| `internal/config` waterfall | **下游阻塞** | `vendor/loader/src/index.ts:104` 对每份配置插值 `!!js`；`packages/boot/config-editor/src/index.ts:91`、`packages/llm/llm-pi-ai/src/index.ts:172` | M1 |
| `internal/update` waterfall | **下游阻塞** | `vendor/loader/src/index.ts:115`、`index.ts:123` 用 `global`/`prepend` 选项挂全局更新钩子 | M1 |
| `Context.extend` 属性合并 | **下游阻塞（需 Go 设计）** | `vendor/loader/src/config/entry.ts:57` 与 `config/tree.ts:16` 用它携带 `Entry.key`/`baseUrl`；`packages/core/scope/src/index.ts:140` 等生产使用 | M1 决策，M2 展开 |
| `Service.invoke` 可调用服务 | 后期功能 | 生产使用者集中在客户端与服务注册层，不阻塞 Loader | M12 收口 |
| `reflect.set()` 覆盖服务值 | 后期功能 | 无生产调用点（仅 `reflect.ts:254` 定义） | 账本保留 |
| `accessor` / `mixin` | 后期功能 | 生产仅 `packages/extensions/cordis-client-runner/src/client/timer.ts:34` 一处；`accessor` 无生产调用点 | M12 |
| traceable Proxy 与 `composeError` 调用栈美容 | 后期功能 | 只影响诊断文本，不影响行为 | M12 诊断收口 |
| `internal/service` 通知事件 | **下游阻塞（弱）** | `packages/api/gateway/src/index.ts:229`、`packages/preset/agent-preset-registry/src/invariant.ts:34` 两个生产消费者 | M1 复核后决定，否则 M2 |
| `internal/status` 状态变更事件 | 后期功能 | 生产消费者为 `packages/core/agent/src/index.ts:271` 与 `packages/client/web/src/boot-client.ts:42` 等 | M5 起按需，M12 收口 |
| `internal/dispatch` 分发事件 | 后期功能 | 26 个生产文件，但几乎全部是 invariant 诊断插件 | 诊断阶段按需 |
| `internal/listener` | 后期功能 | 无任何生产消费者，仅出现在目录生成脚本 | 不排期 |
| `getEffects()` effect 诊断树 | 后期功能 | 无生产调用点 | 不排期 |
| Standard Schema 适配 | 后期功能 | 配置校验当前由各包自己的 `Config` schema 承担 | M2 |
| `Volatile` 配置 | 后期功能 | 仅 `vendor/loader/src/config/entry.ts` 的 volatile-only 更新路径使用 | M2 |
| Loader / YAML include / Timer / 模块 HMR | 后期功能 | 属于 M2、M9 与 M12 的独立范围 | M2 / M9 / M12 |

### 分类口径

- **下游阻塞**：固定提交里的生产代码在 M1 之后的阶段会直接调用；不先补就写不出 Loader 或核心插件。
- **后期功能**：有定义、无阻塞性消费者，或有消费者但不影响 M1–M2 的主链；进入账本但不在 M1 排期。
- **显式 Go 适配**：目标一致，Go 用不同机制表达，已在差异列记录并有测试。

## M0.3：M1 的有限范围

依据上表，M1 只做四件让 Loader 与核心插件可写的事，不做 Cordis 4.0.4 的完整 API 兼容。

| 切片 | 范围 | 验收 |
| --- | --- | --- |
| M1.1 | 建立 `testdata/parity/cordis` 的共享场景与语义 trace，记录 TS 依据、ID 与时钟归一化规则 | 期望值来自固定源码；显式状态与 happens-before 不被归一化抹掉 |
| M1.2 | 补生成器／异步生成器风格的资源登记（让 `function*` 形态的资源可在 Go 中表达为等价顺序），核对依赖失效、激活版本、配置更新与失败恢复 | 加载中更新、Provider 多级失效、无效配置拒绝都有可控测试 |
| M1.3 | 引入最小 `Service` 契约：可用性谓词 `check` 与统一的服务注册入口，使 Loader 的 `[Service.check]` 有对应物 | Provider 不可用时消费者保持 Pending；谓词抛错不改写已发布服务 |
| M1.4 | 补 `internal/plugin`、`internal/config`、`internal/update` 三个 Loader 直接依赖的事件钩子，并复核 `internal/service` 是否有 M1 消费者；核对 `Context.extend` 在 Go 中的等价物 | Loader 的挂接点能在 Go 中表达为显式事件；`Global` 与 `Prepend` 顺序有测试 |
| M1.5 | 收敛最小 Loader 运行时契约，更新公开注释、API 与对应表，补齐读者实验 | 阻塞项全部关闭或有已验证替代；明确哪些行为仍延后 |

M1 的验收不变：G0 通过后按 [M1 细化方案](stages/m01-cordis.md) 展开为执行契约。本页给出的范围是 M1 展开时的输入。

## M1 执行结果

M1 已展开为独立执行契约 [dsh-go-replication-m01](../dsh-go-replication-m01/plan.json)（三个节点：`availability-predicate`、`parity-harness`、`m01-handover`），全部通过并有导出证据。本节记录**实际关闭**与**仍然开放**的缺口，避免把范围写成完成。

### 已关闭

| 缺口 | 实现 | 证据 |
| --- | --- | --- |
| `Service` 可用性谓词（Loader 的 `[Service.check]`） | `Context.ProvideWhen(name, value, check)` 与 `Context.Refresh()`；谓词 panic 记为不可用并记录日志 | [`TestConditionalAvailabilityGatesConsumers`](../../../cordis/lifecycle_test.go)、[`TestUnconditionalProvideStaysAvailable`](../../../cordis/lifecycle_test.go) |
| 行为对照缺少可复现证据 | 六个共享场景 + 两侧 trace + 显式差异声明；Go 侧 [`internal/testkit`](../../../internal/testkit/README.md) | [差分场景说明](../../../testdata/parity/cordis/README.md)、[`Expected trace` 录制来源](../../../testdata/parity/cordis/provenance.json) |
| 覆盖率门禁只覆盖 Cordis | `coverageRequirement` 逐包列出；遗漏包因“没有覆盖数据”失败 | [`TestCoverageGateChecksEveryGatedPackage`](../../../scripts/doccheck/main_test.go) |

### 仍然开放

| 缺口 | 现状 | 归属 |
| --- | --- | --- |
| `internal/plugin`、`internal/config`、`internal/update` 事件 | 未实现；M1 用显式 API 代替了一部分，但 Loader 的挂接点仍需这些事件 | M2 |
| `Context.intercept` 与分层配置合并 | 未实现；M1 只确认了它不阻塞“条件可用性”这一条链 | M2 |
| per-entry isolate realm | 未实现；语义依赖 Loader 的 Entry 模型 | M2 |
| `Context.extend` 的 Go 等价物 | 未决；M1 未新增 API，因为尚无 Go 消费者 | M2 展开 Loader 时决定 |
| 生成器／异步生成器 Effect | **重新判定为表达差异**：`OnDispose` 已提供等价的所有权语义 | M2 复核 |
| `internal/service` 通知事件 | 未实现；两个生产消费者不足以在 M1 提前补 | M2 复核 |

### 过程记录

M1 执行中三次触发计划门禁，均为真实缺陷而非工具误报：

1. `docs/cordis/source-map.md` 在未声明的情况下被修改 —— 契约缺少该文件，改为把文档更新纳入节点范围后重跑。
2. `docs/README.md` 同类遗漏 —— 同样修订契约，而不是回退文档改动。
3. `parity-harness` 的输出清单只列了部分实现文件 —— 补齐后才完成评审。

三次修订都登记了新 revision 并重跑前置节点，没有用降低门禁的方式绕过。

## 固定参考入口

- [vendor/cordis/src](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/cordis/src)
- [vendor/loader/src](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src)
- [Cordis 源码对应与实现范围](../../cordis/source-map.md)
- [当前 Cordis 行为测试](../../../docs/testing.md)
