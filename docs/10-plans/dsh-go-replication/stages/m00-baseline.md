# M0 固定基线与 Cordis 差距审计

## 阶段结果

建立可追溯的复刻起点：每个 Cordis 行为都能找到固定 TS 来源、Go 状态、现有测试和学习入口。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

无上游阶段；先确认工作树、固定源码基线和现有 M0 契约没有活动尝试冲突。

核对已保存的来源清单、补逐行为账本、阅读现有 Cordis；不新增运行时功能。现有 plan.json 是本阶段的执行契约。

学习主题：C01–C04，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [vendor/cordis/src](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/cordis/src)
- [vendor/loader/src](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader/src)
- [docs/architecture.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/architecture.zh.md)
- [docs/capability-seams.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/capability-seams.zh.md)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M0.1 | 无 | 复核 source-inventory.json 的提交、307 个包与 9 个 vendor；逐项检查 capabilities.md 的归属 | 解释范围盘点与行为覆盖的区别 | 来源来自固定 Git 对象；54 个组全部有归属 |
| M0.2 | M0.1 | 创建 cordis-audit.md；逐项写 TS 符号／参考场景、Go 符号、测试名、行为差异 | 将 C01–C04 实验对应到审计行为 ID | 至少覆盖实例、依赖、Scope、effect、事件、更新、取消、诊断八类 |
| M0.3 | M0.2 | 为每个缺口判定下游阻塞、Go 适配、后期功能或待研究；写出 M1 的有限范围 | 解释 Fiber≠goroutine、两种 Context、服务与 Agent 作用域 | 待研究项有调查入口；阻塞项有具体消费者，不能只看 API 名称 |
| M0.4 | M0.3 | 执行现有 Cordis 与完整仓库检查，补语义评审并导出 M0 证据 | 按现有教程预测、运行、制造依赖缺失，再定位源码 | 复刻与学习两目标都通过评审；旧覆盖率不代替行为审计 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `docs/10-plans/dsh-go-replication/cordis-audit.md`
- `docs/10-plans/dsh-go-replication/source-inventory.json`
- `docs/10-plans/dsh-go-replication/capabilities.md`
- `docs/learning/index.md`

## 读者实验

运行 examples/cordis，先预测 Consumer 没有 Provider 的状态，再移除／恢复 Provider；把每行输出定位到 Go 测试和 TS 源码。

## 行为与失败检查

- 本地 DSH 工作树有未提交修改：清单仍只能来自固定提交。
- 源包组存在但没有阶段归属：范围审计拒绝。
- Go 测试通过而参考行为不同：标记差异，不标等价。
- 教程承诺与输出不同：修复或保持未验，不调整基线掩盖差异。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./cordis ./examples/cordis
make check
```

## 退出门禁与交接

**G0：来源清单经审阅；Cordis 审计包含八类行为及全部既有未实现项的分类；首个 M1 切片明确；M0 三节点均有实际证据与评审。**

将已验证的来源和 Cordis 审计交给 M1。主计划 source-baseline、cordis-audit、baseline-acceptance 分别承载 M0.1、M0.2–M0.3、M0.4；细切片不是新增可领取节点。

## 阶段内要冻结的选择

M0 不用新增代码证明审计完成。发现现有运行时 bug 时先记录契约与影响，进入 M1 的修复切片并重验。
