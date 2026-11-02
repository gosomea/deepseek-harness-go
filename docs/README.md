---
description: "按 Go 学习路径或行为问题查找 Cordis 教程、参考与开发验收说明。"
kind: "documentation-index"
---

# 文档索引

## 概述

这里帮助你选择下一篇文档：第一次学习从 Go 概念对照和可运行教程开始，修改某个行为时直接进入参考页。教程负责操作与结果，参考负责时序、错误和所有权。项目路线与验收证据有单独入口。

## 目录

- [学习路径](#学习路径)
- [查阅参考](#查阅参考)
- [开发与验收](#开发与验收)

## 学习路径

读者需要掌握 Go 函数、接口与错误处理；goroutine 和取消的基础有助于后续理解并发约定。

1. [从 Go 理解 Cordis 与 DSH](cordis/go-primer.md)：认识 Plugin、Fiber、Context 与运行时注入。
2. [写第一个插件](cordis/tutorial.md)：从完整小程序进入服务等待和恢复。
3. [运行组合示例](../examples/cordis/README.md)：加入事件，观察监听器释放。
4. [配置与装配](loader/go-primer.md)：从条目身份走到一棵装配树。
5. [对话消息](llm/go-primer.md)：认识消息、内容块与工具调用。
6. [Agent 可见层](scope/go-primer.md)：让两个 Agent 各有一份注册世界。
7. [事件溯源](session/go-primer.md)：为什么日志是权威、`messages` 数组只是投影。
8. [模型 surface 与投影](session/projection.md)：日志怎样变成模型输入。
9. [流与适配](llm/streaming.md)：为什么流式分片不等于最终答案。
10. [tRPC 适配器](llm/trpc-adapter.md)：同一份契约落在具体框架上，框架语义怎样被隔离。
11. [从 Go 理解工具](tools/go-primer.md)：工具定义、准入与执行管线的三角色。
12. [执行管线参考](tools/execution-pipeline.md)：六阶段顺序、拒绝路径与结果唯一性。
13. [提示词装配](systemprompt/assembly.md)：为什么片段顺序必须是显式的。
14. [从 Go 理解 Agent](agent/go-primer.md)：句柄、实例与运行驱动的区别。
15. [turn 与 step](agentloop/turns.md)：一次轮次写了哪些事件。
16. [inbox 与追加输入](agentloop/inbox.md)：claim 为什么必须是原子的。
17. [取消与关闭](agentloop/cancellation.md)：取消、销毁与关闭是三件事。
18. [持久化入门](persistence/go-primer.md)：seam、句柄与单写者。
19. [格式与损坏](persistence/format.md)：撕裂尾部与中间损坏的区别。
20. [恢复与 checkpoint](persistence/recovery.md)：重开之后哪些工作会继续。
21. [派生缓存](persistence/cache.md)：为什么缓存不能是权威。
22. [能力与执行世界](execution/capabilities.md)：同一 cwd 怎样解释文件与进程。
23. [审批与沙箱升级](execution/approval.md)：三种不授权结果与只能更宽的阶梯。
24. [平台矩阵](execution/platform-matrix.md)：哪些能力实测过、哪些未验证。
25. [CLI 与会话](cli/session-cli.md)：读文件→受控修改→退出→恢复。
22. [源码对应表](cordis/source-map.md)：按问题阅读 Go 与固定版本的 DSH 源码。

[完整学习地图](learning/index.md)标明当前可学的主题和后续主题的前置关系。新增主题按[学习笔记模板](learning/note-template.md)同时交付概念对照、完整实验和失败定位；未来主题在实现前保留规划状态。

## 查阅参考

按当前的问题选择页面，参考页可以独立查阅：

| 问题 | 文档 |
| --- | --- |
| 插件何时启动、失败后如何恢复、谁释放资源？ | [生命周期](cordis/lifecycle.md) |
| 类型如何检查、如何隔离服务、依赖何时重启？ | [服务与作用域](cordis/services.md) |
| 事件如何继续或短路、哪些操作会并发？ | [事件](cordis/events.md) |
| 某个公开方法的参数与返回值是什么？ | [生成 API](cordis/api.md) |
| 当前运行时由哪些部分组成？ | [架构](architecture.md) |
| 后续 Harness 模块按什么顺序实现？ | [路线图](roadmap.md) |
| Go 实现与固定 TypeScript 参考怎样对照，差异记在哪？ | [差分场景说明](../testdata/parity/cordis/README.md)、[已声明差异](../testdata/parity/cordis/DIVERGENCES.md)、[对照工具](../internal/testkit/README.md) |
| DSH 的每个部分如何分配到 Go，怎样算复刻完成？ | [系统方案](10-plans/dsh-go-replication/plans.md)、[能力覆盖](10-plans/dsh-go-replication/capabilities.md)、[共同验收](10-plans/dsh-go-replication/acceptance.md) |
| 某个阶段具体先做什么、改哪些文件、怎样学习和验收？ | [阶段实施索引](10-plans/dsh-go-replication/stages/index.md) |
| 配置文档怎么写、命令行怎么检查、出错怎么看？ | [装配教程](loader/tutorial.md)、[dsh-go 命令](../cmd/dsh-go/README.md) |
| 配置条目和插件实例为什么分开、工厂目录怎么建？ | [loader 包](../loader/README.md)、[Go 概念对照](loader/go-primer.md) |
| 条目怎么组成父子树、关闭时谁释放？ | [装配树](loader/entry-tree.md) |
| 运行中改配置、禁用、跨组移动各自什么语义？ | [更新与启停](loader/update-semantics.md) |
| 多个配置来源谁覆盖谁、哪些条目必须成功？ | [app 包](../app/README.md)、[Bundle 与就绪](loader/profile.md) |
| 消息、内容块与工具调用的字段是什么？ | [llm 包](../llm/README.md)、[内容块参考](llm/content-blocks.md) |
| 多个 Agent 同跑时工具为什么不串味？ | [scope 包](../scope/README.md)、[Agent 可见层](scope/go-primer.md) |
| Agent 历史为什么不是一份 messages 数组？ | [session 包](../session/README.md)、[事件日志](session/go-primer.md)、[envelope 与 seq](session/event-log.md) |
| 日志怎样变成模型输入、审计事件怎样不影响历史？ | [投影](session/projection.md) |
| 同一份日志在两侧实现里是否得到同一份历史？ | [Session 差分场景](../testdata/parity/session/README.md)、[已声明差异](../testdata/parity/session/DIVERGENCES.md) |
| 模型流怎样变成消息、适配器丢掉了什么？ | [llm 包](../llm/README.md)、[流与适配](llm/streaming.md)、[tRPC 适配器](llm/trpc-adapter.md) |
| 工具调用怎样被拒绝而不产生副作用？ | [tools 包](../tools/README.md)、[执行管线](tools/execution-pipeline.md) |
| 提示词片段的顺序由谁决定？ | [systemprompt 包](../systemprompt/README.md)、[提示词装配](systemprompt/assembly.md) |
| 两个 Provider 是否得到同一份请求与结果？ | [plugins/llmtrpc 包](../plugins/llmtrpc/README.md)、[tRPC 回环 fixture](../testdata/parity/llmtrpc/README.md)、[已声明差异](../testdata/parity/llmtrpc/DIVERGENCES.md) |
| 一条输入怎样变成多次模型请求，取消后还欠哪些工作？ | [agent 包](../agent/README.md)、[agentloop 包](../agentloop/README.md)、[turn 与 step](agentloop/turns.md) |
| 轮次进行中追加的输入在哪里生效？ | [inbox 与追加输入](agentloop/inbox.md) |
| 取消一轮、销毁实例与关闭注册表有什么不同？ | [取消与关闭](agentloop/cancellation.md) |
| 完整轮次的确定性场景在哪里？ | [Agent 轮次场景](../testdata/parity/agentloop/README.md)、[已声明差异](../testdata/parity/agentloop/DIVERGENCES.md) |
| 会话怎么落盘、重开后又该继续什么？ | [persistence 包](../persistence/README.md)、[JSONL 存储](../plugins/sessionjsonl/README.md)、[恢复与 checkpoint](persistence/recovery.md) |
| 日志末尾被写坏了，还能救吗？ | [格式与损坏](persistence/format.md) |
| 完整持久化行为的确定性场景在哪里？ | [持久化场景](../testdata/parity/persistence/README.md)、[已声明差异](../testdata/parity/persistence/DIVERGENCES.md) |
| Agent 触达本机世界的接口在哪里？ | [capability 包](../capability/README.md)、[工作区](../capability/workspace/README.md)、[文件系统](../capability/fs/README.md) |
| 一条命令被取消时怎样连带终止子进程？ | [子进程 seam](../capability/subprocess/README.md)、[本机 Provider](../plugins/local/README.md) |
| 一次审批能授权几次？更宽的限制谁批准？ | [沙箱](../capability/sandbox/README.md)、[审批](../capability/approval/README.md)、[审批与沙箱升级](execution/approval.md) |
| 一次 CLI 轮次怎么走完并恢复？ | [CLI 与会话](cli/session-cli.md)、[CLI 端到端实验](../examples/cli/README.md) |
| 进入模型的内容怎样先记录再渲染？ | [上下文贡献](../plugins/context/README.md)、[可重建请求](context/contributions.md) |
| 两个 Agent 能用不同的工具和说明吗？ | [设置与作用域](../plugins/settings/README.md)、[preset](../plugins/preset/README.md)、[三层覆盖](context/settings-scope.md) |
| 技能清单和实际加载内容差多少？ | [skills](../plugins/skills/README.md)、[技能与加载](context/skills.md) |
| 附件的身份与请求可访问路径怎样对应？ | [附件能力](../capability/attachment/README.md)、[附件 Provider](../plugins/attachment/README.md)、[附件诊断](context/attachments.md) |
| 超长工具输出怎样缩短但仍能定位原文？ | [spill 能力](../capability/spill/README.md)、[spill Provider](../plugins/spill/README.md)、[预算与 spill](context/budgets.md) |
| 历史变长时怎样压缩且不拆坏工具对？ | [压缩能力](../capability/compaction/README.md)、[压缩 Provider](../plugins/compaction/README.md)、[压缩与表面替换](context/compaction.md) |
| 父 Agent 怎样创建并控制子 Agent？ | [subagent seam](../capability/subagent/README.md)、[进程内 Provider](../plugins/subagent/README.md)、[子代理与生命周期树](tasks/subagents.md) |
| 目标、计划与待办怎样跨轮次存活？ | [taskstate](../capability/taskstate/README.md)、[目标与续跑](tasks/goals.md) |
| 后台任务怎样在重启后可解释？ | [jobs](../capability/jobs/README.md)、[schedule](../plugins/schedule/README.md)、[持久任务与调度](tasks/jobs.md) |
| 多步编排怎样与执行引擎分离？ | [workflow seam](../capability/workflow/README.md)、[fake 引擎](../plugins/workflow/README.md)、[Workflow 与委派](tasks/workflow.md) |
| 内部接口与稳定 wire 协议怎样分开？ | [protocol](../protocol/README.md)、[协议与远程能力](protocols/overview.md) |
| 远程客户端怎样调用 Host 并关联响应？ | [hostapi](../plugins/hostapi/README.md)、[SDK](../plugins/sdk/README.md)、[兼容矩阵](protocols/compat-matrix.md) |
| MCP／LSP／SSH 进程与连接怎样管理？ | [mcp](../plugins/mcp/README.md)、[lsp](../plugins/lsp/README.md)、[ssh](../plugins/ssh/README.md) |
| 外部 web 内容怎样有界且不可信？ | [web](../plugins/web/README.md)、[hooks](../plugins/hooks/README.md) |
| 断线后的 UI 从哪里恢复？ | [客户端投影](../plugins/clientview/README.md)、[客户端与投影](client/overview.md) |
| 会话怎样按 principal 授权？ | [hostapi 控制器](../plugins/hostapi/README.md) |
| Desktop 载体的启动握手怎样？ | [desktophost](../plugins/desktophost/README.md)、[前端缺口](client/frontend-gap.md) |
| 每个源包组有没有处置结论？ | [全域行为账本](conformance/ledger.md)、[范围说明](conformance/scope.md) |
| 还有哪些未验证的兼容/发行缺口？ | [兼容缺口](compatibility/gaps.md)、[发行缺口](release/gaps.md) |
| 各平台的证据验到什么程度？ | [平台矩阵](execution/platform-matrix.md) |

## 开发与验收

修改前阅读[开发说明](development.md)、[文档规范](documentation.md)和[贡献指南](../CONTRIBUTING.md)。提交检查由[测试与门禁](testing.md)定义；新增模块同时增加本索引与包 README。

[系统复刻方案](10-plans/dsh-go-replication/plans.md)及其 [M0 状态](10-plans/dsh-go-replication/status.md)是下一阶段入口。[Cordis v0.1 方案](10-plans/cordis-v01/plans.md)与[Go 学习文档方案](10-plans/go-learning-docs/plans.md)保留各自版本的历史；验收报告用于查验证据，不是学习前置材料。
