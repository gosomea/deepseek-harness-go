# M9 子代理、目标与持久任务

本页是 `plan.json` 的主题页，与它共同决定 revision。

## 现状

M8 已通过其执行契约：上下文层让所有进入模型的内容先记录再渲染，并有可解释的预算与压缩。M9 在其上管理「**谁来做、做到哪、重启后还差什么**」。

## 目标

让父 Agent 委派、等待、继续和取消子 Agent，并使目标、队列与定时任务在恢复后保持可解释的状态。三个必须成立的性质：

1. **fork 是继承事实**：fork 记录继承到父事件序列某处，不复制父会话成子事实，两份日志各自可解释。
2. **能力先声明**：能力不符在 `Spawn` 前拒绝，不跑到深处才失败；深度有界。
3. **持久任务不承诺 exactly-once**：结算丢失的 job 记为 `unknown` 由人工裁决，不自动重放。

workflow 先交付 seam 与 fake 引擎；原 DSH workflow 的生产 PTC Provider 由 M12.4 验收，本阶段不提前宣称完整脚本执行兼容。

## 进入条件

必需前驱：[M8 的 G8](../dsh-go-replication-m08/plans.md)，已通过。

## 执行契约

见 [plan.json](plan.json)。八个节点，依赖顺序：

| 节点 | 切片 | 交付 |
| --- | --- | --- |
| `m91-subagent-seam` | M9.1 | subagent seam、命名注册表、能力声明与深度上限 |
| `m92-inprocess-provider` | M9.2 | 进程内 Provider：spawn／fork／谱系／继承切点 |
| `m93-control-wait` | M9.3 | 控制与等待（释放、interrupt、深度预算由执行者持有） |
| `m94-taskstate` | M9.4 | goal／plan／todo 事件与纯投影、续跑规则 |
| `m95-jobs-schedule` | M9.5 | 持久 jobs 状态机与可控时钟调度 |
| `m96-workflow-fake` | M9.6 | workflow seam 与 fake 引擎、输入校验 |
| `m97-examples` | M9.7 | examples/subagents、examples/jobs 与固定输出 |
| `g9-acceptance` | G9 | 复刻、学习与证据同时验收；`make check` 从仓库根执行 |

## 产物所有权与不变量

- 任何节点的 inputs 不得被链上更晚的节点写入；不使用目录级 writes（包目录以具体文件登记）。
- 共享门禁文件 `scripts/doccheck/main.go`、`scripts/doccheck/main_test.go`、`docs/testing.md` 由 `m91` 登记（新运行时包），最终由 `g9` 冻结。
- 新运行时包登记进 `coverageRequirement` 与其 fixture 测试：`subagent`、`subagentplugin`、`taskstate`、`jobs`、`schedule`、`workflow`、`workflowplugin`。
- `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md` 只在 `g9` 写一次。

## 学习交付

| 笔记 | 主题 | 读者实验 |
| --- | --- | --- |
| [subagents.md](../../tasks/subagents.md) | 生命周期树、能力声明与谱系 | 运行 `examples/subagents` 比较 spawn 与 fork 的 inherit |
| [goals.md](../../tasks/goals.md) | 纯投影与唯一续跑规则 | 追加无 goal 的 goal-done，观察被拒 |
| [jobs.md](../../tasks/jobs.md) | goroutine 与持久任务的差别 | 运行 `examples/jobs` 观察 unknown 不重放 |
| [workflow.md](../../tasks/workflow.md) | 编排与执行分离 | 给引擎未声明能力的脚本，观察执行前拒绝 |

可运行实验：[examples/subagents](../../../examples/subagents/README.md)、[examples/jobs](../../../examples/jobs/README.md)。

## 本阶段实际运行的命令与结果

| 阶段 | 命令 | 退出码 | 结果 |
| --- | --- | --- | --- |
| 契约 | `plan.py validate docs/10-plans/dsh-go-replication-m09/plan.json` | 0 | 8 节点，结构合法 |
| M9.1 | `go test -race -count=1 ./capability/subagent/...` | 0 | 覆盖率 90.3% |
| M9.2 | `go test -race -count=1 ./plugins/subagent/...` | 0 | 覆盖率 96.4% |
| M9.4 | `go test -race -count=1 ./capability/taskstate/...` | 0 | 覆盖率 94.7% |
| M9.5 | `go test -race -count=1 ./capability/jobs/...` | 0 | 覆盖率 97.1% |
| M9.5 | `go test -race -count=1 ./plugins/schedule/...` | 0 | 覆盖率 94.7% |
| M9.6 | `go test -race -count=1 ./capability/workflow/... ./plugins/workflow/...` | 0 | 覆盖率 92.3% / 92.9% |
| M9.7 | `go run ./examples/subagents` 与 `go run ./examples/jobs` | 0 | 输出与 expected.txt 逐字一致 |
| g9 | `make check`（从仓库根） | 0 | fmt/vet/test/coverage/docs/doc-examples/build 全过 |

**这些都是本机结果。** 本地通过不代表 CI 已通过。

### 尚未验证

- workflow 生产 PTC Provider 与外部 subagent 产品桥接：属 M12.4／M10，本阶段记为未实现。
- 真实外部任务系统的持久化 Provider：属 M12，本阶段为内存实现。
- 多平台与网络相关行为：本阶段全部本地、无网络。

## 阶段内的真实失败与处置

1. **import/包名冲突**：`plugins/subagent`、`plugins/workflow` 的测试同时引用 seam 与实现包（同名），改为外部测试包并别名（`seam`／`plugin`／`engine`）后通过。
2. **覆盖率跌破 90%**：初版 jobs 84.3%、taskstate 89% 以下（含 Append 空事件不变量），补真实测试（重复 id、错误状态、markunknown、默认状态等）回升到 97.1%／94.7%，**未降阈值**。
3. **coverage-gate fixture 失败**：新增 7 包注册后，`TestCoverageGateChecksEveryGatedPackage` 的 `all-gated-packages-pass` 样例缺新包使门禁判负；补齐 7 对 Pass/Fail 常量并加入全通过样例后转绿。
4. **gofmt / doccheck**：`route` 类似的多轮 gofmt 与 README "使用"节缺解释、示例导出注释缺失、输出块与文件不符等，均由 doccheck 拒绝后逐条修正。
