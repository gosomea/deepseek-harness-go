# 测试与门禁

## 交付入口

阶段交付运行 `make check`，任一子命令返回非零即失败。检查配置在 [Makefile](../Makefile)，CI 使用等价的 [Go 命令](../.github/workflows/check.yml)。修改门禁时必须有有效和无效样例，确认顶层检查会拒绝违规结果。

| 门禁 | 检查 | 失败条件 |
| --- | --- | --- |
| 格式 | `go run ./scripts/doccheck -fmt` | 仓库内任意 Go 源码未格式化，或源码无法解析 |
| 静态检查 | `go vet ./...` | vet 报告问题 |
| 行为与竞态 | `go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...` | 行为断言、竞态或超时 |
| 覆盖率 | `go run ./scripts/doccheck -coverage coverage.out` | 任一受门禁的运行时包语句覆盖率低于 90%，或该包没有对应统计 |
| 示例快照 | `examples/cordis` 的测试 | 实际输出与 expected.txt 不一致 |
| 包文档 | `go run ./scripts/doccheck` | 包缺 README 或包注释；类型、元数据、必需章节或顺序不符；正文为空 |
| 文档程序 | `go run ./scripts/doccheck -examples` | 完整程序无法编译、非零退出、超时或 stdout 与预期不符 |
| 代码块与输出 | `go run ./scripts/doccheck` | 手写 Go 未分类、围栏未闭合、完整程序缺输出，或引用的输出文件过时 |
| 公开说明 | 同上 | 公开声明/结构体字段缺少以名称开头的注释 |
| 文档链接 | 同上 | 本地目标不存在、片段不存在或链接逃逸目录 |
| API 新鲜度 | 同上 | API 与代码及注释生成结果不一致 |
| 编译 | `go build ./...` | 库、示例或工具不能编译 |

macOS/Linux 使用 Make；Windows 或没有 Make 的环境，从仓库根目录按顺序执行以下 Go 命令。格式检查运行 `go run ./scripts/doccheck -fmt`，它遍历仓库内的 Go 源码而不是固定目录清单，因此新包自动被覆盖；其后任一命令非零都表示失败。

```sh
go vet ./...
go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...
go run ./scripts/doccheck -coverage coverage.out
go run ./scripts/doccheck
go run ./scripts/doccheck -examples
go build ./...
```

竞态检测需要 CGO 与 C 编译器。检查器的 Go 程序标记、输出和非空规则由[文档规范](documentation.md)维护；接口片段不会被当成独立程序运行。示例超时覆盖编译与执行，单个程序失败不会视为文档通过。

## 双实现差分

Session surface 与 Cordis 运行时各自有一族共享场景，见 [Session 差分场景](../testdata/parity/session/README.md) 与 [Cordis 差分场景](../testdata/parity/cordis/README.md)。两族共用同一个比较器，因此差异规则只有一套。

Session 的实时对照检查需要 `DSH_PACKAGES` 指向固定参考的 `packages` 目录：

```sh
DSH_PACKAGES=<DSH>/packages go test -count=1 -run TestLiveReferenceSessionSurfaceMatchesRecorded ./internal/testkit
```

缺少该变量、缺少 Node 或缺少固定构建产物时，检查**跳过**并把结果记为未验证，不记为通过；已记录的参考 trace 仍然参与比较。

### 覆盖范围不对称

Session 场景只对照 **surface 与派生历史**。envelope 的字段级拒绝（`ignorable` 的形状、未知 envelope 字段、必填字段）由 Go 单侧测试固定，因为固定参考的 `foldSurface` 不做 payload 校验——那些规则在它的**入日志边界**（`Session.append` 与 seed 采纳路径）上。这是已记录的差异，不是遗漏。

90% 是本项目运行时包的语句覆盖率下限，不等同于 DSH 的每文件 100% 覆盖规则，也不证明全部行为正确。重点时序必须有独立行为测试。受门禁的包在 [门禁实现](../scripts/doccheck/main.go) 的 `coverageRequirement` 中逐个列出：新运行时包必须显式加入，清单之外或被遗漏的包会因“没有覆盖数据”而失败，不会因为未被选择而悄悄通过。当前受门禁的包是 `cordis`、`internal/testkit`、`loader`、`app`、`llm`、`scope`、`session`、`tools`、`systemprompt`、`plugins/llmtrpc`、`agent`、`agentloop`、`persistence`、`plugins/sessionjsonl`、`capability/workspace`、`capability/fs`、`capability/subprocess`、`capability/sandbox`、`capability/approval` 与 `plugins/local`；每个包都在创建它的同一切片内登记，见 [M2 执行契约](10-plans/dsh-go-replication-m02/plans.md)、[M3 执行契约](10-plans/dsh-go-replication-m03/plans.md)、[M4 执行契约](10-plans/dsh-go-replication-m04/plans.md)、[M5 执行契约](10-plans/dsh-go-replication-m05/plans.md)、[M6 执行契约](10-plans/dsh-go-replication-m06/plans.md)和 [M7 执行契约](10-plans/dsh-go-replication-m07/plans.md)。


### 门禁变更记录：M4 登记三个新运行时包

### 门禁变更记录：M5 登记 `agent` 与 `agentloop`

### 门禁变更记录：M6 登记 `persistence` 与 `plugins/sessionjsonl`

### 门禁变更记录：M7 登记五个 `capability/*` 包与 `plugins/local`

M7 把六个新运行时包加入受门禁清单：`capability/workspace`、`capability/fs`、`capability/subprocess`、`capability/sandbox`、`capability/approval` 由 m71 登记（m71 创建它们），`plugins/local` 由创建它的 m73 登记。

- **有效样例**：`all-gated-packages-pass` 包含这六个包的达标 profile。
- **无效样例**：每个新包都有对应的 `<pkg>-below-threshold` 与 `<pkg>-missing` 用例。

覆盖率阈值 90% 未改动。首次登记时 `plugins/local` 实测 83.2%，随后通过补真实行为测试（策略拒绝不写文件、只读策略拒绝 shell、进程树取消、输出限额、设备特殊文件与权限路径）达到 90.5%，**未调整阈值**。该变更由 [M7 执行契约](10-plans/dsh-go-replication-m07/plans.md) 的 `coverage-registration` 检查断言，并在 `validation/<run-id>` 中留证。


M6 把 `persistence` 与 `plugins/sessionjsonl` 加入受门禁清单，并同样扩展了 `TestCoverageGateChecksEveryGatedPackage`：

- **有效样例**：`all-gated-packages-pass` 现在包含两个新包的达标 profile。
- **无效样例**：为每个新包增加 `<pkg>-below-threshold` 与 `<pkg>-missing` 两类用例。

覆盖率阈值 90% 未改动，`Makefile` 的其他子命令未改动。首次登记时 `sessionjsonl` 实测 84.1%，随后通过补真实 I/O 故障路径测试（只读目录、描述符被关闭、路径被目录占用、未来版本、撕裂尾部）提升到 90.5%，未调整阈值。该变更由 [M6 执行契约](10-plans/dsh-go-replication-m06/plans.md) 的 `coverage-registration` 检查断言，并在 `validation/<run-id>` 中留证。


M5 把 `agent` 与 `agentloop` 加入受门禁清单，并同样扩展了 `TestCoverageGateChecksEveryGatedPackage`：

- **有效样例**：`all-gated-packages-pass` 现在包含两个新包的达标 profile。
- **无效样例**：为每个新包增加 `<pkg>-below-threshold` 与 `<pkg>-missing` 两类用例。

覆盖率阈值 90% 未改动，`Makefile` 的其他子命令未改动。该变更由 [M5 执行契约](10-plans/dsh-go-replication-m05/plans.md) 的 `coverage-registration` 检查断言，并在 `validation/<run-id>` 中留证。


M4 把 `tools`、`systemprompt` 与 `plugins/llmtrpc` 加入受门禁清单（`scripts/doccheck/main.go` 的 `coverageRequirement`），因此同步扩展了它自己的测试 fixture `TestCoverageGateChecksEveryGatedPackage`，而不是让新包只被比较式覆盖：

- **有效样例**：`all-gated-packages-pass` 现在同时包含三个新包的达标 profile，断言门禁接受全部受控包。
- **无效样例**：为每个新包增加 `<pkg>-below-threshold`（覆盖率不足）与 `<pkg>-missing`（profile 里完全没有该包）两类用例，断言门禁分别拒绝。

覆盖率阈值 90% 未改动，Makefile 的其他子命令未改动。这次变更由 [M4 执行契约](10-plans/dsh-go-replication-m04/plans.md) 的 `coverage-registration` 检查断言，并在 `validation/<run-id>` 中留证。

## 行为与证据

| 行为 | 测试 |
| --- | --- |
| 依赖缺失、恢复、再次提供；清理读取旧绑定 | `TestDependencyLossRestartsAndKeepsCleanupAccess` |
| 多层消费者先于 Provider 清理 | `TestTransitiveConsumersUnloadBeforeProvider` |
| 同标签隔离、共享其他服务、重复/类型/nil 错误 | `TestScopesJoinWithoutLeakingOtherServices` |
| 失败回滚、有效配置恢复、拒绝无效更新、旧视图失效 | `TestFailureRollsBackAndUpdateValidatesBeforeRestart` |
| 子实例释放、清理 panic 后继续、错误汇总 | `TestChildrenDisposedAndCleanupContinuesAfterPanic` |
| 手动清理被所有者等待、清理期间可登记变更 | `TestManualDisposerJoinedAndReentrantMutation` |
| 并发注册、关闭取消启动、关闭后拒绝注册 | `TestConcurrentPluginRegistrationAndCloseCancelsStartup` |
| 启动时自卸载仍清理返回资源 | `TestSelfDisposalDuringStartupOwnsReturnedCleanup` |
| Provider 重启重新激活消费者、状态等待与日志配置 | `TestProviderRestartReactivatesConsumers` |
| 加载中的配置更新忽略过时启动结果 | `TestUpdateDuringLoadingUsesLatestConfig` |
| Close 超时返回后可继续等待实际清理完成 | `TestCloseDeadlineBoundsWaitForUncooperativeCleanup` |
| 非法声明、失败状态与失效视图的错误 | `TestInvalidPluginAndCanceledContextRejectWork` |
| 监听顺序、0/空串 bail、所属插件卸载移除监听器 | `TestEventOrderBailValuesAndOwnedRemoval` |
| Waterfall 包装默认行为与否决 | `TestWaterfallDelegationAndVeto` |
| Next 多次调用向后推进，跳过已删除的 Waterfall 监听器 | `TestWaterfallRepeatedNextAdvancesAndSkipsRemovedListener` |
| 并行开始与错误汇总、并发 Once 恰好一次 | `TestParallelJoinsAllErrorsAndOnceIsAtomic` |
| 显式作用域过滤、Global 绕过过滤 | `TestFilterIsExplicitAndGlobalBypassesIt` |
| 跳过已移除监听器、事件 panic 可观察 | `TestDispatchSkipsRemovedListenersAndRecoversPanics` |
| 文档入口可运行、输出未偏离示例说明 | `TestDemoMatchesDocumentedOutput` |
| 条件可用性谓词控制消费者等待、卸载与恢复，谓词 panic 记为不可用 | `TestConditionalAvailabilityGatesConsumers` |
| 无条件服务不受 Refresh 影响 | `TestUnconditionalProvideStaysAvailable` |
| Go trace 与固定 TypeScript 参考的录制 trace 逐行一致 | `TestReplayMatchesRecordedReferenceTrace` |
| 录制 trace 与实时参考一致（需要固定构建产物，缺失时跳过） | `TestLiveReferenceTraceMatchesRecorded` |
| 比较器拒绝未声明差异、未发生声明与无理由声明 | `TestCompareTracesRejectsUndeclaredDifferences`、`TestCompareTracesAppliesOnlyUsedDivergences` |
| 参考 trace 的录制来源与哈希可核对 | `TestReferenceTracesMatchProvenance` |
| Session surface 与固定参考逐行一致（六个共享场景） | `TestSessionParityMatchesRecordedReferenceTraces` |
| 录制 trace 与实时参考一致（需要固定构建产物，缺失时跳过并记为未验证） | `TestLiveReferenceSessionSurfaceMatchesRecorded` |
| Session 场景 fixture 格式正确、参考 trace 齐备 | `TestSessionScenariosAreWellFormed`、`TestSessionScenarioDecoderRejectsIncompleteFixtures` |
| 追加提交后序号仍密集且唯一（并发写入与读取） | `TestConcurrentAppendsAndReads` |
| 被拒绝的 append 不推进序号、不通知观察者 | `TestRejectedAppendLeavesTheLogUnchanged` |
| 未知事件类型默认拒绝、标记 `ignorable` 后保留原文 | `TestUnknownEventPolicy` |
| 审计事件让日志变长而模型历史不变 | `TestAuditEventsLengthenTheLogWithoutChangingHistory` |
| 空内容消息占位但不产生模型消息 | `TestEmptyMessagesKeepTheirPlaceButDeriveToNothing` |
| 离线 fold 与实时 surface 得到同一份结果 | `TestLiveLogComputesTheSameSurfaceAsAFold` |
| 缺少必需消息投影即拒绝整份日志 | `TestRequiredProjectionIsRefusedWithoutAnInterpreter` |
| 未知内容块原样保留、已知块逐字节往返 | `TestUnknownBlockSurvivesRoundTrip`、`TestContentRoundTripIsLossless` |
| 兄弟作用域互不可见、事件只向上准入 | `TestSiblingScopesDoNotLeak`、`TestEventAdmissionFlowsUpTheChain` |
| 覆盖率门禁对每个受门禁包分别拒绝缺失与低于阈值 | `TestCoverageGateChecksEveryGatedPackage` |
| 覆盖率门禁的有效/无效样例覆盖每个受门禁包 | `TestCoverageGateChecksEveryGatedPackage` 的 `all-gated-packages-pass` 与各 `<pkg>-missing`／`<pkg>-below-threshold` 子用例 |

测试源见 [生命周期测试](../cordis/lifecycle_test.go)、[运行时测试](../cordis/runtime_test.go)、[事件测试](../cordis/events_test.go)、[Session 日志测试](../session/log_test.go)、[surface 测试](../session/surface_test.go)、[投影测试](../session/projection_test.go)、[消息测试](../llm/message_test.go)、[作用域测试](../scope/scope_test.go) 和 [示例测试](../examples/cordis/main_test.go)。文档门禁拒绝样例见 [工具测试](../scripts/doccheck/main_test.go)。

## 执行证据

本机每阶段的必要检查经计划 CLI 执行并记录结果；从[Cordis 首轮方案](10-plans/cordis-v01/plans.md)和[Go 学习文档方案](10-plans/go-learning-docs/plans.md)查验相应版本的记录。报告包含工具命令、退出结果和产物指纹。更改已验证产物会使相应节点及后继的证据失效。

远端仓库为 [gosomea/deepseek-harness-go](https://github.com/gosomea/deepseek-harness-go)，每次推送的跨平台结果见 [GitHub Actions](https://github.com/gosomea/deepseek-harness-go/actions)。本地验收与 CI 结果分开记录。当前无真实模型接口或性能基准。代码评审还要核对文档承诺是否与实现一致，尤其是等待、取消、资源所有者和事件过滤；自动门禁只检查其可机械判断的部分。
