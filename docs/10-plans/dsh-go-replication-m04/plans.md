# M4 执行契约：tRPC 模型适配、工具管线与提示词

## 现状

M0、M1、M2 与 M3 的执行契约都已通过。M3 交出了三样 M4 需要的东西：`llm` 的无框架消息与内容块词汇、`session` 的权威事件日志与连续序号、以及可重放并与固定参考逐行对照的 surface。M4 在这份数据契约上第一次引入**一次模型请求**与**一次工具调用**。

M4 的待实施范围由[阶段索引](../dsh-go-replication/stages/index.md)和 [M4 细化方案](../dsh-go-replication/stages/m04-model-tools.md)定义：六个切片 M4.1–M4.6、退出门禁 G4。本页是 M4 的入口，事实归属仍在细化方案和[共同验收页](../dsh-go-replication/acceptance.md)，本页不复制它们的规则。

## 目标

- **复刻**：[[goal:replication]] 同一套 DSH-Go 请求、流与完成契约同时接到 fake Provider 与 tRPC-Agent-Go 模型层，并让 fake 工具经过可观测的 schema 校验、准入、单调 guard 与 post/finalize 顺序。
- **学习**：[[goal:learning]] 让 Go 读者从 delta、message 与 attempt 的区别出发，解释流式显示为什么不等于持久事实；用被拒绝的 fake 工具解释能力、策略与“拒绝不进工具体”。
- **证据**：[[goal:evidence]] 每条行为有固定 TS 来源、Go 入口、行为测试与显式差异；必要检查无密钥、无外网；参考构建产物缺失时差分检查跳过并记为未验证。

## 执行契约

[plan.json](plan.json) 是本阶段的执行契约。七个节点按下面的依赖推进：

[[node:m41-llm-stream]] → [[node:m42-assembler]] → [[node:m43-llmtrpc]] → [[node:m46-examples]] → [[node:g4-acceptance]]

[[node:m44-tools]] → [[node:m45-systemprompt]] → [[node:m46-examples]]

M4 细化方案把 M4.4（工具管线）写成独立于 M4.1–M4.3 的分支。本契约**保留两条根**：`m41-llm-stream` 与 `m44-tools` 都无前驱，在 `m45-systemprompt` 汇合，随后两条汇合路径一起进入 `m46-examples` 与收口节点。这是细化方案的实际形状，不是排版选择：工具管线只依赖 `session`/`scope` 的类型，不依赖流契约。

### 汇合点为什么放在 m45 而不是 m46

`m45-systemprompt` 装配“提示词片段 + 工具 schema”，它同时需要 M4.2 的请求契约与 M4.4 的工具定义。把汇合点提前到 m45，可以让 m46 只负责跨 Provider 契约与实验，而不是既汇合又验收——后者会让“契约没对齐”和“实验写错”两类失败混在同一个节点。

### 串行执行的理由

`m41`、`m44`、`m45` 都要把新运行时包登记进共享的覆盖率门禁（`scripts/doccheck/main.go`、`docs/testing.md`），而登记必须发生在包已存在之后——否则门禁会因为“没有覆盖数据”而拒绝一个尚未创建的包。同时 `m43` 会写入 `go.mod` 与 `go.sum`，`m46` 的跨 Provider 契约测试依赖它。本阶段按仓库当前的默认串行方式推进，不并行 claim 同一 worktree。

## 产物所有权与不变量

契约遵守 M2、M3 已确立的三条不变量：

1. **inputs 不得被本节点或链上更晚的节点写入。**
2. **不使用目录级 writes**：每个 `writes` 逐文件列出。
3. **检查只断言本节点有权写入、或由已通过前驱产出的文件。**

共享门禁文件 `scripts/doccheck/main.go`、`scripts/doccheck/main_test.go`、`docs/testing.md` 被 `m41`、`m44`、`m45` 三个切片先后修改。它们因此不出现在任何节点的 `inputs`，也不作为非最终节点的 `outputs`，最终状态由 [[node:g4-acceptance]] 冻结。

### 对上游产物的已知影响

| 被改文件 | 上游归属 | 后果 |
| --- | --- | --- |
| `scripts/doccheck/main.go`、`main_test.go` | M2 `g2-acceptance`、M3 `g3-acceptance` outputs | M2、M3 收口节点在协议中转为 stale，需按其计划重新验收或接受其历史结论 |
| `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md` | M0 `cordis-audit`、M3 `g3-acceptance` outputs | 同上 |
| `docs/testing.md` | M2 `g2-acceptance`、M3 `g3-acceptance` outputs | 同上 |
| `docs/10-plans/dsh-go-replication/stages/index.md` | M0 `documents` + M3 `g3-acceptance` outputs | M0 全部节点变为 `plan changed; revise required`（M2 与 M3 已使 M0 处于该状态）；该文件由 [[node:g4-acceptance]] 更新当前起点 |
| `go.mod`、`go.sum` | 仓库基线 | M4 首次引入第三方依赖；M1–M3 的 receipt 不受内容影响，但工具链要求随之写入主题页 |

`stages/m04-model-tools.md` 与 `stages/m03-session.md` 列在 M0 契约的 `documents` 中，**不改**：它们是 M0 时点的方案文本。M4 的实际交付范围与实测差异记录在**本页**。

## 阶段内要冻结的选择

| 选择 | 决定 | 理由 |
| --- | --- | --- |
| 模型库与版本 | `trpc.group/trpc-go/trpc-agent-go` **v1.11.2** | 固定参考中该库承担模型层；本机核对 `v1.11.2` 可用，其 `go.mod` 声明 `go 1.21`，本项目 `go 1.22` 满足。版本写死在 `go.mod`，不在实现中浮动 |
| 框架导入边界 | 只有 `plugins/llmtrpc` 可以 import 该库 | 总方案要求核心包与持久化格式不得暴露 tRPC 的 Request、Response 或 Session 类型；该不变量由 `boundary` 检查断言，不靠约定 |
| 流与完成的关系 | delta 只用于实时显示；settlement 形成持久事实 | 参考的 `assistant-stream` 把一次 attempt 压成可存储记录，读取方不必重新物化成员。失败的 attempt 不得伪装成成功消息 |
| tool-call 参数分片 | 按 `id` 分桶累积，未闭合的调用在结算时报错 | 多个 tool-call 的分片可能交错到达；按 index 或按到达顺序都会把两个调用混成一个 |
| usage 语义 | `unknown usage` 与“零 usage”是两种值 | 参考区分“没有 usage 信息”和“usage 全为零”；合并二者会让计费与诊断都失真 |
| 工具准入顺序 | `schema 校验 → pre-execute → 单调 guard → 执行 → post/finalize → result` | 参数错误或拒绝必须在**进入工具体之前**发生；被拒绝的调用不得产生副作用，且最终结果唯一 |
| 工具拒绝的信号 | 用可观察的错误结果，不用 panic 或静默跳过 | 参考把拒绝表达为规范化结果加错误；观察者看到的必须是权威结果，而不是被吞掉的事件 |
| 提示词顺序 | 按显式 `order` 稳定排序，同序按注册顺序 | 请求要可确定性比较；依赖 map 遍历顺序会让同一份输入产生不同请求 |
| tRPC 适配器职责 | 只做类型转换、能力映射与生命周期 | 重试与 attempt 的记录由本项目策略拥有；接入时核对 SDK 内部重试，避免两层重试或不可观察的重复请求 |
| 不支持的能力 | 显式声明或返回可观察错误 | 不能静默丢字段后声称完整复刻 |

Go 差异记录在 `llm/README.md`、`tools/README.md`、`systemprompt/README.md`、`plugins/llmtrpc/README.md` 的限制章节；回环场景中的显式差异记录在 `testdata/parity/llmtrpc/DIVERGENCES.md`。这些路径由本阶段节点创建（见[执行契约](#执行契约)），因此此处按仓库的文档规范写成代码文字而不是链接。

## 学习交付

下表是 [[node:m41-llm-stream]]、[[node:m43-llmtrpc]]、[[node:m44-tools]]、[[node:m45-systemprompt]] 与 [[node:g4-acceptance]] 要交付的主题。路径在创建前保持代码文字，创建后由 [[node:g4-acceptance]] 在[学习地图](../../learning/index.md)中改为实际链接并纳入“当前可学”表。

| 主题 | 笔记（由 m41–m45 创建） | 前置 |
| --- | --- | --- |
| C06 流与适配 | `docs/llm/streaming.md`、`docs/llm/trpc-adapter.md` | C06 对话与流、Go 通道与 context |
| C08 能力与工具 | `docs/tools/go-primer.md`、`docs/tools/execution-pipeline.md` | C02 服务可见性、C06 内容块 |
| 提示词装配 | `docs/systemprompt/assembly.md` | C08、scope 贡献层 |

三个主题都按[学习笔记模板](../../learning/note-template.md)组织。M4 通过后 C08 从[学习地图](../../learning/index.md)的规划行转为可学行，C06 的“流与适配”补齐为完整主题。

## 阶段结论与未验项

**本页结论在 [[node:g4-acceptance]] 之前写入，由收口节点作为最终产物冻结并验收**；在验收之后追加会让已通过的证据立即失效。这与 M2、M3 由收口节点冻结主题页的做法一致。

本阶段的边界必须在交付时一起写明，不得读成已完成：

- **没有 Agent 循环**。`agent`、`agentloop` 与 inbox 属 M5；本阶段只有“一次请求”与“一次工具调用”的契约与管线，没有 turn/step/attempt 状态机。
- **没有持久化**。M4 不写 Session 文件；attempt 的持久表示属 M6，本阶段只保证流可以结算成可存储的记录形状。
- **没有真实端点**。全部必要检查用 fake Provider 与 httptest 回环；凭据、真实 API 与联网 smoke 属 M7，缺密钥不算通过。
- **没有策略与沙箱**。M4 只有准入接口与单调 guard，审批、沙箱与执行世界属 M7；本阶段的工具是 fake，不产生本机副作用。
- **压缩、附件与 token 计量属 M8**。本阶段的请求装配不做预算、截断或 offload。
- **tRPC 的 Agent、Runner、Session 与工具执行器不承担本项目运行时职责**；集成范围限定为模型层。
- **参考构建产物缺失时差分记为未验证**。`DSH_PACKAGES` 未指向固定参考 `packages`，或对应 `lib/` 构建产物不存在时，实时对照检查跳过并记为未验证；已记录的参考 trace 仍参与比较。

## 本阶段实际运行的命令与结果

本契约节点（即本阶段的执行契约落盘工作，不属于 `plan.json` 的七个节点）实际执行的命令、退出码与结果。工作目录为仓库根，`SKILL` 为 `plan-and-phase` 技能目录的绝对路径；`plan.py` 位于技能目录而非本仓库 `scripts/`（本仓库 `scripts/` 下只有 `doccheck`）。

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `python3 "$SKILL/scripts/plan.py" validate docs/10-plans/dsh-go-replication-m04/plan.json` | 0 | `{"valid": true, "plan": "dsh-go-replication-m04", "nodes": 7, "revision": "b8f37086abb59f86952ef660dc6cfc3008029e6514adcb4d629f8fcae1b9ed56"}` |
| `python3 "$SKILL/scripts/plan.py" ready docs/10-plans/dsh-go-replication-m04/plan.json` | 0 | 就绪节点 `m41-llm-stream`、`m44-tools`；其余五个节点 `blocked`（依赖未通过），`complete: false` |
| `python3 "$SKILL/scripts/plan.py" render docs/10-plans/dsh-go-replication-m04/plan.json`（首次） | 2 | `{"error": "unknown document reference: node:m04-contract"}`；原因：主题页里写了 supervisor 的 stage id `m04-contract`，而它不是本契约的节点 id |
| 同上，把该引用改为代码文字后重跑 | 0 | `{"directory": ".../docs/10-plans/dsh-go-replication-m04", "complete": false}`，生成 `status.md` 与 `dag.mmd` |
| `python3 "$SKILL/scripts/plan.py" validate docs/10-plans/dsh-go-replication-m04/plan.json`（render 之后复跑） | 0 | `{"valid": true, "plan": "dsh-go-replication-m04", "nodes": 7, "revision": "f39e778cd8a84dafd262f50ce9a94a61def13917ffff644123356364d200c8fd"}` |

首次 `validate` 曾以退出码 2 失败，原因是 `documents` 中的 `docs/10-plans/dsh-go-replication-m04/plans.md` 尚未存在（`[Errno 2] No such file or directory`）。按协议先落盘主题页与 `dag.mmd`，随后 `validate` 与 `ready` 通过。这次失败记录在案：契约的 `documents` 是真实文件依赖，不能先引用后创建。

本节点共发生**两次失败**，都已修正并记录在上表：（1）`validate` 首次退出码 2，`documents` 里的主题页尚未存在；（2）`render` 首次退出码 2，主题页误用 supervisor 的 stage id 作为 `[[node:]]` 引用。两次都不是门禁问题，而是交付顺序与引用对象写错。

`render` 生成的 `status.md` 与 `dag.mmd` 由 CLI 维护，不手工标记通过。各切片与 `g4-acceptance` 的检查结果在它们各自的 `verify` 之后补记，`make check` 由 `g4-acceptance` 的 `check` 在其 `verify` 内从仓库根执行，随后才 `render`/`export` 到 `validation/<run-id>`。

验证材料统一导出到 `docs/10-plans/dsh-go-replication-m04/validation/<run-id>/`：**每次导出使用新的 run-id，不覆盖也不改写历史 run**；旧证据继续证明旧提交，不得把旧计划长期显示为当前通过。export 只导出已经 `verify` 过的结果，不代替检查。

注意：`validate` 打印的 `revision` 由 `plan.json` 与 `documents`（含本页）的内容共同决定，因此**每次改写本页都会改变该值**。上表记录的是各次运行当时的输出；当前生效的 revision 以 `status.md` 为准。

### 评审要求修订后的复跑（本轮）

按评审意见补齐命令记录与「导出不覆盖历史 run」约束后，重跑自检：

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `python3 "$SKILL/scripts/plan.py" validate docs/10-plans/dsh-go-replication-m04/plan.json` | 0 | `{"valid": true, "plan": "dsh-go-replication-m04", "nodes": 7, "revision": "32d5fbfa52f232932668cf0b0daf29af46d07dd33e748d20c3848143d2afe053"}` |
| `python3 "$SKILL/scripts/plan.py" ready docs/10-plans/dsh-go-replication-m04/plan.json` | 0 | 就绪 `m41-llm-stream`、`m44-tools`；`status.complete: false` |
| `python3 "$SKILL/scripts/plan.py" render docs/10-plans/dsh-go-replication-m04/plan.json` | 0 | 生成 `status.md`（计划版本 `32d5fbfa…`）与 `dag.mmd` |

本轮还出现过一次**工具链使用错误**（不是门禁问题）：把 `ready` 的输出接进 `python3 -c` 时按 `d['complete']` 取值，实际字段是 `d['status']['complete']`，导致 `KeyError: 'complete'`。该管道退出非零，已改用正确路径复跑并通过；`ready` 本身始终 exit 0。

修订内容：（1）补齐 `render` 首次 exit 2、修正后 exit 0、以及 `render` 后 `validate` exit 0 / revision `f39e778` 三条记录；（2）把「每次导出使用新的 run-id，不覆盖也不改写历史 run」写入 `plan.json` 的 `constraints` 与本页；（3）改写本页后 `revision` 由 `f39e778` 变为 `32d5fbfa`，这正是「documents 参与 revision」的直接体现。

### M4 切片与收口实际运行的命令与结果

七个契约节点全部通过 `plan-and-phase` CLI 的 `claim → start → verify`（含语义评审），随后 `render`。`verify` 逐个执行契约登记的检查，以下为结果摘要。工作目录为仓库根，`SKILL=/Users/yuqixian/forever-skills/.agents/skills/plan-and-phase`。

| 节点 | verify 内执行的检查（节选） | 结果 |
| --- | --- | --- |
| `m41-llm-stream` | `go test -race -count=1 ./llm/...`、`./internal/testkit/...`、`go test -race -count=1 -coverprofile=coverage.out ./...`、`doccheck -coverage`、`doccheck`、`doccheck -examples`、`coverage-registration`、`doc-structure` | 8/8 passed，exit 0 |
| `m42-assembler` | `./llm/...`、全仓竞态覆盖率、覆盖率门禁、`doccheck`、`-run Assembler\|Registry\|Delta\|Usage`、`doc-structure` | 全部 passed，exit 0 |
| `m43-llmtrpc` | `./plugins/llmtrpc/...`、`./llm/...`、`boundary`（框架导入唯一边界）、全仓竞态覆盖率、覆盖率门禁、`doccheck`、`coverage-registration`、`doc-structure` | 全部 passed，exit 0 |
| `m44-tools` | `./tools/...`、全仓竞态覆盖率、覆盖率门禁、`doccheck`、`-run Pipeline\|Guard\|Schema\|Registry`、`coverage-registration`、`doc-structure` | 全部 passed，exit 0 |
| `m45-systemprompt` | `./systemprompt/...`、全仓竞态覆盖率、覆盖率门禁、`doccheck`、`coverage-registration`、`doc-structure` | 全部 passed，exit 0 |
| `m46-examples` | `./examples/model/...`、`./examples/tools/...`、`-run ProviderContract ./internal/testkit/...`、`doccheck -examples`、全仓竞态覆盖率、覆盖率门禁、`doccheck` | 全部 passed，exit 0 |
| `g4-acceptance` | **`make check`**（仓库根，exit 0）、`git diff --check`、`provider-contract`、`llmtrpc-behavior`、`boundary`、`gate-integrity`、`coverage-registration`、`module-index`、`stage-index`、`learning-index` | 10/10 passed，exit 0 |

`make check` 的实际输出（本次运行，逐条取自 exit 0 的运行记录）：格式与 vet 通过；竞态测试全仓通过；覆盖率门禁逐包打印 `Cordis 97.7% / testkit 92.1% / loader 92.5% / app 90.7% / llm 91.8% / scope 92.3% / session 94.0% / tools 96.8% / systemprompt 97.6% / llmtrpc 92.1%`，均不低于 90%；`documentation gate passed`（三次，分别对应文档、`-examples`）；`go build ./...` 通过。

### 本阶段真实失败与处置

| 失败 | 真实原因 | 处置 |
| --- | --- | --- |
| `m41` 首次 verify：`full-race-coverage` 失败 | 登记 `tools`/`systemprompt`/`llmtrpc` 后，覆盖率门禁自测 fixture `TestCoverageGateChecksEveryGatedPackage` 的合成 profile 仍只有原 7 个包 | 扩展有效样例并为每个新包新增 `-below-threshold` 与 `-missing` 无效样例；**90% 阈值与门禁逻辑未改**，另在 `docs/testing.md` 立“门禁变更记录” |
| `m43` 首次评审：`review references unknown output evidence` | `plugins/llmtrpc/adapter_coverage_test.go` 是真实交付物但契约漏登 | 补登契约 `outputs`/`writes`（`validate` exit 0），`cancel` + `revise` 后在新 revision 上重跑 m41–m43 |
| `m44` 首次 verify：`doc-structure` 失败 | `docs/tools/execution-pipeline.md` 缺 note-template 小节 | 按 M3 体例重写该页 |
| `g4` 首次 verify 失败 | `stages/index.md` 未登记 M4 契约入口；`docs/README.md` 未以 `../plugins/llmtrpc/README.md` 形式链接 | 两处修正后重跑，10/10 通过 |

契约修订（补登 `adapter_coverage_test.go`）改变了 revision `bd6e1da → 9cf625f2`，使已通过的 m41–m43 转为 `plan changed; revise required`。按协议先 `cancel` 活动 attempt、再 `revise`，并在新 revision 上重新验收，而不是让旧结论继续生效。

### 验证材料导出

七个节点全部通过后执行 `render` 与 `export`。`export` **每次写入一个新的 run 目录**，不覆盖也不改写历史 run；因此本页不锚定某一个 run-id——它是本页的 inputs 之一，固定引用会让本页的每次改写立刻使刚导出的证据失效。查验证据时按 run-id 查看 `validation/` 下的目录，当前生效的 revision 以 `status.md` 为准。

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `python3 "$SKILL/scripts/plan.py" render docs/10-plans/dsh-go-replication-m04/plan.json` | 0 | `{"directory": ".../docs/10-plans/dsh-go-replication-m04", "complete": true}`，写出 `status.md` 与 `dag.mmd` |
| `python3 "$SKILL/scripts/plan.py" export docs/10-plans/dsh-go-replication-m04/plan.json --out docs/10-plans/dsh-go-replication-m04/validation/<run-id>` | 0 | `{"directory": ".../validation/<run-id>", "complete": true}`，写出 `report.json`（含 7 份 receipt 与全部事件）与 `report.md` |

导出的 `report.json` 中每个节点各有一份 receipt，逐条记录该节点检查的 `passed` 与 `exit_code`；其中 `g4-acceptance` 的 receipt 含 **`make-check: passed=true, exit_code=0`**，以及 `diff`、`provider-contract`、`llmtrpc-behavior`、`boundary`、`gate-integrity`、`coverage-registration`、`module-index`、`stage-index`、`learning-index` 共 10 项检查。`report.json` 的 `status.complete` 为 `true`。

**本轮门禁样例变更的证据**：登记 `tools`／`systemprompt`／`plugins/llmtrpc` 到 `coverageRequirement` 后，门禁自测 `TestCoverageGateChecksEveryGatedPackage` 出现的真实失败与修复（有效样例扩展 + 每个新包的 `-missing`／`-below-threshold` 无效样例）记录在 `docs/testing.md` 的「门禁变更记录：M4 登记三个新运行时包」一节；`g4-acceptance` 的 `gate-integrity` 检查断言覆盖率比较式与 Makefile 子命令未被削弱，`coverage-registration` 检查断言三个新包均已登记。这些证据同时出现在导出的 `report.json` 与本页。

### 尚未验证

- 本阶段**没有调用任何真实模型端点**，也没有凭据与联网 smoke；全部检查使用 fake Provider 与进程内 `httptest` 回环。真实端点、审批与沙箱属 M7。
- 框架语义结论基于 `trpc-agent-go v1.11.2` 的探测；升级后需重新探测并更新 `testdata/parity/llmtrpc/DIVERGENCES.md`。
- **没有磁盘持久化**：attempt 的紧凑记录、Session 文件与格式互读属 M6。
- `docs/llm/streaming.md` 与 `docs/llm/trpc-adapter.md` 的差异表列出未实现项（紧凑 attempt 记录、reasoning 签名、图片 offload 等），这些不是本阶段的缺陷，而是已声明的边界。
- **本地 `make check` 通过不代表 CI 已通过**。本次运行的命令、退出码与结果记录在本节；CI 结果需另行查看 GitHub Actions。
- `go.mod` 尚未加入 tRPC 依赖；v1.11.2 与 `go 1.21+` 的要求来自本机对照 `@v/list` 与 `@v/v1.11.2.mod`，其**在新 `go.sum` 中的实际解析结果尚未验证**。
- `boundary`、`gate-integrity`、`module-index`、`stage-index`、`learning-index` 等检查已登记进契约，但**均未运行**，它们断言的目标文件（`tools/`、`systemprompt/`、`plugins/llmtrpc/`）也尚未创建。
- 本页为 [[node:g4-acceptance]] 的最终产物，届时冻结；本轮结论不代表 M4 已通过。
