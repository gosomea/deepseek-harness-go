# M8 指令、技能、附件与上下文压缩

本页是 `plan.json` 的主题页，与它共同决定 revision。

## 现状

M7 已通过其执行契约：`capability` 的五个 seam（workspace/fs/subprocess/sandbox/approval）、`plugins/local` 的本机 Provider 与「先过策略再动手」的工具、`plugins/llmtrpc` 的凭据与端点、`cmd/dsh-go` 的会话子命令都已落地。M7 交出的是**受控的执行世界**：副作用先过策略与审批，进程可取消，输出有上限，退出会释放。

但「模型看到什么」还没有契约。目前会话日志记录的是消息与工具调用，而**指令、技能、文件引用、附件、工具输出的截断、压缩摘要**这些同样进入请求的东西没有自己的位置。于是出现三类问题：请求里的内容无法从日志重建（不知道模型为什么看到这些）；工具输出变大后只能整段塞进历史；历史变长时没有可解释的压缩行为。

M8 解决的就是这一层。

## 目标

使**所有进入模型的上下文可由日志重建**，并在工具输出、附件与历史增大时有**可解释的预算与压缩行为**。

三个必须成立的性质：

1. **模型可见即已记录**：任何进入请求的内容先作为日志事实存在，再由请求渲染。同一份日志 + 同一版本代码必须重建出同一份请求；渲染不得引入日志里没有的内容。
2. **压缩不拆坏工具对**：一次工具调用与它的结果同进同出。压缩边界落在两者之间会留下悬空关联。
3. **摘要失败不伪造成功**：摘要调用失败、写日志失败或中途取消时，不得报告「替换已完成」。

## 进入条件

必需前驱：[M7 的 G7](../dsh-go-replication-m07/plans.md)，已通过。

## 执行契约

见 [plan.json](plan.json)。八个节点，严格串行（M8.4 依赖 M8.1；M8.5 依赖 M8.3 与 M8.4）：

| 节点 | 切片 | 交付 |
| --- | --- | --- |
| `m81-contributions` | M8.1 | 指令、时间、文件／Session 引用作为 owned 贡献；区分日志事实、渲染与请求瞬时访问路径 |
| `m82-settings-preset` | M8.2 | settings、preset/persona 与 Agent 独立能力配置；默认值／会话覆盖／作用域不串用 |
| `m83-skills` | M8.3 | 技能发现、按需读取／启用与依赖声明；卸载清理贡献 |
| `m84-attachments` | M8.4 | 附件身份、不可变存储与请求访问映射；损坏／换世界／不支持模态都有诊断 |
| `m85-token-spill` | M8.5 | token 计量、输出保留策略、spill 与可追溯引用 |
| `m86-compaction` | M8.6 | 压缩 seam、fake 摘要 Provider、start/summary/end 与表面替换 |
| `m87-summary-route` | M8.7 | 真实摘要路由；不同模型能力／预算的集成 fixture |
| `g8-acceptance` | G8 | 复刻、学习与证据同时验收；`make check` 在它自身 verify 内从仓库根执行 |

### 为什么是串行

M8.2 改的是配置的作用域，M8.3 的技能依赖声明建立在它之上；M8.4 的附件映射与 M8.3 的技能加载都要经 M8.1 的贡献机制进入请求，M8.5 的预算要同时看技能与附件的体积，M8.6 的压缩边界要处理 M8.5 溢出后的引用。把其中任何一段当作可并行，都会在「同一份日志重建同一份请求」这条不变量上翻车。

### 阶段内要冻结的选择

- **预算与 token 估算采用可确定性测试的实现**，并记录误差边界；区分「usage 实测」与「估算」。不把估算写成实测。
- **压缩后的表面替换按固定源码核对**：不会把「只写审计事件」自动等同于「模型可见摘要」。
- **多模态只在三方（适配器、重放、消费者）共同支持后才开放**；平台支持矩阵如实记录。

## 产物所有权与不变量

- 任何节点的 inputs 不得被链上更晚的节点写入；不使用目录级 writes。
- 共享门禁文件 `scripts/doccheck/main.go`、`scripts/doccheck/main_test.go`、`docs/testing.md` 由创建新运行时包的同一切片登记，**因此不出现在任何节点的 inputs**；最终状态由 `g8-acceptance` 冻结并登记为它的 outputs。
- 注册断言的集合**按当时是否已存在分层**：每个切片的 `coverage-registration` 只断言到它自己创建的包（避免断言未来文件），`g8` 断言含全部新运行时包的完整集合。
- `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md` **只在 g8 写一次**。阶段中途把 M8 标成「当前可学」与事实不符；多次写入只会让已通过节点连续 stale。
- `Makefile` 与 `.github/workflows/check.yml` 是 M1/M2/M3 已验收的产物，本阶段**不得改写**，只由 `gate-integrity` 只读断言。

### 对上游产物的已知影响

`docs/10-plans/dsh-go-replication/stages/index.md` 列在 M0 契约的 documents 中。`g8` 更新它会使 M0 相关节点变为 `plan changed; revise required`——这是按阶段交接要求执行 g8 的已知代价。

## 学习交付

| 笔记 | 主题 | 读者实验 |
| --- | --- | --- |
| [contributions.md](../../context/contributions.md)（M8.1，已交付） | 日志事实／渲染／请求访问路径三种东西 | 同一份日志重建请求，比较两次渲染 |
| [settings-scope.md](../../context/settings-scope.md)（M8.2，已交付） | 默认值／作用域覆盖／会话覆盖三层优先级 | 预测三层覆盖结果，并用 `Source` 解释 |
| [skills.md](../../context/skills.md)（M8.3，已交付） | 技能清单与实际加载的 token 差异 | 注入技能，观察未启用内容不进请求 |
| [attachments.md](../../context/attachments.md)（M8.4，已交付） | 持久附件与本次请求可访问路径 | 改文件后观察诊断而非过期引用 |
| [budgets.md](../../context/budgets.md)（M8.5，已交付） | 长输出如何缩短但仍可定位原文 | 注入超长输出，观察预算与 spill |
| [compaction.md](../../context/compaction.md)（M8.6，已交付） | 先预测压缩后 history，再从日志重建 | 运行 fake 摘要压缩并重放比较实际请求 |

可运行实验：[examples/context](../../../examples/context/README.md)（已可运行，含压缩三阶段与摘要失败演示）。

**六篇笔记均已交付并改为活链接**：`skills.md`、`attachments.md`、`budgets.md`、`compaction.md` 随各自切片落地后，已把反引号路径改回仓库相对链接，避免 M8 final 节点的文档与链接检查在阶段末期才暴露冲突。

`settings-scope.md` 未列入 M8 原始五篇清单，是 M8.2 切片按 `c-learning`「各阶段至少两篇」的要求额外交付的第二篇；它同样七节齐备并链到本阶段实验。

## 本阶段实际运行的命令与结果

**契约阶段已完成；M8.1–M8.7 与 g8 收口已交付**：契约落盘后按计划评审要求做了一次受控修改（新增 `doccheck-integrity`）。实现方面，`plugins/context`、`plugins/settings`、`plugins/preset`、`plugins/skills`、`capability/attachment`、`plugins/attachment`、`capability/spill`、`plugins/spill`、`capability/compaction`、`plugins/compaction`（含真实摘要路由 `route.go`）与四个上下文场景 fixture、可运行实验均已落地。下表按真实输出记录，并在 g8 之前**一次定稿**。

| 阶段 | 命令 | 退出码 | 结果 |
| --- | --- | --- | --- |
| 契约（改动前） | `plan.py validate docs/10-plans/dsh-go-replication-m08/plan.json` | 0 | 8 节点，结构合法；改动前 revision `f0e90072abc794e8716a8108daa7b8f97fed266dfff7649c6742a869bbc0112d` |
| 契约（新增 check 后） | `plan.py validate docs/10-plans/dsh-go-replication-m08/plan.json` | 0 | 8 节点，结构合法；改动后 revision `9bc58e72205d47c837ccd4168847d8c48594a8c80ee1aa067f7f491c6f855479` |
| 契约 | 按 `g8-acceptance` 的 argv 实跑新 check `doccheck-integrity` | 0 | `doccheck integrity: OK (five branches present, weakened samples rejected)` |
| 基线（改动前） | `make check` | 2 | 唯一失败者 `docs`：7 条 broken link；`fmt-check`/`vet`/`test`/`coverage`/`doc-examples`/`build` 均为 0 |
| 现状（s0 收口后） | `make check` | 0 | 全部子命令通过；`broken link` 计数 0 |
| M8.1 | `go build ./plugins/context/` | 0 | 包编译通过 |
| M8.1 | `go test -race -count=1 -timeout=240s ./plugins/context/...` | 0 | 通过；语句覆盖率 94.3%（补测前 87.6%，低于 90% 门禁，加分支用例后达标） |
| M8.1 | `go run ./examples/context` | 0 | 输出与 `examples/context/expected.txt` 逐字一致 |
| M8.1 | `go test -count=1 ./examples/context/...` | 0 | `TestDemoMatchesDocumentedOutput` 通过 |
| M8.1 | `go run ./scripts/doccheck -examples` | 0 | 笔记里的 `go runnable=context-rebuild` 程序实跑且输出匹配 |
| M8.1 | `go test -count=1 ./scripts/doccheck/...` | 0 | 新增 `context` 后覆盖率门禁及其 fixture 三件套通过 |
| M8.1 | `make check`（从仓库根） | 0 | 全部子命令通过；`context coverage: 94.3%` 已进门禁输出 |
| M8.2 | `go test -race -count=1 -timeout=240s ./plugins/settings/...` | 0 | 通过；语句覆盖率 92.2% |
| M8.2 | `go test -race -count=1 -timeout=240s ./plugins/preset/...` | 0 | 通过；语句覆盖率 100.0% |
| M8.2 | `go test -race -count=1 -run 'Scope\|Default\|Override' ./plugins/settings/...` | 0 | scope-behavior 场景通过 |
| M8.2 | `go run ./scripts/doccheck -coverage <profile>` | 0 | `settings coverage: 92.2%`、`preset coverage: 100.0%` 已进门禁输出 |
| M8.2 | `go run ./scripts/doccheck` 与 `-examples` | 0 | 文档门禁通过；新增笔记的 `go runnable=settings-scope` 程序实跑且输出匹配 |
| M8.3 | `go test -race -count=1 -timeout=240s ./plugins/skills/...` | 0 | 通过；语句覆盖率 97.2% |
| M8.4 | `go test -race -count=1 -timeout=240s ./capability/attachment/... ./plugins/attachment/...` | 0 | 通过；覆盖率 96.4% / 94.6% |
| M8.5 | `go test -race -count=1 -timeout=240s ./capability/spill/... ./plugins/spill/...` | 0 | 通过；覆盖率 100.0% / 96.0% |
| M8.6 | `go test -race -count=1 -timeout=240s ./capability/compaction/... ./plugins/compaction/...` | 0 | 通过；覆盖率 95.0% / 91.5% |
| M8.7 | `go test -race -count=1 -run 'Route\|Budget\|Cancel' ./plugins/compaction/...` | 0 | route-behavior 场景通过（route.go 真实摘要路由） |
| M8.7 | `go test -race -count=1 -timeout=240s -run ContextScenario ./internal/testkit/...` | 0 | 四个上下文场景（spill／compaction／summary-failure／route-switch）回放两次逐行一致 |
| M8.7 | `go test -race -count=1 -timeout=240s ./examples/context/...` | 0 | `TestDemoMatchesDocumentedOutput` 通过；输出含压缩三阶段与失败不报 end |
| g8 | `make check`（从仓库根） | 0 | fmt／vet／test／coverage／docs／doc-examples／build 全部通过；compactionplugin 91.5%、testkit 90.2% |

**这些都是本机结果。** 本地通过不代表 CI 已通过。

### M8.1 的真实失败与处置

1. **覆盖率不达标**：首轮 `plugins/context` 语句覆盖率 87.6%，低于 90% 门禁。处置是补充分支用例（覆盖 `Validate` 各拒绝路径、`Add` 的非法输入、`Record` 的校验先于追加、`Rebuild` 的 ID 一致性），而非降低阈值；补测后 94.3%。
2. **笔记程序字符串字面量被破坏**：写入时 `\n\n` 被展开成真实换行，`go vet`/编译失败（`string literal not terminated`）。处置是改写为不跨行的字面量拼接。
3. **相对链接深度写错两次**：`docs/context/contributions.md` 到仓库根是 `../../` 而非 `../../../`；`plugins/context/README.md` 到 `docs/` 用 `../docs/` 而非 `../../docs/`。均由 `doccheck` 的 broken-link 与 escapes-repo 分支拒绝后发现并改正——这正是该门禁有判别力的实例。
4. **示例包违反文档规范**：`examples/context/main.go` 的导出方法缺以名称开头的注释、`README.md` 的「验证」节只有代码块无说明正文，均被 `doccheck` 拒绝后补写。
5. **一次与本次改动无关的既有 flake**：`cmd/dsh-go` 的 `TestSessionSignalStopsTheRun` 在整模块高负载下曾失败一次（`want 130, got 0`），单独运行与后续 `make check` 均通过；该包属 M7 交付且本阶段未改动，如实记录为观察到的既有不稳定，不计入本节点缺陷。

### 本次对契约的受控修改

按计划评审要求，`g8-acceptance` 新增一条独立 check `doccheck-integrity`（插入在 `gate-integrity` 之后），使「`scripts/doccheck` 旧门禁语义不可削弱」从描述承诺变为契约实体。该 check 对 `scripts/doccheck` 做五分支正向断言（覆盖率解析与 90% 阈值、`has no coverage data` 提示、broken link 扫描、`func checkFormatting`、`func checkExamples`），并以三份被削弱样例（阈值 90 改 50、删 broken-link 扫描、删 `has no coverage data` 提示）证明该检查确实会拒绝。除该 check 外未改动 M8 契约的其余节点、依赖与检查。

修改前后 revision 分别为上表两行所示值；旧 revision 上的历史验证记录仍然证明旧提交，不因本次修改而失效。

### 第二次受控修改：修正三处被合并的包模式 argv

g8 收口阶段实跑发现三处 check 的 argv 把两个包模式合并进了**同一个字符串元素**（`./capability/attachment/... ./plugins/attachment/...` 等），`go test` 因此报 “matched no packages” 并**一个包都不测**——这是一条会静默通过的坏门禁，正好违反了本契约约束「每个包模式必须是独立的 argv 元素」。处置是把这三处（`attachment-behavior`、`spill-behavior`、`compaction-behavior`）拆成两个独立 argv 元素，使其**真正运行两个包**。这是**加强**门禁（从零覆盖变为实际覆盖），不是放宽；`expected_exit` 仍为 0。修改后在新 revision 上重跑全部节点。

### 尚未验证

阶段级未验证项（如实记录，不记为通过）：

- 真实摘要路由的联网 smoke：`plugins/compaction` 的 `Route` 已接入 `llm.Provider`，但真实端点需凭据；本阶段全部使用 fake Provider，缺凭据时记为未验证。
- 参考构建产物（DSH_PACKAGES 指向的固定参考 packages 或 lib 构建产物）缺失时，跨实现差分记为未验证；本阶段上下文场景是「同一实现两次运行」的确定性对照，非跨实现（见 [DIVERGENCES.md](../../../testdata/parity/context/DIVERGENCES.md) D1）。
- 多模态支持矩阵中未实测的模态记为「仅代码」或「未实现」；本阶段未上传真实多模态附件。

### 关于执行顺序的如实说明

M6 与 M7 都因「切片实现先于逐节点 `claim → start → verify`」被评审要求补记。本阶段的实践中：M8.1–M8.6 的实现先于本次会话存在；本次会话补齐 m87-summary-route 缺失产物后，重新验证了因改动而失效的 m86，再验证 m87，最后经 `revise` 在新 revision 上重跑全部节点与 g8。此处如实记录实际顺序，不写应收顺序。

## 阶段内的真实失败与处置

- **m87 产物缺失**：进入本阶段时 `plugins/compaction/route.go`、`internal/testkit/context_scenario.go`、`testdata/parity/context/*`、`examples/context` 更新均不存在；补齐后 m87 的 7 项检查通过。
- **import cycle**：`plugins/compaction` 的内部测试包引用 `internal/testkit` 造成循环依赖，`go test` FAIL。处置是把 `route_test.go` 改为外部包 `compaction_test` 并把插件包别名为 `plugin`，再通过。
- **gofmt 不合规**：`route_test.go` 首版非 gofmt-clean，`make check` 的 `fmt-check` 以 EXIT=2 拒绝。处置 `gofmt -w`，未改门禁。
- **覆盖率跌破 90%**：新增 `context_scenario.go` 后 `testkit` 语句覆盖率降到 89.1%，低于门禁。处置是补写 `Validate` 各拒绝分支、`ContextScenarioDir`、`LoadContextScenario` 缺文件与预算过小等真实测试，回升到 90.2%，**未降低阈值**。
- **fixture 预算过小**：`long-output-spill` 首版 budget 8 装不下引用本身，`Fit` 拒绝；调整到 20（小于内容却可容纳引用）后 `shortened=true` 成立。
