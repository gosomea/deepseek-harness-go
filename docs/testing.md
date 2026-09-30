# 测试与门禁

## 交付入口

阶段交付运行 `make check`，任一子命令返回非零即失败。检查配置在 [Makefile](../Makefile)，CI 使用等价的 [Go 命令](../.github/workflows/check.yml)。修改门禁时必须有有效和无效样例，确认顶层检查会拒绝违规结果。

| 门禁 | 检查 | 失败条件 |
| --- | --- | --- |
| 格式 | `gofmt -l` | 任意源码未格式化 |
| 静态检查 | `go vet ./...` | vet 报告问题 |
| 行为与竞态 | `go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...` | 行为断言、竞态或超时 |
| 覆盖率 | `go run ./scripts/doccheck -coverage coverage.out` | Cordis 语句覆盖率低于 90%，或没有对应统计 |
| 示例快照 | `examples/cordis` 的测试 | 实际输出与 expected.txt 不一致 |
| 包文档 | `go run ./scripts/doccheck` | 包缺 README 或包注释；类型、元数据、必需章节或顺序不符；正文为空 |
| 文档程序 | `go run ./scripts/doccheck -examples` | 完整程序无法编译、非零退出、超时或 stdout 与预期不符 |
| 代码块与输出 | `go run ./scripts/doccheck` | 手写 Go 未分类、围栏未闭合、完整程序缺输出，或引用的输出文件过时 |
| 公开说明 | 同上 | 公开声明/结构体字段缺少以名称开头的注释 |
| 文档链接 | 同上 | 本地目标不存在、片段不存在或链接逃逸目录 |
| API 新鲜度 | 同上 | API 与代码及注释生成结果不一致 |
| 编译 | `go build ./...` | 库、示例或工具不能编译 |

macOS/Linux 使用 Make；Windows 或没有 Make 的环境，从仓库根目录按顺序执行以下 Go 命令。格式检查运行 `gofmt -l cordis examples scripts`，输出必须为空；其后任一命令非零都表示失败。

```sh
go vet ./...
go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...
go run ./scripts/doccheck -coverage coverage.out
go run ./scripts/doccheck
go run ./scripts/doccheck -examples
go build ./...
```

竞态检测需要 CGO 与 C 编译器。检查器的 Go 程序标记、输出和非空规则由[文档规范](documentation.md)维护；接口片段不会被当成独立程序运行。示例超时覆盖编译与执行，单个程序失败不会视为文档通过。

90% 是本项目 Cordis v0.1 的语句覆盖率下限，不等同于 DSH 的每文件 100% 覆盖规则，也不证明全部行为正确。重点时序必须有独立行为测试。新运行时包加入时，同步扩展覆盖率检查；不要沿用仅检查 Cordis 的选择器后声称覆盖了其他包。

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

测试源见 [生命周期测试](../cordis/lifecycle_test.go)、[运行时测试](../cordis/runtime_test.go)、[事件测试](../cordis/events_test.go) 和 [示例测试](../examples/cordis/main_test.go)。文档门禁拒绝样例见 [工具测试](../scripts/doccheck/main_test.go)。

## 执行证据

本机每阶段的必要检查经计划 CLI 执行并记录结果；从[Cordis 首轮方案](10-plans/cordis-v01/plans.md)和[Go 学习文档方案](10-plans/go-learning-docs/plans.md)查验相应版本的记录。报告包含工具命令、退出结果和产物指纹。更改已验证产物会使相应节点及后继的证据失效。

远端仓库为 [gosomea/deepseek-harness-go](https://github.com/gosomea/deepseek-harness-go)，每次推送的跨平台结果见 [GitHub Actions](https://github.com/gosomea/deepseek-harness-go/actions)。本地验收与 CI 结果分开记录。当前无真实模型接口或性能基准。代码评审还要核对文档承诺是否与实现一致，尤其是等待、取消、资源所有者和事件过滤；自动门禁只检查其可机械判断的部分。
