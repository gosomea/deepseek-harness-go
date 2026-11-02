# M5 执行契约：无密钥的完整 Agent 轮次

## 现状

M0–M4 的执行契约都已通过。M4 交出了「一次请求 + 一次工具调用」的契约与管线：`llm` 的流与 assembler、`tools` 的准入与六阶段执行、`systemprompt` 的片段装配，以及 `plugins/llmtrpc` 这唯一的框架导入边界。但**没有循环**：没有任何东西把一次用户输入变成一串 turn/step，也没有东西决定下一次模型请求从哪里来。

M5 的待实施范围由[阶段索引](../dsh-go-replication/stages/index.md)和 [M5 细化方案](../dsh-go-replication/stages/m05-agent-loop.md)定义：六个切片 M5.1–M5.6、退出门禁 G5。本页是 M5 的入口，事实归属仍在细化方案与[共同验收页](../dsh-go-replication/acceptance.md)。

M5 是方案里的**「可学习的最小 Harness」检查点**：它不等于完整 DSH 复刻，只证明主链在无密钥条件下可运行、可重建、可取消。

## 目标

- **复刻**：[[goal:replication]] 由统一 app 装配启动一个 Agent，用 fake Provider 经**两次模型请求与一次 fake 工具调用**完成一次轮次，并证明每次模型可见输入都能从 session 日志重建。
- **学习**：[[goal:learning]] 让 Go 读者区分句柄、实例与运行驱动；从一次轮次的两条时间线（权威日志与实时流）解释 turn/step/attempt/inbox；用排队输入与取消各自定位一个可预测的生效点。
- **证据**：[[goal:evidence]] 每条行为有固定 TS 来源、Go 入口、行为测试与显式差异；必要检查无密钥、无外网、使用可控时钟与 fake；参考产物缺失时差分记为未验证。

## 进入条件

必需前驱：[M2 的 G2](../dsh-go-replication/stages/m02-composition.md) 与 [M4 的 G4](../dsh-go-replication/stages/m04-model-tools.md)。

- **G2** 提供显式插件树与最小 profile 的装配能力，[[node:m56-harness]] 的 fake profile 建立在它之上。
- **G4** 提供 `llm` 的请求/流契约与 `tools` 的执行管线，[[node:m53-turn-step]] 的 turn→step→请求→结算→工具批直接消费它们。

两个 gate 都是本阶段开工的前置，不通过则不启动本阶段。M4 的 G4 已通过，证据见 [M4 主题页](../dsh-go-replication-m04/plans.md)与其 `validation/` 导出。契约内以 `constraints` 的首条显式登记；`depends_on` 与 `requires_gates` 都保持为空，因为这两个 gate 属于**其他计划**。这不是省略：本仓库的 `plan.py` 对两个字段都要求引用**本计划内**的节点 id，`validate` 会以 `required gate is not an upstream gate: m51-agent-contract / g2-acceptance` 拒绝跨计划引用。该真实约束已写入契约的 `constraints`。

**fake-only 确定性要求**（G5 的必要条件均不含真实模型）：全部必要检查使用 fake Provider、fake 工具与可控时钟；不读真实时间、不发网络请求、不需要密钥。真实模型验证不是 G5 的必要条件，缺密钥不算通过。

## 执行契约

[plan.json](plan.json) 是本阶段的执行契约。七个节点串行推进：

[[node:m51-agent-contract]] → [[node:m52-scoped-setup]] → [[node:m53-turn-step]] → [[node:m54-inbox]] → [[node:m55-cancel]] → [[node:m56-harness]] → [[node:g5-acceptance]]

### 为什么 M5 是串行而不是分支

M5 细化方案把六个切片依次写成同一链条，本契约沿用该形状而非另设并行根。理由是真实的：`agent` 的公共契约要先冻结，默认驱动才能实现它；driving 的 turn/step 顺序要先成立，inbox 才有「在哪一步生效」这个可回答的问题；inbox 的排队语义要先确定，取消才能说明「保留 inbox 但不派发新工具」。链上每一环的下游都依赖上游的语义，而非仅仅依赖上游的文件存在。

### 阶段内要冻结的选择（契约这里裁决，细化页保留原文）

**1. 句柄、实例与驱动的分离。** `agent` 只定义公共契约：`Handle`（外部消费者看到的句柄）、`Factory`（创建 seam）与状态。默认驱动在 `agentloop`，它实现 `Factory` 并在作用域内发布 Agent。**`agent` 不得 import `agentloop`**，扩展插件依赖 `agent` 而不能反向绑定默认循环。该不变量由 `agent-boundary` 检查断言。

**2. 工厂与句柄的权限分离。** 工厂提供方负责清理它创建的活跃实例；**裸查询句柄不获得销毁权限**。这条区分来自细化页的验收项，也是 M9 子代理委派的前提：能「找到」一个 Agent 不等于能「关掉」它。

**3. 轮次事件在 session 日志中的表示。** turn、step、request、attempt 与 tool 事件都写入权威日志，且每条事件引用它所属的 turn 与 step。实时流（`agent/assistant-stream` 对应的 Go 侧投影）只是投影，**不构成持久事实**。每次请求必须能仅从日志重建——这是 M3 数据契约的直接延续。

**4. 工具调用先记录后执行。** tool-call 在执行前写入日志；已发生的结果按契约结算。被取消而**从未派发**的 tool-call 不得出现在日志里被当作已执行。这条区分让「取消后不派发新工具」成为可断言的事实，而不只是行为描述。

**5. fake-only 确定性。** 全部必要检查使用 fake Provider、fake 工具与可控时钟；不读真实时间、不发网络请求、不需要密钥。真实模型验证不是 G5 的必要条件。

**6. agentloop 不得有专用分支。** 默认循环不能出现供应商、文件系统或网络专用代码；这些能力通过插件贡献，而不是硬编码为 loop 的特权。该不变量由 `loop-purity` 检查断言。

## 产物所有权与不变量

契约遵守 M2–M4 已确立的三条不变量：

1. **inputs 不得被本节点或链上更晚的节点写入。**
2. **不使用目录级 writes**：每个 `writes` 逐文件列出。
3. **检查只断言本节点有权写入、或由已通过前驱产出的文件。**

共享门禁文件 `scripts/doccheck/main.go`、`scripts/doccheck/main_test.go`、`docs/testing.md` 被 `m51` 与 `m52` 两个切片先后修改（每个新运行时包在同一片内登记进 `coverageRequirement`）。它们因此不出现在任何节点的 `inputs`，也不作为非最终节点的 `outputs`，最终状态由 [[node:g5-acceptance]] 冻结。

### 对上游产物的已知影响

| 被改文件 | 上游归属 | 后果 |
| --- | --- | --- |
| `scripts/doccheck/main.go`、`main_test.go` | M2/M3/M4 收口节点 outputs | 相应收口节点在协议中转为 stale，需按其计划重新验收或接受其历史结论 |
| `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md` | M0 `cordis-audit`、M4 `g4-acceptance` outputs | 同上 |
| `docs/testing.md` | M4 `g4-acceptance` outputs | 同上 |
| `docs/10-plans/dsh-go-replication/stages/index.md` | M0 `documents` + M4 `g4-acceptance` outputs | M0 全部节点变为 `plan changed; revise required`；该文件由 [[node:g5-acceptance]] 更新当前起点 |
| `app/` | M2 `g2-acceptance` outputs | M5.6 新增 `app/harness.go` 与 `app/harness_test.go`，属新增文件而非改写既有装配逻辑 |

`stages/m05-agent-loop.md` 列在 M0 契约的 `documents` 中，**不改**：它是 M0 时点的方案文本。M5 的实际交付范围、裁决与实测差异记录在**本页**。

## 学习交付

下表是 [[node:m51-agent-contract]]、[[node:m53-turn-step]]、[[node:m54-inbox]]、[[node:m55-cancel]] 与 [[node:g5-acceptance]] 要交付的主题。路径在创建前保持代码文字，创建后由 [[node:g5-acceptance]] 在[学习地图](../../learning/index.md)中改为实际链接并纳入「当前可学」表。

| 主题 | 笔记（由 m51–m55 创建） | 前置 |
| --- | --- | --- |
| C09 Agent 轮次 | `docs/agent/go-primer.md` | C02 服务可见性、C08 工具 |
| turn 与 step | `docs/agentloop/turns.md` | C07 事件溯源、C09 |
| inbox 与追加输入 | `docs/agentloop/inbox.md` | turn 与 step |
| 取消与关闭 | `docs/agentloop/cancellation.md` | inbox、C03 资源所有权 |

四个主题都按[学习笔记模板](../../learning/note-template.md)组织。M5 通过后 C09 从[学习地图](../../learning/index.md)的规划行转为可学行。

## 阶段结论与未验项

**本页结论在 [[node:g5-acceptance]] 之前写入，由收口节点作为最终产物冻结并验收。**

### 写序约定（M4 的实际教训）

`plans.md` 是本契约的 `documents` 之一，对它的**任何写入都会改变 revision**，并使全部已通过节点转为 `plan changed; revise required`。M4 阶段这已实际发生三次，代价是三次 revise 与全节点重跑。因此 M5 采用固定顺序：

1. 各切片节点依次 `claim → start → verify`；
2. 主题页内容（含实际运行过的命令、退出码与结果）**在 g5 之前定稿**；
3. `g5-acceptance` 在自己的 `verify` 内从仓库根执行 `make check`，随后 `render → export` 写入**新的** `validation/<run-id>`；
4. **export 之后不再编辑本页**。若确需再改，必须按协议对新 revision 重新验收所有受影响节点，不能让旧结论继续生效。

导出使用新的 run-id，不覆盖也不改写历史 run。查验证据时按 run-id 查看 `validation/` 下的目录；当前生效的 revision 以 `status.md` 为准。

### 未验边界

本阶段的边界必须在交付时一起写明，不得读成已完成：

- **没有真实模型**。全部必要检查使用 fake Provider 与可控时钟；真实端点、凭据与联网 smoke 属 M7，缺密钥不算通过。
- **不是完整 DSH 复刻**。M5 是「可学习的最小 Harness」检查点，只证明主链可运行、可重建、可取消。
- **没有持久化**。M5 的 Session 是内存日志；磁盘格式、单写者、恢复与格式互读属 M6。
- **没有本机副作用**。工具是 fake，不读写文件、不起进程；审批与沙箱属 M7。
- **没有上下文管理**。token 预算、spill 与 compaction 属 M8。
- **没有子代理与持久任务**。委派、mailbox 与排程属 M9；M5 只交付 inbox 与取消这两个前提。
- **差分检查依赖参考构建产物**。`DSH_PACKAGES` 未指向固定参考 `packages`，或对应 `lib/` 构建产物不存在时，实时对照检查跳过并记为未验证。

## 本阶段实际运行的命令与结果

本契约节点（执行契约落盘工作，不属于 `plan.json` 的七个节点）实际执行的命令与结果。工作目录为仓库根，`SKILL` 为 `plan-and-phase` 技能目录的绝对路径；`plan.py` 位于技能目录而非本仓库 `scripts/`（本仓库 `scripts/` 下只有 `doccheck`）。

实际命令序列（按执行顺序）：先落盘 `plan.json`，再写本页与 `dag.mmd`，然后依次执行下表命令。

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `python3 "$SKILL/scripts/plan.py" validate docs/10-plans/dsh-go-replication-m05/plan.json` | 0 | `{"valid": true, "plan": "dsh-go-replication-m05", "nodes": 7, "revision": "ee66cf302db6d5d810a9adb0551b1bb5297a19f24e457f5e93668115c15fe5e9"}` |
| `python3 "$SKILL/scripts/plan.py" ready docs/10-plans/dsh-go-replication-m05/plan.json` | 0 | 就绪节点 `m51-agent-contract`；其余六个节点 `blocked`（依赖未通过），`status.complete: false` |
| `python3 "$SKILL/scripts/plan.py" render docs/10-plans/dsh-go-replication-m05/plan.json` | 0 | `{"directory": ".../docs/10-plans/dsh-go-replication-m05", "complete": false}`，生成 `status.md` 与 `dag.mmd` |
| 同上 `validate`（在 render 与本页写入之后复跑） | 0 | `{"valid": true, "plan": "dsh-go-replication-m05", "nodes": 7, "revision": "19c8b81942213c2ef0a7fcb5e6471f8dc23317675d05ea13908518485d417acf"}` |
| `validate`（在补齐进入条件、并把跨计划 gate 的字段语义限制写入契约之后复跑） | 2 → 0 | 失败一次：`{"error": "required gate is not an upstream gate: m51-agent-contract / g2-acceptance"}`；修正 `requires_gates` 后通过：`{"valid": true, "nodes": 7, "revision": "9a217d1100a50d8f4b583738f960467479e46d0e665b02639b2fee79ff7ce383"}` |
| `validate` / `render` / `validate`（最终一轮，本页写定之后） | 0 / 0 / 0 | `validate` 返回 `{"valid": true, "nodes": 7}`；`render` 生成 `status.md` 与 `dag.mmd`；`render` 之后复跑 `validate` 返回**同一** revision，证明 `render` 只写 `status.md`/`dag.mmd`、不改 `plans.md` |

**为什么本页不锚定自己最终的 revision**：`plans.md` 属契约 `documents`，对它的**每次写入都会改变 revision**——上表每一行都记录了一个当时真实生效的值，但记录本身又会把它改写。因此本页只记录「实际运行过哪些命令、退出码与结果」，**当前生效的 revision 以 [status.md](status.md) 为准**；`render` 之后 `plans.md` 不再改动，`status.md` 与 `plan.json` 构成该 revision 的稳定来源。

### 切片实现实际运行的命令与结果

七个契约节点用 `plan-and-phase` CLI 逐个 `claim → start → verify`（含语义评审）。`verify` 在节点内执行契约登记的检查；下表是本次实现期间从仓库根实际运行、并写入证据的命令与结果。

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go test -race -count=1 ./agent/... ./agentloop/...` | 0 | 两个新包全部通过（`agent`、`agentloop`） |
| `go test -race -count=1 -timeout=120s ./agentloop/...` | 0 | 取消与关闭等待的回归测试通过 |
| `go test -count=1 ./app/...` | 0 | Harness 装配与完整轮次断言通过 |
| `go test -count=1 ./internal/testkit/` | 0 | 四条轮次场景回放与校验通过 |
| `go test -count=1 ./examples/harness/...` | 0 | 示例输出与 `expected.txt` 逐字一致 |
| `go run ./examples/harness` | 0 | `requests=2 toolBodyRuns=1`；重建历史为 user／assistant(tool-call)／tool／assistant 四条；`second request carries the tool result=true` |
| `go test -count=1 -coverprofile=coverage.out ./...` | 0 | 全仓测试通过并产出覆盖率 |
| `go run ./scripts/doccheck -coverage coverage.out` | 0 | 12 个受门禁包全部 ≥90%：Cordis 97.7%、testkit 92.1%、loader 92.5%、app 91.0%、llm 91.8%、scope 92.3%、session 94.0%、tools 96.8%、systemprompt 97.6%、llmtrpc 92.1%、agent 97.5%、agentloop 91.2% |
| `go run ./scripts/doccheck` / `-examples` | 0 | `documentation gate passed`（文档、链接、公开注释、完整程序） |
| `make check`（仓库根，预备运行） | 0 | 格式、vet、竞态测试、覆盖率门禁、文档门禁、示例快照、`go build ./...` 全部通过。**该次运行不替代 `g5-acceptance` 在其自身 `verify` 内执行的 `make check`**，后者结果写入导出的 `validation/<run-id>` |

### 实现期间的真实失败与处置

| 失败 | 真实原因 | 处置 |
| --- | --- | --- |
| 第一次请求后不再发第二次请求 | `runSteps` 在工具结果后因 inbox 为空提前退出 | 修实现：模型请求过工具调用时必须继续；`answeredTools` 单独表达该原因 |
| `agent loop` 超过步数上限（无限循环） | 续跑前只检查 `inbox.len() > 0` 却从不 claim，队列永不为空 | 修实现：续跑前先 `claimInto`，把 steering 记成 user 消息 |
| `Dispose` 在活动轮次中挂死 | `dispose` 等待 `a.done`，而该通道只在同一函数末尾关闭 | 修实现：活动轮次等 `idle` 通道；补「活动轮次中 dispose」回归测试 |
| 取消被报成 `ErrIncompleteStream` | 症状覆盖了原因 | 修实现：上下文已取消时以 `context.Canceled` 为准 |
| `Lookup` 返回的句柄可断言成 `Owner` | 权限分离名存实亡 | 修实现：返回只转发 `Handle` 的包装类型 |
| `session: invalid event: event data has no message` | `user/message` 用嵌套载荷，与其 surface 规则不符 | 修实现：`user/message` 用顶层消息，其余四类用 `"message"` 嵌套 |
| `agentloop` 覆盖率 64.3% → 75.7% → 83.3% → 91.2% | 新增包未登记门禁且分支未覆盖 | 先删除不可达死代码，再补真实分支测试；**未降低 90% 阈值、未改门禁逻辑** |
| `testkit` 覆盖率 89.3% | 新场景回放的 action 路径未被覆盖 | 补 `waitForAttempt`／`send`／`cancel` 的真实测试；顺带修出 `block-end` 块构造缺失的真实缺陷 |
| `Harness.Start` 缺 provider/model source | 装配未填 route，assistant 消息被消息词汇拒绝 | 修实现：Harness 记录已安装 Provider 的 route 与 model 并填入 spec |
| fixture 的工具调用未闭合 | `full-turn`／`rejected-input` 缺 `block-end` | 修 fixture 与场景转换器 |

### 门禁变更

M5 把 `agent` 与 `agentloop` 登记进 `scripts/doccheck` 的 `coverageRequirement`，并同步扩展 `TestCoverageGateChecksEveryGatedPackage`：`all-gated-packages-pass` 纳入两个新包的达标 profile，并为每个新包新增 `<pkg>-below-threshold` 与 `<pkg>-missing` 两类无效样例。**覆盖率阈值 90% 未改动，`Makefile` 与其他门禁定义未改动**，变更记录见 [测试与门禁](../../testing.md) 的「门禁变更记录：M5 登记 agent 与 agentloop」。

### 尚未验证

- 本节点只完成契约落盘与结构自检；**没有运行任何 Go 测试或 `make check`**，因此 M5 的实现状态仍为未开始。
- `agent/`、`agentloop/` 与 `examples/harness` 尚未创建；契约中登记的 `agent-boundary`、`loop-purity` 与各 `doc-structure` 检查均**未运行**。
- 本页为 [[node:g5-acceptance]] 的最终产物，届时冻结；本轮结论不代表 M5 已通过。
