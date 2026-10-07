# 从 Go 逐步理解 DSH

## 从哪里开始

你需要掌握 Go 的函数、接口、错误处理，随后在涉及并发的实验中学习 goroutine、通道和 `context.Context`。不要求先掌握 TypeScript、Cordis 或 Agent 框架。先运行一个小程序，解释它为什么这样工作，再沿着源码对应读两种实现。

当前能直接学习和运行的是 Cordis 与 M2 的装配层（C01–C05）；其他主题属于[复刻路线](../10-plans/dsh-go-replication/plans.md)中的未来阶段。本页把学习路径与能力建设对应起来，未实现主题不会给出假装可运行的命令。

每个未来主题的实验和验收已在[阶段方案](../10-plans/dsh-go-replication/stages/index.md)细化。阶段页给出要做的实验，所属模块教程在实现后提供真正可运行的命令与输出。

## 当前可学的四个主题

| 主题 | 从 Go 的哪个问题进入 | DSH 概念 | 阅读与实验 | 学完能够解释 |
| --- | --- | --- | --- | --- |
| C01 插件与实例 | 工厂函数返回一个实例后，谁启动和关闭它？ | Plugin、Fiber、activation、Context | [概念入门](../cordis/go-primer.md)、[第一个插件](../cordis/tutorial.md) | Plugin 是定义，Fiber 是注册实例；重启产生新的激活，Fiber 并不是 goroutine |
| C02 运行时依赖 | 构造函数注入后，依赖消失了怎么办？ | Inject、Provider、Consumer、Pending | [服务参考](../cordis/services.md)、[教程的依赖与恢复](../cordis/tutorial.md) | Go interface 管调用契约，运行时依赖还要管理出现、失效和恢复 |
| C03 资源所有权 | 一个函数返回后，它创建的监听器和子插件由谁释放？ | Effect、disposer、父子所有权、回滚 | [生命周期](../cordis/lifecycle.md)、[组合示例](../../examples/cordis/README.md) | defer 处理当前调用栈，插件生命周期需要跨调用持有与释放资源 |
| C04 事件与隔离 | 一次函数调用、广播和中间件委派分别适合什么？ | Emit、Parallel、Serial、Bail、Waterfall、Scope | [事件参考](../cordis/events.md)、[源码对应](../cordis/source-map.md) | 五种分发的等待与短路；Go 显式事件过滤与服务 Scope 的边界 |

建议先完整阅读教程，再根据问题查参考页。第一次运行后，先预测“把依赖名称改成不存在的名称会怎样”，再实际修改并比较状态；恢复名称后观察激活。源码入口由对应表维护，不要求读者先通读九个 TS 文件。

### 四个主题与审计行为的对应

[M0 的 Cordis 行为审计](../10-plans/dsh-go-replication/cordis-audit.md) 把每个已实现行为定位到固定 TS 源码、Go 入口、测试名和本页的读者实验。按主题查时使用下面的对应：

| 主题 | 审计中的行为类 | 想验证时先看的测试 |
| --- | --- | --- |
| C01 插件与实例 | 第 1 类 | [`TestInvalidPluginAndCanceledContextRejectWork`](../../cordis/runtime_test.go) |
| C02 运行时依赖 | 第 2 类 | [`TestDependencyLossRestartsAndKeepsCleanupAccess`](../../cordis/lifecycle_test.go) |
| C03 资源所有权 | 第 3 类 | [`TestManualDisposerJoinedAndReentrantMutation`](../../cordis/lifecycle_test.go) |
| C04 事件与隔离 | 第 4、5 类 | [`TestFilterIsExplicitAndGlobalBypassesIt`](../../cordis/events_test.go) |

### 用差分场景验证四个主题

C01–C04 的每个行为都有对应的共享场景：同一份 JSON 分别喂给 Go 运行时与固定的 TypeScript 参考，两侧 trace 逐行比较。运行方式：

```sh
go test -v ./internal/testkit
```

| 主题 | 场景 | 可以亲手验证的现象 |
| --- | --- | --- |
| C01 插件与实例 | [`self-dispose-during-apply`](../../testdata/parity/cordis/scenarios/self-dispose-during-apply.json) | Apply 中自卸载后，实例状态是 `disposed`，返回的清理仍被执行 |
| C02 运行时依赖 | [`dependency-pending-recovery`](../../testdata/parity/cordis/scenarios/dependency-pending-recovery.json) | 可用性由假转真时消费者激活；由真转假时消费者卸载而 Provider 保持 Active |
| C03 资源所有权 | [`cleanup-order-and-panic`](../../testdata/parity/cordis/scenarios/cleanup-order-and-panic.json)、[`transitive-consumers`](../../testdata/parity/cordis/scenarios/transitive-consumers.json) | 清理倒序执行、panic 后其余清理继续；消费者先于 Provider 释放 |
| C04 事件与隔离 | [`event-dispatch-modes`](../../testdata/parity/cordis/scenarios/event-dispatch-modes.json) | Emit/Serial/Bail/Waterfall 的调用顺序与 bail 短路 |

预测再运行：先写下你预期的 trace 行，再执行测试并对比。差异不等于错误——[`DIVERGENCES.md`](../../testdata/parity/cordis/DIVERGENCES.md) 记录了两条已声明的差异（清理错误语义与独立兄弟节点的清理顺序），每条都写明理由。

审计页列出 M1 仍未关闭的缺口（Loader 依赖的内部事件、`intercept` 分层配置、per-entry isolate）。这些能力没有 Go 实现，本页不会给出假装可运行的命令。

## 后续学习地图

下表均为规划。每个主题上线时，要先交付完整实验与失败说明，再把名称变成实际笔记链接。主题编号用于稳定导航，不要求一主题恰好一篇文章。

| 主题 | 前置主题 | 你要回答的问题 | Go 视角与 DSH 对应 | 阶段 |
| --- | --- | --- | --- | --- |
| C05 配置与装配 | C01–C04 | 为什么配置条目和插件实例需要不同身份？ | 显式注册表、工厂、结构体配置 ↔ Entry、Loader、profile、bundle | M2，已可学：[概念对照](../loader/go-primer.md)、[装配树](../loader/entry-tree.md)、[更新语义](../loader/update-semantics.md)、[Bundle 与就绪](../loader/profile.md)、[装配教程](../loader/tutorial.md) |
| C06 对话与流 | Go 数据类型与接口 | 一条 assistant message 为什么不能等同于一次 HTTP chunk？ | DSH 消息／流 ↔ 本项目 Go 类型 ↔ tRPC 模型请求／响应；assembler 与取消 | M3–M4，M7 真实接入 |
| C07 事件溯源 | C04、C06 | 为什么 Session 不直接保存一份 messages 数组？ | append-only log、纯函数 fold ↔ SessionEvent、deriveMessages、projection | M3 |
| C08 能力与工具 | C02、C06–C07 | 工具函数为什么需要定义、提供方和执行管线？ | 小接口、Provider、middleware ↔ seam、schema、guard、tool-call/result | M4 |
| C09 Agent 轮次 | C05–C08 | 一条输入怎样跨多次模型请求，取消后还欠哪些工作？ | 状态机、队列、context 取消 ↔ Agent、turn、step、attempt、inbox | M5 |
| C10 持久化与恢复 | C07、C09 | whenIdle 返回后为什么还可能需要 flush？ | 文件句柄、单写者、提交边界 ↔ SessionHandle、generation、checkpoint | M6 |
| C11 策略与执行世界 | C08–C10 | 远程 fs 与本地 shell 混用为什么会出错？ | cwd、进程树、能力接口 ↔ workspace、sandbox、approval、执行世界 | M7 |
| C12 上下文与压缩 | C06–C10 | 模型看到的内容怎样从日志重建，超预算怎么办？ | 可重放投影、引用与预算 ↔ prompt、skills、attachments、spill、compaction | M8 |
| C13 子代理与长任务 | C09–C12 | fork 复制了哪些事实，谁可以终止子 Agent？ | 所有权树、消息队列、幂等 ↔ subagent、goal、jobs、schedule、workflow | M9 |
| C14 协议与远程能力 | C08–C11 | 为什么内部 Go interface 不能直接充当稳定 wire 协议？ | DTO、codec、request ID、流取消 ↔ SDK、ACP、MCP、LSP、SSH、Host API | M10 |
| C15 客户端与载体 | C07、C10、C14 | 断线后的 UI 从哪里恢复，实时 delta 是真源吗？ | 服务端状态与快照 ↔ Host、client store、resources、Web、Desktop | M11 |
| C16 完整系统 | 已学相关领域 | 如何证明每个扩展都有归属，发布后仍可诊断和恢复？ | 平台接口、基准、分发与可观察性 ↔ native、telemetry、PTC、实验扩展与发行 | M12 |

## 三条阅读路线

C06 的模型接入采用 [tRPC-Agent-Go 模型层](../10-plans/dsh-go-replication/plans.md#模型接入选型trpc-agent-go)。学习实验先运行 fake，再运行无密钥的 tRPC 回环 Provider，观察同一场景的请求、分片与错误转换；真实端点在 M7 加入。重点解释底层通信库怎样接入 DSH 的自有契约，相关实现与笔记仍属规划。

**从零理解运行时：** C01 → C02 → C03 → C04 → C05。完成后能够独立写出一个有依赖、会释放资源的插件，并解释配置如何变成运行中的树。

**理解 Agent 主链：** 在前一条基础上读 C06 → C07 → C08 → C09 → C10。完成后能够画出“输入→请求→流→工具→下一请求→结算→恢复”，并指出哪些是持久事实。

**理解完整产品：** 继续 C11 → C12，再按问题选择 C13、C14、C15，最终进入 C16。某个协议或平台 Provider 可以单独查阅，但应先了解其能力接口和所有者。

## 每篇笔记的阅读方式

第一层回答一个具体问题，并把新概念连接到已有 Go 知识。第二层运行完整实验、预测输出、制造一次可控失败。第三层沿着两侧源码解释时序、所有权和差异。TS 语法只解释读当前片段所需的部分。

概念对照必须说明边界。例如 Fiber 不是 goroutine，`cordis.Context` 不是标准库 `context.Context`，服务 Scope 也不自动等同于 Agent 作用域。文件名相似只提供导航，不能证明行为等价。

新增笔记按[模板](note-template.md)组织。实验所在模块负责行为事实；学习页组织认知顺序，引用对应源码和参考，不把所有规则复制到第二处。未来每个主题的实现、实验和笔记共同通过[验收](../10-plans/dsh-go-replication/acceptance.md#学习验收怎样做)后才标记可学。
