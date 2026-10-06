# DSH 能力覆盖与 Go 归属

## 盘点边界

本表为规划分配，不是实现清单。所有路径基于 [固定源码树](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218)；详细包名来自 [来源清单](source-inventory.json)。当前 Go 仅实现 Cordis 的一个子集，下表各业务能力均未实现。阶段定义见 [总方案](plans.md#里程碑)。

一个源包组可能跨多个阶段；较早阶段只交付表中标明的子集。M12 必须展开包组内尚未审计的每个包，再按行为账本收口。表中 Go 路径是设计候选，只有已有代码链接才代表当前实现。

## 工作区能力

| DSH 包组 | 包数 | 阶段 | Go 归属（规划） | 必讲的概念 |
| --- | --- | --- | --- | --- |
| [core](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/core) | 8 | M1、M3–M5 | scope/session/systemprompt/tools/agent/agentloop | Agent 作用域、事件溯源、工厂与可替换循环 |
| [llm](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/llm) | 7 | M3–M4、M7–M8 | 自有 llm 契约、plugins/llmtrpc 与 retry／token Provider | DSH↔Go↔tRPC 类型映射、流组装、路由能力、attempt 与 usage |
| [boot](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/boot)、[bundle](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/bundle) | 11 | M2、M7、M10–M12 | loader/app 与 profile、bundle、插件管理 | 条目身份、层叠 patch、required 启动与配置重载 |
| [session](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/session) | 20 | M6、M8、M12 | persistence、projection 与相应插件 | 格式迁移、单写者、flush、统计、标题与遥测 |
| [storage](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/storage)、[session-query](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/session-query) | 8 | M6、M12 | storage 能力及 JSON/SQLite/query Provider | 领域存储与 Session 日志的边界；索引能否重建 |
| [interaction](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/interaction) | 5 | M4、M7、M9 | approval/questions/commands 与策略插件 | 请求与应答所有权、fail-closed、一次授权、权限预设 |
| [fs](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/fs)、[subprocess](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/subprocess)、[shell](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/shell)、[workspace](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/workspace) | 21 | M7 | capability 下的执行接口与本地 Provider | 执行世界、cwd、路径、文件观察、进程树与输出 |
| [sandbox](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/sandbox)、[guard](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/guard) | 6 | M7 | sandbox 能力、策略与平台实现 | 能力不足如何拒绝、不可放宽的守卫、超时与重复调用 |
| [credentials](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/credentials)、[identity](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/identity) | 6 | M7、M10–M12 | credentials/authorization 与身份 Provider | 凭据解析与使用边界、账户认证、匿名标识 |
| [settings](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/settings)、[preset](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/preset) | 4 | M2 最小配置；M8 完整能力 | settings、preset/persona 注册与插件 | 配置与会话覆盖、Agent 隔离、schema 与默认值 |
| [context](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/context)、[skill](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/skill) | 12 | M8 | 指令、引用、skills 发现与注入插件 | 模型可见来源、指令优先级、按需加载与执行环境 |
| [attachment](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/attachment)、[spill](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/spill)、[compaction](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/compaction) | 10 | M8 | 附件／spill／compaction 能力及 Provider | 不可变附件、超大输出引用、预算与摘要边界 |
| [subagent](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/subagent) | 10 | M9、M10 | subagent 能力、进程内／外部产品 Provider | fork 与 spawn、继承切点、父子所有权、控制与等待 |
| [goal](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/goal)、[plan](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/plan)、[todo](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/todo) | 6 | M9 | 持久状态与面向模型的工具／驱动插件 | 目标、计划和待办各自的日志与续跑规则 |
| [jobs](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/jobs)、[schedule](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/schedule)、[workflow](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/workflow)、[webhook](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/webhook) | 10 | M9、M10、M12 | 任务、时钟、工作流、Webhook Provider | 持久调度、重试幂等、外部触发与恢复 |
| [api](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/api)、[host](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/host) | 18 | M10–M11 | Host API、控制器、webserver、静态资源等 | 远程能力、认证、订阅、启动与 Host 所有权 |
| [sdk](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/sdk)、[acp](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/acp)、[typert](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/typert) | 8 | M10 | protocol 与 SDK/ACP 服务、客户端和生成工具 | wire DTO、代码生成、流与取消、版本协商 |
| [mcp](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/mcp)、[lsp](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/lsp)、[ssh](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/ssh) | 9 | M10 | 客户端协议和远程执行 Provider | 远程能力发现、进程管理、重连与执行世界 |
| [web](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/web) | 6 | M10 | web fetch/search 能力与多个 Provider | 外部内容、结果结构、限额、失败与可替换实现 |
| [terminal](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/terminal) | 3 | M7 基础；M10–M11 PTY | terminal 能力、shell Provider 与工具 | PTY 生命周期、输入输出流、大小与重连 |
| [hooks](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/hooks)、[extensions](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/extensions) | 7 | M10–M12 | hook 桥与受管理的外部插件通道 | 生命周期注入、协议桥与扩展权限；动态 JS 的替代 |
| [client](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/client) | 59 | M11 | 客户端继续使用 Web 技术栈；Go 提供 Host | 资源、模块、store、59 个包中的 UI 与模型投影 |
| [deliverables](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/deliverables)、[feedback](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/feedback) | 4 | M8、M11–M12 | 交付物／工作区变更／反馈插件与客户端 | 展示内容与模型上下文、反馈的持久事实与视图 |
| [browser-use](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/browser-use)、[computer-use](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/computer-use) | 2 | M12 | 对应能力 seam 与浏览器／桌面 Provider | 工具能力与载体分离、目标选择、状态观察与取消 |
| [ptc-runtime](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/ptc-runtime) | 2 | M12 | PTC 服务与进程／协议 Provider | 代码执行与子工具调用、结果归属、隔离与策略继承 |
| [document](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/document) | 1 | M12 | Office 转换 Provider | 外部进程／转换产物与原始附件的所有权 |
| [experimental](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/experimental) | 20 | M12，逐项立子计划 | Agent Teams、语音、驱动适配、WebWorker 等 | 实验能力边界与明确启用；不能以 experimental 为由从账本消失 |
| [runtime-diagnostics](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/runtime-diagnostics) | 1 | M5 起逐阶段；M12 收口 | 不变量与诊断插件 | 可观察错误、日志一致性与运行时断言 |
| [test-support](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/test-support)、[util](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/util) | 23 | M0 起逐阶段；M12 收口 | internal/testkit 与按消费者归属的辅助包 | fake、快照、回放、原子写、有界输出和时间抽象 |

表中合计 54 个包组、307 个工作区包；每个组恰好归属一行。该校验只证明没有遗漏包组，不证明已覆盖每个包的行为。特别是 core、session、client 和 experimental 必须在所属阶段继续拆解。

## Vendor 与语言机制

| 来源 | 阶段 | 复刻／适配选择 | 学习重点 |
| --- | --- | --- | --- |
| vendor/cordis | M0–M1，后续按消费者补齐 | 当前已有子集；差异由 [对应表](../../cordis/source-map.md)维护 | 插件实例、激活、依赖图、服务与可逆副作用 |
| vendor/loader、include、group | M2，M12 兼容收口 | 稳定条目与组树先行，YAML/include/patch 后补；动态导入用显式注册替代 | 配置不是运行时实例；解析失败与激活失败不同 |
| vendor/timer | M1 按需、M9 | 挂靠插件所有者的 Go timer/ticker，确保取消与清理；调度服务另属 schedule | 一次定时、周期定时与持久排程的区别 |
| vendor/hmr | M2 配置重载；M12 模块替代方案 | 不承诺原地替换 Go 模块；分别评估配置重载、进程重启和外部协议插件 | 保留实例身份与重建代码的边界 |
| vendor/schemastery | M2、M4 | 先定义校验契约，再选择 Go schema 方案；不逐行翻译 TS 类型推导 | 默认值、校验、模型工具 schema 与运行时数据 |
| vendor/cosmokit | 各消费者阶段 | 按需求用标准库或局部辅助函数替代，保留必要语义测试 | 语言便利机制与业务不变量的区别 |
| vendor/logger-console | M1、M7 | 当前 slog；随后补字段、错误与输出约定 | 可观察性与所有者定位 |

这 9 个 vendor 包均已归属。只在 Go 普通包中组织自写实现，避免误用 Go 的 `vendor/` 依赖目录。

## 应用、发行和工程支持

| 来源面 | 阶段与交付 | 学习／验收重点 |
| --- | --- | --- |
| apps/cli | M2 装配；M7 本机入口；M10 profiles | 单一启动器、配置优先级、stdout/stderr、退出码、取消与 home |
| apps/web、apps/desktop-host、apps/desktop | M11；M12 平台打包 | Go Host 协议、Web 客户端、Desktop 私有 profile、升级与关闭；载体不等于内核 |
| python/sdk、python/sdk-runtime | M10 协议；M12 分发 | Python 客户端互操作和平台运行时包是不同交付；不得仅凭 Go SDK 推断 Python 兼容 |
| native/system | M7 识别 OS 能力；M12 完整分配 | 按原生调用点决定 Go syscall、平台包或外部组件；逐 OS 记录支持矩阵 |
| website、docs | 全阶段文档；M12 网站与语言版本 | 当前中文 Go 文档；未来决定网站投影／双语范围并接入新鲜度检查 |
| scripts、Makefile、配置、.github、.gitlab-ci.yml | 全阶段门禁；M12 发行 | 映射测试、文档生成、依赖边界、构建／发布职责；不机械复制 pnpm 命令 |
| benchmarks、snapshots、test-support | 各功能阶段；M12 性能 | 可代表真实 Session、流和并发的基准；基线、环境和回归阈值 |
| patches、锁文件、第三方声明 | 每次引入依赖；M12 审计 | 源 DSH 补丁语义、Go 替代、许可证与可复现供应链 |
| .agents、AGENTS、贡献与安全规则 | M0 起持续维护 | 决策归属与历史证据；把适用规则落成 Go 检查，不复制失效的内部引用 |

工程支持、网站和实验功能仍属于完整范围。选择不复刻某项时，需要记录替代方式、用户影响和批准的范围变更；仅延后不能从完整复刻的分母移除。

## 基线升级

按固定提交读取来源，不追随本地工作树。升级时重新枚举清单，对比包与行为的增删改，再更新当前计划 revision 和相关 fixture。旧证据保留其原 SHA；新行为未审计前不沿用旧完成状态。
