# M5 无密钥的完整 Agent 轮次

## 阶段结果

由统一 app 装配启动一个 Agent，经两次 fake 模型请求与一次 fake 工具调用完成轮次，并从日志重建每次模型可见输入。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M2](m02-composition.md) 的 G2、[M4](m04-model-tools.md) 的 G4。

实现公开 Agent 契约、工厂、拥有者句柄、inbox 和默认 driver；先支持可验证的最小轮次，再补输入路由、维护与取消。所有必要路径使用 fake。

学习主题：C09，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages/core/agent](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/core/agent)
- [packages/core/agent-loop](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/core/agent-loop)
- [packages/core/agent-default-model](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/core/agent-default-model)
- [packages/core/agent-tool-presentation](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/core/agent-tool-presentation)
- [packages/test-support/agent-loop-testkit](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/test-support/agent-loop-testkit)
- [docs/agent-lifecycle.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/agent-lifecycle.zh.md)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M5.1 | G2、G4 | 定义 agent 公共句柄、状态、创建／恢复所需接口与 factory seam；默认实现放 agentloop | 解释句柄、实例、运行驱动与销毁能力 | 外部消费者依赖 agent；创建失败不发布半个实例 |
| M5.2 | M5.1 | 实现 scoped setup、发布、初始化等待、所有者释放与 provider 卸载 | 用 setup 失败观察事务边界 | 先 setup/初始化后处理输入；失败回滚服务、Session 和注册表 |
| M5.3 | M5.2 | 实现 turn→step→请求→流 settlement→工具 batch→后续 step | 画出 event log 与实时流两条时间线 | 每次请求可由日志重建；call 在执行前记录；结果按契约结算 |
| M5.4 | M5.3 | 实现 inbox、send 路由、steering、wakeup、idle 与维护任务边界 | 预测轮次中追加输入在哪一步生效 | 排队输入不重复领取；idle 等待涵盖替换工作；零 step 轮次闭合 |
| M5.5 | M5.4 | 实现首个取消原因、流中断、工具取消、关闭与并发控制 | 比较取消一轮、保留 inbox、销毁 Agent | 取消后不派发新工具；已发生结果仍按规则入日志；停止可等待 |
| M5.6 | M5.5 | 交付 app fake profile、examples/harness 和完整轮次 fixture | C09 从运行结果定位 agent/agentloop/Session/LLM/Tools | 正常、失败、取消、排队各有确定性快照与人工讲解 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `agent/`
- `agentloop/`
- `app/`
- `examples/harness/`
- `testdata/parity/agentloop/`
- `docs/agent/`
- `docs/agentloop/`

## 读者实验

预置 fake 模型先请求一次加法工具，再读取工具结果给出答案。输出 turn/step/attempt/call/result 顺序，重放 Session 并比较两次模型请求。

## 行为与失败检查

- setup 或 agent created 初始化失败，Agent 和 Session ID 不泄漏。
- 输入被拒绝或取消，可能有 turn 而没有 step。
- 流中断前已有可见前缀，按基线结算；未派发 tool-call 不执行。
- 同一 Agent 的活动串行，多个 Agent 作用域和取消独立。
- 卸载工厂提供方清理其创建的活跃实例，裸查询句柄不获得销毁权限。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./agent/... ./agentloop/... ./app/...
go run ./examples/harness
make check
```

## 退出门禁与交接

**G5：无密钥主链可运行；完整日志重建请求；取消与创建回滚成立；C09 可解释 turn/step/attempt/inbox；只宣称最小 Harness 检查点。**

M6 将已建立的 Session/Agent 恢复语义接入存储；M7 将相同 loop 接到本机 Provider。agentloop 不能出现供应商和文件系统专用分支。

## 阶段内要冻结的选择

需要默认模型、工具呈现或诊断时通过插件贡献；不把这些业务硬编码为 loop 的特权。真实模型验证不属于 G5 的必要条件。
