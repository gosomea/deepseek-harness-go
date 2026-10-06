# M9 子代理、目标与持久任务

## 阶段结果

让父 Agent 委派、等待、继续和取消子 Agent，并使目标、队列与定时任务在恢复后保持可解释的状态。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M8](m08-context.md) 的 G8。

先实现进程内 spawn/fork 和控制，再补 goal/todo/plan、持久 jobs/schedule。workflow 先交付 seam 与 fake 引擎；原 PTC 脚本 Provider 依赖 M12.4，不在此提前宣称完整支持。

学习主题：C13，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages/subagent](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/subagent)
- [packages/goal](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/goal)
- [packages/plan](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/plan)
- [packages/todo](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/todo)
- [packages/jobs](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/jobs)
- [packages/schedule](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/schedule)
- [packages/workflow](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/workflow)
- [docs/subsystems/subagent.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/subsystems/subagent.zh.md)
- [docs/subsystems/workflow.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/subsystems/workflow.zh.md)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M9.1 | G8 | 定义命名 subagent Provider 注册、功能描述、句柄与父子所有权 | 解释单服务 seam 与多 Provider 注册表 | 不支持的 continue/fork 能力明确拒绝 |
| M9.2 | M9.1 | 实现进程内 spawn、fork、继承切点、谱系与 scoped setup | 对比新会话与共享前缀分叉 | 父子后续日志独立，fork cut 和继承事实准确 |
| M9.3 | M9.2 | 实现消息投递、继续、等待、interrupt、深度／预算限制 | 预测父取消、子结束、迟到消息的结果 | 释放不泄漏；控制权限明确；重复投递有规则 |
| M9.4 | M9.3 | 实现 todo/plan/goal 的事件、状态投影及 goal 续跑策略 | 区分任务列表、计划模式、目标驱动 | 恢复状态一致；用户输入与续跑竞争有确定性仲裁 |
| M9.5 | M9.4 | 实现持久 jobs/schedule、可控时钟、claim/重试/恢复策略 | 解释 goroutine 与持久任务的差别 | 过期、重复触发、崩溃恢复、外部副作用不确定性分别处理 |
| M9.6 | M9.3、M9.5 | 定义 workflow seam、输入校验与 fake 引擎；对接子代理和任务生命周期 | 一个两步委派实验解释编排与执行 Provider | 未知脚本能力拒绝；meta/args 校验先于执行；parent 必需 |
| M9.7 | M9.6 | 交付 examples/subagents、examples/jobs 与跨重启状态 fixture | C13 画出生命周期树、Session 谱系和持久队列三张关系 | 每个 long-running owner 有结束／取消／恢复证据 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `capability/subagent/`
- `plugins/subagent/`
- `plugins/goal/`
- `plugins/todo/`
- `plugins/plan/`
- `plugins/jobs/`
- `plugins/schedule/`
- `capability/workflow/`
- `examples/subagents/`
- `examples/jobs/`
- `docs/tasks/`

## 读者实验

父 Agent fork 两个具有不同工具集的子 Agent，通过 fake 模型各完成一项工作；取消其中一个，继续另一个，再重启调度进程观察已结算任务不会重新产生副作用。

## 行为与失败检查

- 子 Agent 创建失败、Provider 卸载、父提前结束与取消传播。
- fork 后继承前缀不能复制成新产生的 child 创建事实。
- 重复消息、离线 child、未知描述符与无法继续的 Provider。
- 定时器跳时、进程崩溃、重试窗口以及幂等键冲突。
- 模型提出完成与实际目标状态、等待中的用户输入不一致。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./plugins/subagent/... ./plugins/jobs/... ./plugins/schedule/... ./plugins/goal/...
go run ./examples/subagents
go run ./examples/jobs
make check
```

## 退出门禁与交接

**G9：进程内子代理、目标与持久任务切片验收；可控时钟与恢复测试通过；workflow 的 fake 契约及生产 Provider 缺口明确。**

M10 的外部 subagent/任务协议切片必须等待本阶段相应 gate；M12 用 PTC Provider 替换 fake workflow 引擎并完成原脚本语义兼容。

## 阶段内要冻结的选择

按功能描述处理外部 Provider 差异。持久副作用不能默认 exactly-once；幂等能力不足时明确不确定状态和人工恢复方式。
