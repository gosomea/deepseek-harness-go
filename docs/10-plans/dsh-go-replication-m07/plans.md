# M7 执行契约：真实模型、本机执行策略与 CLI

## 现状

M0–M6 的执行契约都已通过。M6 结束时，会话可以关闭并重开：`persistence` 定义存储 seam 与单写者规则，`plugins/sessionjsonl` 用自己的格式把日志落到磁盘，恢复能报告「还欠什么工作」而不重放副作用。但那份能力仍然**碰不到真实世界**：

- 模型只有 fake Provider 与回环 HTTP fixture，没有真实端点与凭据；
- 没有任何本机能力：不能读文件、不能执行命令；
- 没有 CLI，全部验收都靠包级测试与示例程序。

M7 的待实施范围由[阶段索引](../dsh-go-replication/stages/index.md)与 [M7 细化方案](../dsh-go-replication/stages/m07-local-product.md)定义：七个切片 M7.1–M7.7、退出门禁 G7。本页是 M7 的入口，事实归属仍在细化方案与[共同验收页](../dsh-go-replication/acceptance.md)。

## 目标

- **复刻**：[[goal:replication]] 在临时工作区通过 CLI 使用真实模型与本机工具，恢复 Session，并证明**每个副作用都先经过明确策略**；无审批应答者时不得自动放行，也不得隐式降级隔离。
- **学习**：[[goal:learning]] 让 Go 读者用**同一个 cwd** 解释「文件世界」与「进程世界」；预测无人应答、拒绝与取消三种审批结果；区分本地可验证的假数据测试与需要凭据的真实 smoke。
- **证据**：[[goal:evidence]] 每条行为有固定 TS 来源、Go 入口、行为测试与显式差异；真实 API 先过本地回环 fixture，联网 smoke 单列且**不把缺密钥写成通过**。

## 进入条件

必需前驱：[M6 的 G6](../dsh-go-replication-m06/plans.md)。

M6 提供 M7 需要的全部输入：可重开的会话与明确的恢复语义（`persistence.Resume`/`Restore`）、单写者与 flush 承诺、以及「恢复只描述不行动」的既有纪律。M7 在这些之上接入**进程、文件与策略**。

M6 还留下两条直接适用的教训，本契约把它们写成了约束：

1. **不要改动已验收前驱的产物**。M6 期间曾越界改写 `Makefile` 与 `.github/workflows/check.yml`（M1 `parity-harness` 与 M2/M3 收口节点的 outputs），被评审要求回退。M7 改为只读断言（`gate-integrity`），不改这两个文件。
2. **主题页必须一次定稿，且不锚定易变标识**。M6 期间主题页被反复编辑，每次都触发「revision 失效 → 全部节点重跑」。M7 的约束是：主题页内容在 g7 之前定稿、export 后不再编辑，且**不得把结论写死在具体 revision 或 run-id 上**。

按 M5/M6 契约登记过的真实限制：`plan.py` 的 `depends_on` 与 `requires_gates` 只接受**本计划内**的节点 id，跨计划引用会被 `validate` 以 `required gate is not an upstream gate` 拒绝。因此 G6 前驱以本节文本与本契约 `constraints` 登记。

## 执行契约

[plan.json](plan.json) 是本阶段的执行契约。八个节点串行推进：

[[node:m71-capability-contract]] → [[node:m72-approval-policy]] → [[node:m73-local-providers]] → [[node:m74-local-tools]] → [[node:m75-real-endpoint]] → [[node:m76-cli]] → [[node:m77-cli-e2e]] → [[node:g7-acceptance]]

### 为什么顺序如此（含一处真实的并行点）

`m71 → m72 → m73 → m74` 是严格链：能力契约先冻结「执行世界」的定义，审批才能在它之上定义「谁批准什么」，本地 Provider 才能实现「怎么执行」，工具才能注册「暴露哪些动作」。策略必须先于能力暴露——**先有策略与执行能力，再暴露写文件和命令工具**，这是细化方案的明确要求。

`m75`（真实端点与凭据）只依赖 `m71`，因为模型端点与文件/进程能力是两条独立的接入路径；但契约仍把它排在 `m74` 之后执行，避免同时推进入口与策略、让失败归因变难。

`m76`（CLI）**同时**依赖 `m74` 与 `m75`：一次完整的 CLI 轮次要能调模型、又能改文件，缺任何一半都跑不通。

`m77`（无密钥 E2E）依赖 `m76`：端到端场景需要一个真实的 CLI 进程边界。

### 阶段内要冻结的选择（契约这里裁决）

**1. 副作用之前先过策略。** 任何写文件、删除、启动进程或执行命令，必须先在审批与沙箱策略上得到结论。`plugins/local/` 不得直接调用 `os/exec`、`os.WriteFile`、`os.Create` 或 `os.Remove`，只能经 capability seam 执行。该边界由 `policy-before-effect` 检查断言——**先用静态检查把「不能绕过」写成机器可验证的事实，再谈行为测试**。

**2. 无审批应答者不得自动放行。** `unavailable`（无人应答）、`rejected`（拒绝）、`cancelled`（取消）三种结果都必须**不授权**。headless 政策必须显式声明是拒绝还是要求一次性授权，**绝不允许「默认放行」**。三种结果各有行为测试。审批绑定具体调用身份：一次授权只对它针对的那次调用有效，重复回答、迟到回答与取消都不得被当成新授权。

**3. 沙箱只能严格更宽地升级。** 升级阶梯为 `read-only` → `workspace-write` → `danger-full-access`，且校验在**执行时**而不是在 tool schema 里（schema 是全局的，而有效模式是每次调用的真相）。运行配置所需能力缺失时**拒绝启动**，不得隐式降级隔离。

**4. 路径规范化与执行世界一致。** 读取路径与执行 cwd 必须属于同一个 workspace 世界；越界路径、符号链接逃逸、大小写与平台分隔符差异都有测试，不一致的配置被拒绝。

**5. 真实 API 先过本地回环 fixture。** 真实端点接入先以本地 HTTP 回环验证协议与错误映射；联网 smoke 单列运行、结果单独记录，**缺密钥时记为未验证**。凭据只从环境/配置文件读取，不写入仓库、不写入日志、不进入 fixture。

**6. 平台矩阵如实。** Linux 与 macOS 的行为有本地证据，**Windows 的 shell 与路径规则单独验证，不以 Bash 测试代替**；不支持项必须明确阻塞对应配置，而不是静默降级。

## 产物所有权与不变量

契约遵守 M2–M6 已确立的三条不变量：

1. **inputs 不得被本节点或链上更晚的节点写入。**
2. **不使用目录级 writes**：每个 `writes` 逐文件列出。
3. **检查只断言本节点有权写入、或由已通过前驱产出的文件。**

共享门禁文件 `scripts/doccheck/main.go`、`scripts/doccheck/main_test.go`、`docs/testing.md` 由**创建新运行时包的同一切片**登记：`capability/{workspace,fs,subprocess,sandbox,approval}` 五个包在 [[node:m71-capability-contract]] 登记，`plugins/local` 在创建它的 [[node:m73-local-providers]] 登记。因此这些文件不出现在任何节点的 `inputs`（`m73` 的 inputs 也不含它们），最终状态由 [[node:g7-acceptance]] 冻结。

注册断言的集合按「当时是否已存在」分层，这是为了避免断言一个尚未创建的包：`m71` 的 `coverage-registration` 只列当时已存在的包；`m73` 与 `g7` 列**含 `plugins/local` 的完整集合**，从而保证该包确实受 90% 覆盖率门禁约束。

### 对上游产物的已知影响

| 被改文件 | 上游归属 | 后果 |
| --- | --- | --- |
| `scripts/doccheck/main.go`、`main_test.go` | M2–M6 收口节点 outputs | 相应收口节点在协议中转为 stale |
| `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md` | M0 与 M5/M6 收口节点 outputs | 同上；M7 只在 g7 写一次 |
| `docs/testing.md` | M5/M6 收口节点 outputs | 同上 |
| `docs/10-plans/dsh-go-replication/stages/index.md` | M0 `documents` + M5/M6 收口节点 outputs | M0 全部节点变为 `plan changed; revise required`；由 g7 更新交接 |
| `plugins/llmtrpc/` | M4 的适配器 | M7.5 新增端点与凭据接线，既有回环适配语义不改 |
| `app/` | M5 的装配 | M7.6 让 CLI 复用唯一 app，不新增第二套装配 |
| `Makefile`、`.github/workflows/check.yml` | M1 `parity-harness`、M2/M3 收口节点 | **本阶段不改写**，只由 `gate-integrity` 只读断言 |

`stages/m07-local-product.md` 列在 M0 契约的 `documents` 中，**不改**：它是 M0 时点的方案文本。

## 学习交付

| 主题 | 笔记（由 m71–m76 创建） | 前置 |
| --- | --- | --- |
| C11 能力与执行世界 | `docs/execution/capabilities.md` | C05–C09 |
| 审批与沙箱升级 | `docs/execution/approval.md` | 能力与执行世界 |
| 平台矩阵 | `docs/execution/platform-matrix.md` | 本地 Provider |
| C11 CLI 与会话恢复 | `docs/cli/session-cli.md` | C10 持久化与恢复 |

四个主题都按[学习笔记模板](../../learning/note-template.md)组织。M7 通过后 C11 从[学习地图](../../learning/index.md)的规划行转为可学行；索引更新只在 [[node:g7-acceptance]] 发生一次。

## 契约冻结后的 revision 变更（一次写全，可追溯）

本页与 `plan.json` 共同决定 revision。契约在 `399d20d7` 通过之后，`plan.json` 又被改写数次。**下面是完整链条、每轮增删的产物与理由。**

| revision | 触发原因 | 增删内容 | 性质 |
| --- | --- | --- | --- |
| `399d20d7` | 契约首次通过 | — | 初始冻结 |
| `b6c28297` | 逐节点 verify 前核对声明产物 | 改名：`capability/approval/policy.go` → `request.go`、`plugins/local/fs.go` → `paths.go`；删除实现中不存在的 `capability/workspace/paths.go`（路径逻辑写在 `workspace.go` 内）；补登实现中真实产生的测试与 parity 文档 | 把声明对齐真实交付物 |
| `842ab0f1` | g7 的 outputs 缺共享门禁文件 | 把 `scripts/doccheck/main.go`、`main_test.go`、`docs/testing.md` 登记为 **g7 的 outputs**（原先只在 writes） | 避免评审引用未声明产物 |
| `ff54f2e0` | 主题页定稿（命令与失败记录） | 无产物增删 | 记录 |
| `622acb40` | `policy-before-effect` 首跑失败 | 检查范围从「整个 `plugins/local`」收窄到「工具层 `plugins/local/tools.go`」；同步改写与之矛盾的约束文字 | **修正检查定义的真实缺陷** |
| `9884091e` | m71 评审引用未声明产物 | 补登 `capability/README.md` | 对齐真实交付物 |
| `e887d270` | m72 评审引用未声明产物 + 主题页一次性定稿 | 主题页补全 revision 链、`policy-before-effect` 收窄依据、契约产物合并交代；无产物增删 | 记录 |
| `6c17b0ad` | m73 的 `start` 反复报 `missing evidence file` | 删除契约中两处指向**已合并文件**的引用：m73 的 inputs 含 `capability/workspace/paths.go`、m74 的 inputs 含 `plugins/local/fs.go` | **修正契约残留** |
| `9b388a90` / `e10e7c49` | `coverage-gate` 空输出失败 | 两次尝试消除覆盖率 profile 竞争：先各自独立文件，再改为 `mktemp` 私有临时文件 | **修正检查定义的真实缺陷** |
| `189c1ce0` | m77 评审引用漏登产物 | 补登 `testdata/parity/cli/scenarios/read-only-command.json` | 对齐真实交付物 |
| `b4d57e02` | g7 的 `module-index` 首次失败 | 三处索引一次写完：`docs/README.md`、`docs/roadmap.md`（含五个子包链接）、`docs/learning/index.md`（C11/C11b/C11c 转为可学行）、`stages/index.md`（M7 登记与 M8 交接） | **补齐真实缺口** |

### `policy-before-effect` 收窄的裁决依据

首跑时该检查扫描整个 `plugins/local/`，因此命中 `os.WriteFile`、`os.Remove`、`os/exec`、`exec.Command`（见下）。但它扫描的目录**正是 capability seam 的实现**：`OSFS` 与 `OSProcess` 的职责就是调用这些标准库函数。把它们列为违禁，等于要求实现者不要实现。

真正的规则是：**工具层不得绕过 seam 直接触达世界**。因此检查改为：

1. 只扫描工具文件 `plugins/local/tools.go`；
2. 断言该文件**不含** `os/exec`、`exec.Command`、`exec.Cmd`、`os.WriteFile`、`os.Create`、`os.Remove`；
3. 断言它**确实**经 seam 调用（`.fs.Read`、`.fs.Write`、`.process.Run`）。

实测结果：`tools.go` 的违禁命中为 **none**，且三项 seam 调用**全部存在**。约束文字同步改为同样口径——原先那句「`plugins/local/` 不得直接调用 os/exec 等」与实现自相矛盾，是真实的表述缺陷。

### 被静默丢弃的契约产物：合并与替代

对齐产物时，若只保留「已存在的文件」，会静默丢掉三处**契约点名但实际以别的方式实现**的产物。这里逐项交代，不靠过滤器消失：

| 契约产物 | 实际载体 | 理由 |
| --- | --- | --- |
| `testdata/parity/llmtrpc/endpoint/README.md`、`loopback.json` | `plugins/llmtrpc/loopback_test.go` 中的进程内 `httptest` 服务器 | 回环 fixture 以**进程内**服务器实现更可靠：真 HTTP 客户端、真解码器、真翻译都在跑，只有端点是本地的，且不需要额外的 fixture 文件参与快照比对。契约原设想是一份可复用的 JSON 正文；实现改为测试内固定的 SSE 正文，语义相同 |
| `testdata/parity/cli/scenarios/approve-once.json` | `read-write-refuse.json` 中的 `allowed-once` 步骤 | 「允许一次」不是独立场景，而是拒绝场景的**后半段**：先拒绝、再允许，才能证明「拒绝不写、允许才写」的顺序。拆成两个文件会丢掉这个顺序 |
| `capability/workspace/paths.go`、`capability/approval/policy.go`、`plugins/local/fs.go` | 分别并入 `workspace.go`、`request.go`、`paths.go` | 这些逻辑的实现文件与契约的预想拆分不同；文件是手段，行为与测试才是产物 |

**结论**：三处是**契约修订**（产物合并/替换），不是「不存在就不声明」。

## 写序约定（M4、M5、M6 三轮的实际教训）

`plans.md` 是本契约的 `documents` 之一，对它的**任何写入都会改变 revision**，并使全部已通过节点转为 `plan changed; revise required`。M4、M5 与 M6 三阶段都已实际为此付出 revise 与全节点重跑的代价（M6 甚至付出多轮）。因此 M7 沿用固定顺序：

1. 各切片节点依次 `claim → start → verify`；
2. 主题页内容（含实际运行过的命令、退出码与结果、未验边界）**在 g7 之前一次定稿**；
3. `g7-acceptance` 在自己的 `verify` 内从仓库根执行 `make check`，随后 `render → export` 写入**新的** run-id；
4. **export 之后不再编辑本页**。若确需再改，必须按协议对新 revision 重新验收所有受影响节点。

本页**不写死 revision 与 run-id**：它们会随写入本页而立即过时。**当前生效的 revision 与导出目录以 `status.md` 和 `validation/` 下的实际目录为准**（两者由 `plan.py render/export` 生成，不在此手写链接）。

## 本阶段实际运行的命令与结果

工作目录为仓库根；`SKILL` 为 `plan-and-phase` 技能目录的绝对路径。

### 交付实现实际运行过的命令

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go test -race -count=1 ./capability/...` | 0 | 五个能力包的契约与行为测试全部通过 |
| `go test -race -count=1 ./plugins/local/...` | 0 | 本机 fs/进程 Provider 与工具行为通过（含拒绝审批不写文件、进程树取消、输出限额） |
| `go test -race -count=1 ./plugins/llmtrpc/...` | 0 | 凭据解析、端点校验、四态 smoke 判定通过 |
| `go test -race -count=1 ./cmd/dsh-go/...` | 0 | 会话命令的退出码、恢复语义、信号与不泄漏密钥通过 |
| `go test -count=1 ./examples/cli/...` | 0 | 示例输出与 `expected.txt` 逐字一致 |
| `go run ./examples/cli` | 0 | 读取→拒绝写入（文件未变）→允许写入→只读拒绝命令→重开（`events=4 messages=2`） |
| `go test -count=1 -run CLIScenario ./internal/testkit/...` | 0 | 四个 CLI 场景回放通过 |
| `go run ./scripts/doccheck` / `-examples` | 0 | `documentation gate passed` |

### 完整门禁（阶段内预备运行，不替代 G7）

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `make check` | 2（**失败**） | `testkit coverage below 90%: 864/968 statements`（89.3%）——新增场景回放代码未被覆盖；未放宽阈值，改为补测试 |
| `make check` | 2（**失败**） | `TestReadWriteRefuseScenario` 失败：场景在**最终**状态上检查"拒绝后文件内容"，而随后的允许写入已改变文件 |
| `make check` | 2（**失败**） | `approval` 并发测试失败：同一交换的两个调用方看到不同答案 |
| `make check` | 0 | 20 个受门禁包全部 ≥90%；文档门禁、示例快照、`go build ./...` 通过 |

**这些 `make check` 是阶段内预备运行，不能替代 `g7-acceptance` 在其自身 `verify` 内从仓库根执行的那一次。** 后者结果与 `render → export` 产物一并写入导出的 `validation/<run-id>`。

### 实现期间的真实失败与处置

以下是**主题页先前定稿之后**又发生的两次真实失败，按同样口径补记：

| 失败 | 真实原因 | 处置 |
| --- | --- | --- |
| m71 的 `policy-before-effect` 检查失败 | 检查扫描整个 `plugins/local/`，而该目录正是 seam 的实现，`os.WriteFile`/`os/exec` 是其职责 | 修检查定义：收窄到工具层并改为断言「经 seam 调用」；同步修正与之矛盾的约束文字（见上节） |
| m71 的评审被拒、节点停在 `verifying` | evidence 引用了未声明的 `capability/README.md` | 补登产物并 revise，在新 revision 上重跑 |
| m72 的评审被拒、节点停在 `verifying` | evidence 引用了属于 m71 的 `capability/approval/approval.go` | 把 m72 的 evidence 收窄到它自己声明的产物；`approval.go` 的契约定义归 m71，`request.go` 的渠道实现归 m72 |
| m73 的 `start` 连续两次报 `missing evidence file: capability/workspace/paths.go`，节点停在 `claimed` | 契约里 **inputs** 仍引用已合并的 `capability/workspace/paths.go`（路径逻辑写在 `workspace.go` 内）；`start` 会校验 inputs 存在 | 先误判为状态文件陈旧而 `cancel`+`retry` 两次（无效），捕获 `start` 的完整输出后定位到契约字段；删除该引用，并顺带清掉 m74 的 `plugins/local/fs.go`。教训：**`start` 的报错指向契约字段，不是运行时状态**，重试不能解决 |
| m72 的 `coverage-gate` 在 verify 内 `exit 1` 且**输出为空**（手动按同一 argv 运行却通过） | `full-race-coverage` 与 `coverage-gate` **并发写同一个 `coverage.out`**，互相截断 | 先改为各自独立的 profile 文件，再改为 `mktemp` 私有临时文件——不依赖相邻检查的时序，也不在仓库根留下产物 |
| m71 的 verify 报 `changes outside declared writes: coverage-gate.out, coverage-race.out` | 上一版把覆盖率 profile 写在仓库根，超出了声明写入范围 | 这是**保护正确生效**：处置是把 profile 移出仓库根，而**不是**放宽保护。同时撤销那一次把 `coverage-*.out` 加进 `writes` 的做法 |
| m77 的评审被拒、节点停在 `verifying` | evidence 引用了契约漏登的 `testdata/parity/cli/scenarios/read-only-command.json`（该 fixture 真实存在且被门禁点名：只读策略拒绝命令） | 补登该产物为 m77 的 outputs/writes；它是**漏登**，不是新建 |
| g7 的 `module-index` 首次失败 | `docs/README.md` 与 `docs/roadmap.md` 未**同时**链接六个新包的 README（只在 README 加了链接，roadmap 漏了子包） | 修文档而非改门禁：roadmap 补齐 capability 五个子包与 `plugins/local` 的链接；复跑 `module-index: PASS` |
| 批量索引更新中途 `AssertionError` | `learning/index.md` 中 C11 行的实际措辞与脚本预期不同（标题为「C11 策略与执行世界」） | 按实际措辞重写并完成索引更新；断言失败前已写入的部分经复核无误 |
| m77 首轮场景断言把「最终状态」当作「拒绝当下」 | 两个期望描述不同时点：拒绝后文件未变，而随后的允许写入本来就会改变它 | 修回放器：在**拒绝当刻**取样，与最终状态分开 |

**这些都是本机结果。** 本地 `make check` 通过不代表 CI 已通过；真实端点 smoke 缺凭据时记为未验证。

### 关于执行顺序的如实说明

本阶段的实现工作**先于**逐节点的 `claim → start → verify` 完成，逐节点记录属于在实现落定后按协议补跑的流程。这与本页「写序约定」描述的「边实现边逐节点验收」并不一致：**实际是"先实现、后按契约逐节点留证"**。每次契约变更都在新 revision 上从零重跑全部受影响节点，因此最终结论仍对应同一冻结 revision；但这一点不应被读成严格按节点顺序推进。


| 失败 | 真实原因 | 处置 |
| --- | --- | --- |
| `go vet` 报 context 泄漏 | 适配器在 `Done()` 为 nil 时既未启动 goroutine 又持有 cancel | 修实现：nil 表示不可取消，直接返回 `context.Background()` |
| 世界内路径被判为"在外部" | 开启符号链接解析时未一并规范化**根** | 修实现：构造时同样解析根 |
| `the parent environment leaked into the command` | `exec.Cmd` 把 nil `Env` 解释为**继承父环境**，与契约"nil 表示空环境"相反——凭据泄漏 | 修实现：nil 改为空非 nil 切片 |
| `NewTools` 允许既无 fs 也无 process | `read_file` 会 nil panic | 修实现：构造时拒绝无提供者的配置 |
| 同一交换的两个并发调用方看到不同答案 | 第二个呈现者在首个未落定答复时被直接拒绝 | 修实现：等待 settled 通道取得同一答复；其取消也被处理 |
| 恢复得到 `messages: 0` | CLI 只记录 turn 边界，**未记录消息事件** | 修实现：记录 `user/message` 与嵌套的 `assistant/message` |
| 取消被报为普通失败（exit 1 而非 130） | 取消在 setup 阶段未被分类 | 修实现：入口先检查 `ctx.Err()` 并报中断 |
| `testkit` 89.3% 低于门禁 | 新增 CLI 场景回放器未被覆盖 | 补真实失败路径测试（取消审批、缺失文件、长内容裁剪、无路径拒绝检查）；**未降低阈值** |
| 场景断言在最终状态上检查"拒绝当下" | 两个期望描述**不同时点** | 修回放器：在拒绝当刻取样，与最终状态分开 |
| 示例中臆造 API（`NewFakeProvider` option 形式、`llm.CapabilityText`、`AddScript`） | 凭印象写调用 | 查真实 API 后改为 `NewFakeProviderWithOptions`、`CapabilityStreaming`、排空流取样 |

### 门禁变更

M7 把六个新运行时包登记进 `scripts/doccheck` 的 `coverageRequirement`：`capability/{workspace,fs,subprocess,sandbox,approval}` 由 m71 登记，`plugins/local` 由创建它的 m73 登记；并扩展 `TestCoverageGateChecksEveryGatedPackage` 的有效与无效样例（每包 `-below-threshold` 与 `-missing`）。记录见 [测试与门禁](../../testing.md)。**90% 阈值与 `Makefile` 的 `check` 组成未改动。**

### 阶段最终门禁运行（g7-acceptance 自身 verify 内）

下表是**由收口节点 `g7-acceptance` 在它自己的 `verify` 内**实际执行、并写入导出证据的结果。它不是阶段内的预备运行，而是本阶段的最终门禁结论。

| 检查 id | 命令 | 退出码 |
| --- | --- | --- |
| `make-check` | `make check`（仓库根） | 0 |
| `diff` | `git diff --check` | 0 |
| `cli-e2e` | `go test ./examples/cli/...` | 0 |
| `cli-scenario` | `go test -run CLIScenario ./internal/testkit/...` | 0 |
| `capability-behavior` | `go test -race ./capability/...` | 0 |
| `local-behavior` | `go test -race ./plugins/local/...` | 0 |
| `policy-before-effect` | 断言工具层经 seam 调用、不直接触达世界 | 0 |
| `no-secret-leak` | 扫描树中是否存在凭据字面量 | 0 |
| `gate-integrity` | 断言 `check` 组成与覆盖率比较式未被削弱 | 0 |
| `coverage-registration` | 断言六个新包在 `coverageRequirement` 与门禁 fixture 中 | 0 |
| `module-index` | `docs/README.md` 与 `docs/roadmap.md` 同时链接新包 README | 0 |
| `stage-index` | `stages/index.md` 登记 M7 契约与 M8 交接 | 0 |
| `learning-index` | 三篇 execution 笔记、CLI 笔记与 C11 列入当前可学主题 | 0 |

**13 项检查全部通过，`make-check` 退出码 0。**

同一冻结 revision 上 `m71`–`m77` 与 `g7-acceptance` 逐个通过，`status` 返回 `complete: true`；随后 `render` exit 0、`export` exit 0 写入 `validation/` 下的一个 run 目录。**本页不写死 revision 与 run-id**（写入本页即会使其过时）；当前生效值以 `status.md` 与 `validation/` 下的实际目录为准。

### 尚未验证

- 本节点只完成契约落盘与结构自检；**没有运行任何 Go 测试或 `make check`**，M7 的实现状态仍为未开始。
- `capability/`、`plugins/local/`、`cmd/dsh-go/`、`examples/cli/` 尚未创建；契约中登记的 `capability-behavior`、`policy-before-effect`、`no-secret-leak`、`cli-e2e` 等检查均**未运行**。
- 真实模型端点与联网 smoke **未运行**，且按契约在缺密钥时记为未验证。
- Windows 平台行为**未验证**（当前环境为 macOS）。
- 本页为 [[node:g7-acceptance]] 的最终产物，届时冻结；本轮结论不代表 M7 已通过。
