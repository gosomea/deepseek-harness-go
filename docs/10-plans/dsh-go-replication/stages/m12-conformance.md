# M12 全域覆盖、互操作与发行收口

## 阶段结果

对固定基线的每个包和行为给出可追溯结论，补齐余下兼容与 Provider，并发布范围准确、可复现的完整产品。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M9](m09-long-tasks.md) 的 G9、[M11](m11-clients.md) 的 G11。

本阶段必须拆为独立子计划，不是一次大合并。G9 与 G11 是总体入口；可提前调查，但任何生产扩展仍需对应领域前驱。延后项保留缺口，不得用汇总文档替代实现。

学习主题：C16 及各领域补充，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages)
- [vendor](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor)
- [apps](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/apps)
- [python](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/python)
- [native](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/native)
- [website](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/website)
- [scripts](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/scripts)
- [benchmarks](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/benchmarks)
- [snapshots](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/snapshots)
- [patches](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/patches)
- [THIRD_PARTY_NOTICES.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/THIRD_PARTY_NOTICES.md)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M12.1 | G9、G11 | 将 307 个包、9 个 vendor、apps/Python/native/工程支持逐项展开为行为账本；为所有未归属项指定 owner 和 gate | 建立全域概念→接口→Provider→Consumer→学习入口索引 | 每项有等价、适配或缺口结论；比例分母保留，不能用包组表冒充逐包覆盖 |
| M12.2 | M12.1、G2 | 补 YAML/include/惰性配置/volatile/HMR 与原 profile 兼容；设计外部插件安装和进程隔离替代 | 说明 Go 静态二进制与 Node 模块加载边界 | 原配置 fixture 有指定版本结果；不支持的动态语义显式拒绝；插件安装失败可恢复 |
| M12.3 | M12.1、G6 | 实现 DSH Session 格式读取／迁移／generation 发布和完整 storage/query Provider | 比较自有格式、原 DSH 历史格式、索引与只读迁移 | 逐版本 golden + 跨实现验证；未来格式拒绝；原 generation 不被覆盖；压缩格式支持明确 |
| M12.4 | M12.1、G7、G9 | 实现 PTC 执行 Provider及生产 workflow 引擎、hooks 与外部扩展桥 | 解释模型脚本、子工具 dispatch、策略继承与子代理归属 | 原 workflow 脚本 fixture、策略拒绝、超时／取消、结果邻接和资源隔离验收 |
| M12.5 | M12.1、G10、G11 | 逐类补浏览器、计算机操作、Office、语音、Agent Teams、WebWorker 等实验能力 | 每项独立概念页与最小实验，定位其能力 seam | 每项独立子计划、环境支持和开关；真实环境缺失不能由 mock 代替完整验收 |
| M12.6 | M12.1、G10、G11 | 补凭据／账户平台、遥测、全部模型/远程 Provider、反馈/交付物及客户端剩余功能 | 解释隐私字段、产品数据与模型上下文的边界 | 逐 Provider 能力矩阵、错误与回放；UI/协议/存储消费方成套验证 |
| M12.7 | M12.2–M12.6 | 完成 native 平台矩阵、真实负载基准、泄漏／恢复耐久测试 | C16 用 profile 和 trace 定位长 Session／多 Agent 开销 | 基准输入、环境、阈值与统计方法固定；无模型网络抖动干扰必要性能门禁 |
| M12.8 | M12.7 | 完成 CLI/Host/Desktop/Python SDK 发行、依赖锁定、许可证、升级／回滚与构建 smoke | 按发布包从零安装、启动、恢复和诊断 | 构建来源可追溯；三平台矩阵；真实打包／安装证据；缺签名权限明确阻塞发布 gate |
| M12.9 | M12.8 | 完成文档网站与语言范围决策、全域对照审阅、发布范围与版本说明 | C16 从入门路径到全域源码索引可导航 | 全部承诺有实现、学习、测试、兼容和平台证据；范围变更需明确批准 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `docs/conformance/`
- `docs/compatibility/`
- `docs/release/`
- `benchmarks/`
- `scripts/`
- `python/`
- `native/`
- `website/`
- `plugins/`
- `web/`
- `desktop/`
- `THIRD_PARTY_NOTICES.md`

## 读者实验

从干净环境安装候选发布包，加载固定 profile，读取指定格式的会话，运行工具与子代理后重启恢复；再按能力索引追到相应学习页、Go 源码和 TS 基线。

## 行为与失败检查

- 未审计包或功能被遗漏，范围 gate 必须拒绝完整复刻声明。
- 一次 SDK 升级破坏 reason/usage/replay 字段，适配器门禁拒绝。
- 迁移到新 generation 后原文件变化，兼容 gate 拒绝。
- 某 OS 用空 stub 返回成功，支持矩阵与功能 gate 必须拒绝。
- 发布包与测试提交不一致，或只从源码运行未测安装产物。
- 性能提升依靠丢失事件、缩短历史或降低并发负载，基准无效。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
make check
go test -race -count=1 -timeout=60s ./...
go test -run '^$' -bench . -benchmem ./benchmarks/...
```

## 退出门禁与交接

**G12：所有子计划、兼容与平台 gate 通过；基线账本无未解释／未批准缺口；每个学习主题和发布入口可复现。仍有延后项时只发布范围明确的阶段版本。**

发布后按固定 DSH 基线维护；升级参考 SHA 或依赖要建立差异计划并重新验证受影响分支。历史 evidence 保留，当前版本另行声明。

## 阶段内要冻结的选择

网站双语、桌面签名、第三方账户接入和特定硬件能力在各子计划进入前明确选择及外部条件。缺少环境时可以完成可验证的独立分支，但完整 gate 保持未通过。
