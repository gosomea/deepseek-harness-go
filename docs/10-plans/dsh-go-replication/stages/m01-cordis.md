# M1 Cordis 下游基础与行为对照

## 阶段结果

证明通用运行时足以支撑 Loader 和核心能力插件，并把必要缺口修复成可运行的学习实验。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M0](m00-baseline.md) 的 G0。

以 M0 判定的下游阻塞为实施范围；复用已有 Cordis，逐行为核对和补齐。完整动态 API 与诊断功能继续留在账本。

学习主题：C01–C04，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [vendor/cordis/src](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/cordis/src)
- [vendor/timer](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/timer)
- [vendor/loader/src](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src)
- [docs/cordis-primer.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/cordis-primer.zh.md)
- [packages/test-support/loader-smoke](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/test-support/loader-smoke)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M1.1 | G0 | 建立 testdata/parity/cordis 的共享场景与语义 trace，记录 TS 依据、ID／时钟归一化规则 | 在 docs/cordis/source-map.md 解释对照方法和已有 Go 适配 | 期望来自源码／参考运行；显式状态与 happens-before 不被归一化抹掉 |
| M1.2 | M1.1 | 核对并补 cordis 的依赖、激活版本、配置更新、失败恢复 | 用状态图串起 Pending→Active→失效→恢复 | 加载中更新、Provider 多级失效、无效配置拒绝均有可控测试 |
| M1.3 | M1.2 | 核对 effect、子插件、手动 disposer、panic 与取消的所有权 | 以 defer 的边界解释跨调用资源所有者 | 清理至多一次；消费者先于提供方；失败仍继续清理；外部等待可收敛 |
| M1.4 | M1.2 | 核对服务隔离、五种事件分发与显式过滤；按 M0 决定补所需通知或元数据 | 对比直接调用、广播、短路、waterfall 和服务 Scope | Once 并发、监听卸载重叠、重复 Next 与 Global 规则有证据 |
| M1.5 | M1.3、M1.4 | 形成最小 Loader 所需的运行时契约，更新公开注释、API 与对应表 | 完善现有四主题练习和答案，新增必要完整程序 | 所有下游阻塞关闭或有经过验证的替代；明确哪些 Cordis 行为仍延后 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `cordis/`
- `testdata/parity/cordis/`
- `examples/cordis/`
- `docs/cordis/source-map.md`
- `docs/cordis/lifecycle.md`
- `docs/cordis/services.md`
- `docs/cordis/events.md`

## 读者实验

同一条依赖失效场景，分别观察状态、监听数、清理顺序；再在 Apply 中触发取消，说明过时激活为何不能重新发布服务。

## 行为与失败检查

- 激活失败后释放登记资源及返回的 Cleanup。
- Provider 在 Consumer 启动期间变化，旧绑定与旧结果不能泄漏。
- Cleanup 超时／panic 不阻止其他资源释放，最终状态可观察。
- 服务 Scope 与事件 Filter 不自动等价，Go 适配的差异被测试固定。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./cordis/... ./examples/cordis
make api
make check
```

## 退出门禁与交接

**G1：M0 标为下游阻塞的条目全部关闭；关键时序有测试，Go 适配有评审；C01–C04 可独立运行；API 与文档同步。**

向 M2 交付可用的注册、更新、卸载和等待契约；向 M3/M4 交付服务、事件与资源所有权基础。

## 阶段内要冻结的选择

未证明消费者需要的 Proxy、generator、模块 HMR 等不提前补实现；需要新增 API 时先写它解决的场景。Cordis 维持零第三方运行时依赖。
