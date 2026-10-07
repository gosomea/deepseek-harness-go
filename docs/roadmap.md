# Go 复刻 DSH 路线

## 当前起点

当前实现是 Cordis 核心加上 M2 的装配层，范围与差异见 [源码对应表](cordis/source-map.md)。仓库已有分类 README、公开注释、可运行教程、行为测试与 CI。M0（来源与差距审计）、M1（Cordis 下游基础与行为对照）与 M2（Loader、配置身份与最小 Profile）都已通过各自执行契约：M2 交付 [loader](../loader/README.md)（条目身份、工厂目录、JSON 编解码、父子树、更新与启停）与 [app](../app/README.md)（命名 bundle、按序 overlay、required 就绪判定），并加上 [cmd/dsh-go](../cmd/dsh-go/README.md) 配置检查入口。Session、LLM、工具与 Agent Loop 尚未实现；[当前架构](architecture.md)仅描述已有代码。

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

**下一步是 M3**：对话词汇、Agent Scope 与内存 Session。M2 已交出 loader 的条目身份、工厂目录、父子树、更新与启停，以及 app 的 bundle/profile 与 required 就绪判定；M1 未关闭的缺口已在 [M2 主题页](10-plans/dsh-go-replication-m02/plans.md#m1-遗留缺口的归属裁决)逐项裁决归属，其中 YAML include、`!!js` 惰性表达式、配置热重载与原 profile 兼容明确归属 M12.2，不得因为 M2 通过就认为这些能力已可用。

[Cordis 首轮方案](10-plans/cordis-v01/plans.md)和[Go 学习文档方案](10-plans/go-learning-docs/plans.md)保留原版本的验收历史。新的计划不复用历史“通过”来解锁未来阶段；代码与输入变化后的当前状态由各计划重新验证。
