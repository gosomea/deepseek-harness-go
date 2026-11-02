# 从 Go 逐步理解 DSH

## 从哪里开始

你需要掌握 Go 的函数、接口、错误处理，随后在涉及并发的实验中学习 goroutine、通道和 `context.Context`。不要求先掌握 TypeScript、Cordis 或 Agent 框架。先运行一个小程序，解释它为什么这样工作，再沿着源码对应读两种实现。

当前能直接学习和运行的是 Cordis、M2 的装配层、M3 的数据契约、M4 的模型、工具与提示词层、M5 的最小 Harness、M6 的会话持久化与 M7 的策略与执行世界；其他主题属于[复刻路线](../10-plans/dsh-go-replication/plans.md)中的未来阶段。本页把学习路径与能力建设对应起来，未实现主题不会给出假装可运行的命令。

每个未来主题的实验和验收已在[阶段方案](../10-plans/dsh-go-replication/stages/index.md)细化。阶段页给出要做的实验，所属模块教程在实现后提供真正可运行的命令与输出。

## 当前可学的主题

| 主题 | 从 Go 的哪个问题进入 | DSH 概念 | 阅读与实验 | 学完能够解释 |
| --- | --- | --- | --- | --- |
| C01 插件与实例 | 工厂函数返回一个实例后，谁启动和关闭它？ | Plugin、Fiber、activation、Context | [概念入门](../cordis/go-primer.md)、[第一个插件](../cordis/tutorial.md) | Plugin 是定义，Fiber 是注册实例；重启产生新的激活，Fiber 并不是 goroutine |
| C02 运行时依赖 | 构造函数注入后，依赖消失了怎么办？ | Inject、Provider、Consumer、Pending | [服务参考](../cordis/services.md)、[教程的依赖与恢复](../cordis/tutorial.md) | Go interface 管调用契约，运行时依赖还要管理出现、失效和恢复 |
| C03 资源所有权 | 一个函数返回后，它创建的监听器和子插件由谁释放？ | Effect、disposer、父子所有权、回滚 | [生命周期](../cordis/lifecycle.md)、[组合示例](../../examples/cordis/README.md) | defer 处理当前调用栈，插件生命周期需要跨调用持有与释放资源 |
| C04 事件与隔离 | 一次函数调用、广播和中间件委派分别适合什么？ | Emit、Parallel、Serial、Bail、Waterfall、Scope | [事件参考](../cordis/events.md)、[源码对应](../cordis/source-map.md) | 五种分发的等待与短路；Go 显式事件过滤与服务 Scope 的边界 |
| C05 配置与装配 | 编译期写死的调用关系怎样变成部署时可配置的组合？ | Entry、Loader、profile、bundle | [概念对照](../loader/go-primer.md)、[装配树](../loader/entry-tree.md)、[装配教程](../loader/tutorial.md) | 配置条目与插件实例为什么必须分开；声明树与运行树的区别 |
| Agent 可见层 | 两个 Agent 同时运行，注册的工具为什么不串味？ | scope、ScopeKey、父链、贡献层、事件准入 | [Agent 可见层](../scope/go-primer.md) | 为什么作用域不是“给注册加一个 owner 字段”；它为什么不等于 Cordis 的服务隔离 |
| C06 对话与流 | 一次模型回答里到底有哪些值？ | Message、ContentBlock、source、工具调用参数分片 | [消息模型](../llm/go-primer.md)、[内容块参考](../llm/content-blocks.md) | 为什么一条 assistant message 不能等同于一次 HTTP chunk；未知内容怎样不被静默改写 |
| C07 事件溯源 | 为什么 Agent 历史不是一份 `messages` 数组？ | SessionEvent、连续序号、未知事件政策、surface、投影 | [事件日志](../session/go-primer.md)、[envelope 与 seq](../session/event-log.md)、[投影](../session/projection.md)、[Session 差分场景](../../testdata/parity/session/README.md) | 为什么日志是权威而消息数组是投影；审计事件怎样让日志变长而历史不变；同一日志为什么必须得到同一历史 |
| C06b 流与适配 | 一次模型回答里到底有哪些值？适配器丢掉了什么？ | StreamChunk、Assembler、Provider、能力声明 | [流与适配](../llm/streaming.md)、[tRPC 适配器](../llm/trpc-adapter.md)、[examples/model](../../examples/model/README.md) | 为什么分片不等于消息；未知 usage 与零 usage 的区别；框架语义差异记在哪 |
| C08 能力与工具 | 工具函数为什么需要定义、提供方和执行管线？ | Registry、InputSchema、GuardChain、Pipeline 六阶段 | [从 Go 理解工具](../tools/go-primer.md)、[执行管线](../tools/execution-pipeline.md)、[examples/tools](../../examples/tools/README.md) | 为什么“能调用”不等于“该调用”；拒绝与参数错误为什么都不进工具体；结果为什么必须唯一 |
| C08b 提示词装配 | 提示词片段的顺序由谁决定？ | Fragment、Order、scope 过滤、工具 schema 装配 | [提示词装配](../systemprompt/assembly.md) | 为什么顺序必须是显式字段；同一输入为什么必须渲染同一结果；schema 与校验为什么同源 |
| C09 Agent 轮次 | 一条输入怎样跨多次模型请求，取消后还欠哪些工作？ | Agent 句柄、turn/step/attempt、inbox、取消与所有权 | [从 Go 理解 Agent](../agent/go-primer.md)、[turn 与 step](../agentloop/turns.md)、[examples/harness](../../examples/harness/README.md) | 句柄、实例与驱动为什么分开；日志为什么能重建每次请求；取消为什么保留 inbox |
| C09b inbox 与取消 | 轮次进行中追加的输入在哪里生效？取消与销毁有什么不同？ | claim 原子性、steering、Canceler 与 Owner | [inbox 与追加输入](../agentloop/inbox.md)、[取消与关闭](../agentloop/cancellation.md) | 为什么「队列非空」不等于「还要继续」；取消为什么保留 inbox；销毁为什么必须等待释放 |
| C10 持久化与恢复 | whenIdle 返回后为什么还可能需要 flush？重开后哪些工作会继续？ | seam、单所有权句柄、单写者、append/flush/可见、checkpoint、恢复不重放 | [持久化入门](../persistence/go-primer.md)、[恢复与 checkpoint](../persistence/recovery.md)、[examples/session-replay](../../examples/session-replay/README.md) | append 与 flush 为什么是两个承诺；恢复为什么只描述不行动；checkpoint 为什么记进日志 |
| C10b 格式与损坏 | 末尾写坏了和中间写坏了有什么不同？ | 记录边界、校验和、可修复尾部、版本拒绝 | [格式与损坏](../persistence/format.md) | 为什么未来版本要拒绝而不是尽力解析；撕裂尾部为什么能截掉 |
| C10c 派生缓存 | 缓存和日志不一致时该信谁？ | 权威日志、派生读模型、投影、重建 | [派生缓存](../persistence/cache.md) | 为什么删除缓存后必须重建一致；缺投影为什么要报错 |
| C11 策略与执行世界 | 远程 fs 与本地 shell 混用为什么会出错？ | cwd、进程树、能力接口、单向升级 | [能力与执行世界](../execution/capabilities.md)、[审批与沙箱升级](../execution/approval.md)、[capability 包](../../capability/README.md) | 为什么逐元素判包含；为什么缺能力要拒绝启动；为什么同模式升级也被拒 |
| C11b 本机工具与 CLI | 一次 CLI 轮次怎样走完并恢复？ | 策略先于副作用、进程树取消、输出限额、退出释放 | [CLI 与会话](../cli/session-cli.md)、[examples/cli](../../examples/cli/README.md)、[plugins/local](../../plugins/local/README.md) | 为什么拒绝之后文件必须未变；为什么恢复不重放工具 |
| C11c 平台矩阵 | 哪些能力真的验证过？ | 实测／仅代码／未验证／未实现 | [平台矩阵](../execution/platform-matrix.md) | 为什么「有代码」不等于「验过」；为什么未验证不能算通过 |

建议先完整阅读教程，再根据问题查参考页。第一次运行后，先预测“把依赖名称改成不存在的名称会怎样”，再实际修改并比较状态；恢复名称后观察激活。源码入口由对应表维护，不要求读者先通读九个 TS 文件。

C06 与 C07 有可运行入口：`examples/session` 手工写出一份九条事件的日志，打印每条事件是否产生模型历史、派生出的历史，以及装上内容投影前后的差异；`go run ./examples/session` 的输出逐字保存为它的 `expected.txt`。C07 的等价性另有双实现证据：六个 Session 场景分别由 Go 与固定参考执行并逐行比较，当前没有已声明差异。

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
| C12 上下文与压缩 | C06–C11 | 模型看到的内容怎样从日志重建，超预算怎么办？ | [上下文贡献](../context/contributions.md)、[设置与三层覆盖](../context/settings-scope.md)、[技能与加载](../context/skills.md)、[附件诊断](../context/attachments.md)、[预算与 spill](../context/budgets.md)、[压缩与表面替换](../context/compaction.md) | M8 |
| C13 子代理与长任务 | C09–C12 | fork 复制了哪些事实，谁可以终止子 Agent？ | [子代理与生命周期树](../tasks/subagents.md)、[目标与续跑](../tasks/goals.md)、[持久任务与调度](../tasks/jobs.md)、[Workflow 与委派](../tasks/workflow.md) | M9 |
| C14 协议与远程能力 | C08–C11 | 为什么内部 Go interface 不能直接充当稳定 wire 协议？ | [协议与远程能力](../protocols/overview.md)、[兼容矩阵](../protocols/compat-matrix.md) | M10 |
| C15 客户端与载体 | C07、C10、C14 | 断线后的 UI 从哪里恢复，实时 delta 是真源吗？ | [客户端与投影](../client/overview.md)、[前端缺口](../client/frontend-gap.md) | M11 |
| C16 全域覆盖与发行 | — | 每个包组的处置是什么，哪些是缺口？ | [全域行为账本](../conformance/ledger.md)、[范围说明](../conformance/scope.md)、[兼容缺口](../compatibility/gaps.md)、[发行缺口](../release/gaps.md) | M12 |

## 三条阅读路线

C06 的模型接入采用 [tRPC-Agent-Go 模型层](../10-plans/dsh-go-replication/plans.md#模型接入选型trpc-agent-go)。学习实验先运行 fake，再运行无密钥的 tRPC 回环 Provider，观察同一场景的请求、分片与错误转换；真实端点在 M7 加入。重点解释底层通信库怎样接入 DSH 的自有契约，相关实现与笔记仍属规划。

**从零理解运行时：** C01 → C02 → C03 → C04 → C05。完成后能够独立写出一个有依赖、会释放资源的插件，并解释配置如何变成运行中的树。

**理解 Agent 主链：** 在前一条基础上读 C06 → C07 → C08 → C09 → C10 → C11 → C12（C09 至 C12 已可学）。C06、C06b、C07、C08、C08b、C09、C09b、C10、C10b、C10c、C11、C11b、C11c、C12 与 C13 已可学；C14 起属后续阶段。完成后能够画出“输入→请求→流→工具→下一请求→结算→恢复”，并指出哪些是持久事实。

**理解完整产品：** 继续 C11 → C12，再按问题选择 C13、C14、C15，最终进入 C16。某个协议或平台 Provider 可以单独查阅，但应先了解其能力接口和所有者。

## 每篇笔记的阅读方式

第一层回答一个具体问题，并把新概念连接到已有 Go 知识。第二层运行完整实验、预测输出、制造一次可控失败。第三层沿着两侧源码解释时序、所有权和差异。TS 语法只解释读当前片段所需的部分。

概念对照必须说明边界。例如 Fiber 不是 goroutine，`cordis.Context` 不是标准库 `context.Context`，服务 Scope 也不自动等同于 Agent 作用域，而 Session 的 surface 也不等同于 `session-projection` 的客户端读模型。文件名相似只提供导航，不能证明行为等价。

新增笔记按[模板](note-template.md)组织。实验所在模块负责行为事实；学习页组织认知顺序，引用对应源码和参考，不把所有规则复制到第二处。未来每个主题的实现、实验和笔记共同通过[验收](../10-plans/dsh-go-replication/acceptance.md#学习验收怎样做)后才标记可学。
