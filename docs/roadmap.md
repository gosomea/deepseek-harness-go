# Go 复刻 DSH 路线

## 当前起点

当前实现是 Cordis 核心、M2 的装配层、M3 的数据契约、M4 的模型、工具与提示词层，、M5 的最小 Harness，以及 M6 的会话持久化与恢复，范围与差异见 [源码对应表](cordis/source-map.md)。仓库已有分类 README、公开注释、可运行教程、行为测试、双实现差分与 CI。M0（来源与差距审计）、M1（Cordis 下游基础与行为对照）、M2（Loader、配置身份与最小 Profile）、M3（对话词汇、Agent Scope 与内存 Session）与 M4（tRPC 模型适配、工具管线与提示词）都已通过各自执行契约：M2 交付 [loader](../loader/README.md) 与 [app](../app/README.md)；M3 交付 [llm](../llm/README.md)（消息与内容块词汇）、[scope](../scope/README.md)（Agent 可见层）与 [session](../session/README.md)（事件日志、surface 与投影）；M4 在 llm 上补齐流、assembler 与 Provider 契约，并交付 [tools](../tools/README.md)（schema、准入与执行管线）、[systemprompt](../systemprompt/README.md)（有序片段与工具 schema 装配）与 [plugins/llmtrpc](../plugins/llmtrpc/README.md)（框架导入的唯一边界）。M5 交付 [agent](../agent/README.md)（公共句柄与工厂契约）与 [agentloop](../agentloop/README.md)（默认驱动：turn/step、inbox、取消与关闭），并在 `app` 上提供最小 Harness 装配。M6 交付 [persistence](../persistence/README.md)（存储 seam、单写者规则、恢复状态与派生缓存）与 [sessionjsonl](../plugins/sessionjsonl/README.md)（自有格式的 JSON Lines 后端：帧、lease、flush 与损坏恢复）。M7 交付 [capability](../capability/README.md) 的五个 seam：[workspace](../capability/workspace/README.md)（执行世界与路径规则）、[fs](../capability/fs/README.md)（读写与冲突检测）、[subprocess](../capability/subprocess/README.md)（进程与输出限额）、[sandbox](../capability/sandbox/README.md)（单向升级阶梯）、[approval](../capability/approval/README.md)（一次性授权）；并由 [plugins/local](../plugins/local/README.md) 提供本机 Provider 与「先过策略再动手」的工具，`plugins/llmtrpc` 接入凭据与端点，`cmd/dsh-go` 提供会话子命令。真实端点与联网 smoke 单列，缺凭据记为未验证。**M8** 交付上下文层：使所有进入模型的内容先记录再渲染（[plugins/context](../plugins/context/README.md)）、[设置](../plugins/settings/README.md)与 [preset](../plugins/preset/README.md)、[技能](../plugins/skills/README.md)、[附件能力](../capability/attachment/README.md)与 [附件 Provider](../plugins/attachment/README.md)、[spill 能力](../capability/spill/README.md)与 [spill Provider](../plugins/spill/README.md)、[压缩能力](../capability/compaction/README.md)与 [压缩 Provider](../plugins/compaction/README.md)**M9** 交付子代理与长任务：[capability/subagent](../capability/subagent/README.md)（seam 与注册表）、[plugins/subagent](../plugins/subagent/README.md)（进程内 spawn／fork）、[capability/taskstate](../capability/taskstate/README.md)（goal／plan／todo 投影）、[capability/jobs](../capability/jobs/README.md) 与 [plugins/schedule](../plugins/schedule/README.md)（持久任务与可控时钟）、[capability/workflow](../capability/workflow/README.md) 与 [plugins/workflow](../plugins/workflow/README.md)（workflow seam 与 fake 引擎）。**M10** 交付协议与远程能力：[protocol](../protocol/README.md)（wire DTO／版本）、[plugins/hostapi](../plugins/hostapi/README.md)（网关分发/认证/订阅）、[plugins/sdk](../plugins/sdk/README.md)（SDK 客户端）、[plugins/mcp](../plugins/mcp/README.md) 与 [plugins/lsp](../plugins/lsp/README.md)（stdio 生命周期）、[plugins/ssh](../plugins/ssh/README.md)（远程执行 seam）、[plugins/hooks](../plugins/hooks/README.md)（hook 桥）、[plugins/web](../plugins/web/README.md)（有界 fetch）。**M11** 交付客户端投影与 Desktop Host 侧：[plugins/clientview](../plugins/clientview/README.md)（快照/增量/重连投影）、[plugins/hostapi](../plugins/hostapi/README.md) 控制器、[plugins/desktophost](../plugins/desktophost/README.md)（启动握手）；真实 Web UI/E2E/Desktop 打包记为缺口（见 [前端缺口](client/frontend-gap.md)）。**M12** 收口：[全域行为账本](conformance/ledger.md)（54 包组 + 9 vendor 机器校验，verified=39/adapted=16/gap=8）、[范围说明](conformance/scope.md)、[兼容缺口](compatibility/gaps.md) 与 [发行缺口](release/gaps.md)；本版本为范围明确的阶段版本，不声称完整复刻。[当前架构](architecture.md)仅描述已有代码。

[系统方案](10-plans/dsh-go-replication/plans.md)接续早期 Cordis 与文档计划，统一维护 M0–M12 的范围、依赖和取舍。[能力覆盖表](10-plans/dsh-go-replication/capabilities.md)为固定 DSH 基线的全部包组、vendor、应用与工程支持分配归属。

[阶段实施索引](10-plans/dsh-go-replication/stages/index.md)提供 13 份细化方案、81 个切片；每个阶段列出预计文件、学习实验、失败场景和退出门禁。

## 两项共同交付

每个阶段同时完成：

1. **复刻**：固定来源的行为契约、Go 实现、失败／取消／恢复测试、显式差异。
2. **概念对应与学习笔记**：Go 前置知识、问题引入、完整实验、TS↔Go 源码导航和预测练习。

阶段完成需要两项都有证据；通过测试或写完笔记都不能单独表示完成。学习内容从[学习地图](learning/index.md)进入；新笔记使用[模板](learning/note-template.md)。具体要求见[共同验收标准](10-plans/dsh-go-replication/acceptance.md)。

模型调用已选定 [tRPC-Agent-Go 模型层](10-plans/dsh-go-replication/plans.md#模型接入选型trpc-agent-go)，通过本项目 `llm` 契约与 Provider 适配器接入。M4 验证无密钥适配，M7 接入真实端点；Cordis、Session、工具管线和 Agent Loop 按 DSH 行为实现。

## 交付检查点

| 检查点 | 范围 | 学习结果 |
| --- | --- | --- |
| M0–M2 运行时与装配 | 来源审计、Cordis 下游基础、Loader 与最小 profile | 理解插件定义、实例、激活、依赖、资源和配置身份 |
| M3–M5 最小 Harness | 对话词汇、Session、fake LLM／工具、prompt、Agent Loop | 无密钥运行完整工具轮次，并从日志解释每一步 |
| M6–M7 本机可用 | 持久化恢复、真实模型、策略、工作区、文件／进程工具、CLI | 理解落盘边界、执行世界、取消与审批 |
| M8–M9 持续工作 | 上下文、压缩、技能、子代理、目标、持久任务 | 理解预算、会话谱系、持久状态与任务恢复 |
| M10–M11 协议与界面 | SDK/API/ACP/MCP、远程能力、Web 与 Desktop | 理解 Host 权威状态、协议兼容与客户端投影 |
| M12 全域收口 | 剩余 Provider／扩展、平台、性能、发行与完整文档 | 能从能力清单定位实现、差异与验证证据 |

检查点是阶段分组，实际依赖见[里程碑 DAG](10-plans/dsh-go-replication/plans.md#里程碑)。最小 Harness、本机可用和完整复刻分别验收；尚未通过兼容检查时，不承诺原 DSH 配置、Session 文件、客户端或插件可直接复用。

## 下一步与证据

M0 已核对固定来源、建立[逐行为审计](10-plans/dsh-go-replication/cordis-audit.md)并把既有测试与学习实验关联起来；[M0 契约](10-plans/dsh-go-replication/plan.json)三个节点全部通过。

M1 把审计判定的下游阻塞展开成 [M1 契约](10-plans/dsh-go-replication-m01/plan.json)：补条件可用性谓词（对应 Loader 的 `[Service.check]`），并建立 [Go 与固定 TypeScript 参考的差分对照](10-plans/dsh-go-replication/cordis-audit.md#m1-执行结果)。六个共享场景逐行比较两侧 trace，差异必须显式声明且带理由。

M2 把 Loader、配置身份与最小 profile 展开成 [M2 契约](10-plans/dsh-go-replication-m02/plans.md)：六个节点串行交付条目与工厂目录、父子树与分组、按 ID 更新与启停、bundle/profile 与 overlay、CLI 与示例。学习入口从[概念对照](loader/go-primer.md)到[装配教程](loader/tutorial.md)。YAML include、`!!js` 惰性表达式、配置热重载与原 profile 兼容明确归属 M12.2。

M3 把对话词汇、Agent 可见层与内存 Session 展开成 [M3 契约](10-plans/dsh-go-replication-m03/plans.md)：六个节点串行交付三个新包。`llm` 给出消息角色、内容块、工具调用／结果与无损 JSON codec；`scope` 给出作用域身份、父子链、按名遮蔽的贡献层与事件路由；`session` 给出只能追加的事件日志、连续序号、未知事件政策，以及从日志派生模型历史的 surface 与消息投影。学习入口从[对话消息](llm/go-primer.md)经 [Agent 可见层](scope/go-primer.md) 到[事件溯源](session/go-primer.md)与[投影](session/projection.md)。

M3 的 surface 语义有**双实现差分**证据：六个共享场景分别由 Go 与固定参考执行，trace 逐行比较，当前没有任何已声明差异，见 [Session 差分场景](../testdata/parity/session/README.md)。参考构建产物缺失时该对照记为未验证，不记为通过。

M7 的 CLI 有**确定性场景**证据：四条 fixture（拒绝先于副作用、无人应答不授权、只读拒绝命令、退出后可重开）由 `internal/testkit` 回放成逐行 trace，两次运行逐行一致，见 [CLI 场景](../testdata/parity/cli/README.md)；与参考实现的关系见 [差异清单](../testdata/parity/cli/DIVERGENCES.md)。

M6 的持久化有**确定性场景**证据：四条 fixture（重开一致、第二写者被拒、撕裂尾部可修复、未来版本拒绝）由 `internal/testkit` 回放成逐行 trace，两次运行逐行一致，见 [持久化场景](../testdata/parity/persistence/README.md)；与参考实现的关系在 [差异清单](../testdata/parity/persistence/DIVERGENCES.md) 中如实说明为「同一实现的确定性」，并明确**不兼容 DSH 会话格式**。

M5 的轮次有**确定性场景**证据：四条 fixture（完整轮次、排队输入、流中取消、被拒绝的工具调用）由 `internal/testkit` 回放成逐行 trace，两次运行逐行一致，见 [Agent 轮次场景](../testdata/parity/agentloop/README.md)；与参考实现的关系在 [差异清单](../testdata/parity/agentloop/DIVERGENCES.md) 中如实说明为「同一实现的确定性」，不是跨实现差分。

M4 的模型层有**回环 HTTP 契约**证据：fake Provider 与 tRPC 适配器在同一份 `llm.Request` 上产出一致的消息、终止原因与 usage 语义，跨 Provider 契约测试见 [provider_contract_test.go](../internal/testkit/provider_contract_test.go)；适配器与框架的真实差异逐条记录在 [tRPC 差异清单](../testdata/parity/llmtrpc/DIVERGENCES.md)。全部检查无密钥、无外网，真实端点属 M7。

M3 明确**未**交付：真实模型与流、assembler 与工具管线（M4）；磁盘持久化、单写者与格式互读（M6）；图片附件服务与真实 offload 投影、压缩（M8）；客户端读模型（M11）。`session` 不冒用 DSH 的会话文件格式名，也不声称能读取原有 DSH 数据。

**下一步是 M4**：tRPC 模型适配、工具管线与提示词。M3 已交出无框架类型的消息契约、事件日志与可重放的 surface，M4 的 fake Provider 与工具将建立在这份数据契约上。M1 未关闭的缺口已在 [M2 主题页](10-plans/dsh-go-replication-m02/plans.md#m1-遗留缺口的归属裁决)逐项裁决归属，其中 YAML include、`!!js` 惰性表达式、配置热重载与原 profile 兼容明确归属 M12.2，不得因为 M2 通过就认为这些能力已可用。

[Cordis 首轮方案](10-plans/cordis-v01/plans.md)和[Go 学习文档方案](10-plans/go-learning-docs/plans.md)保留原版本的验收历史。新的计划不复用历史“通过”来解锁未来阶段；代码与输入变化后的当前状态由各计划重新验证。
