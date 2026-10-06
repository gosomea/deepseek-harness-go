# M8 指令、技能、附件与上下文压缩

## 阶段结果

使所有进入模型的上下文可由日志重建，并在工具输出、附件和历史增大时有可解释的预算与压缩行为。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M7](m07-local-product.md) 的 G7。

逐项补模型可见贡献、设置与 preset、引用／附件、spill 和 compaction。多模态只有在适配器、重放和消费者共同支持后才开放。

学习主题：C12，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages/context](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/context)
- [packages/skill](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/skill)
- [packages/attachment](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/attachment)
- [packages/spill](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/spill)
- [packages/compaction](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/compaction)
- [packages/settings](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/settings)
- [packages/preset](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/preset)
- [packages/deliverables](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/deliverables)
- [docs/subsystems/compaction.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/subsystems/compaction.zh.md)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M8.1 | G7 | 把 instructions、时间、文件／Session 引用组织为 owned 贡献，记录模型可见来源 | 分清日志事实、prompt 渲染、请求瞬时访问路径 | 同一版本代码和日志重建请求；来源与内容形式分开 |
| M8.2 | M8.1 | 实现 settings、preset/persona 与 Agent 独立能力配置 | 两个 Agent 使用不同工具和说明 | 默认值／会话覆盖／作用域不串用；热变更规则明确 |
| M8.3 | M8.2 | 实现 skills 发现、按需读取／启用与依赖声明 | 比较技能目录清单和实际加载内容的 token 影响 | 未启用内容不意外进入请求；卸载清理贡献 |
| M8.4 | M8.1 | 实现附件身份、不可变存储和请求访问映射；补模型能力声明 | 解释持久附件与本次请求可访问路径 | 损坏附件、换执行世界、不支持模态都有诊断 |
| M8.5 | M8.3、M8.4 | 实现 token 计量、输出保留策略、spill 及可追溯引用 | 长输出如何缩短但仍可定位原文 | 截断信息不丢；spill 生命周期明确；usage 与估算区分 |
| M8.6 | M8.5 | 实现 compaction seam、fake 摘要 Provider、start/summary/end 与表面替换 | 先预测压缩后 history，再从日志重建 | 保留 call/result 配对；摘要失败不伪造成功；并发上下文不丢 |
| M8.7 | M8.6 | 接真实摘要路由，加入不同模型能力／预算的集成 fixture | C12 解释压缩调用自身的来源、usage 与恢复 | 每次摘要调用可重建；遗留锁、换路由、取消和多模态有边界 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `capability/attachment/`
- `capability/spill/`
- `capability/compaction/`
- `plugins/context/`
- `plugins/skills/`
- `plugins/settings/`
- `plugins/preset/`
- `plugins/compaction/`
- `examples/context/`
- `docs/context/`

## 读者实验

给 fake Agent 注入技能、文件引用和超长工具输出，观察预算与 spill；运行 fake 摘要压缩，重放全部日志并比较压缩后的实际请求。

## 行为与失败检查

- 路径存在但附件读取能力不足或文件变化，不能使用过期访问引用。
- 工具 call/result 跨压缩边界；不得留下悬空关联。
- 压缩中出现新注入、取消或崩溃，范围重验与遗留状态明确。
- 路由切换后旧 Provider replay state 不应无条件交给另一家。
- 摘要成功但写日志失败，不能报告已完成替换。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./plugins/context/... ./plugins/skills/... ./plugins/compaction/... ./session/...
go run ./examples/context
make check
```

## 退出门禁与交接

**G8：模型可见可重建不变量覆盖新增贡献；技能、附件、spill、预算与压缩实验通过；每个模态有明确支持矩阵。**

M9 在受控预算和 preset 上运行子代理；M10/M11 扩展相应 wire 类型与 UI。完整 schema/API 的新增必须连带更新各消费者。

## 阶段内要冻结的选择

先采用可确定性测试的预算／token 估算并记录误差边界。不会把只写审计的 compaction 事件自动等同为模型可见摘要；表面变更按固定源码核对。
