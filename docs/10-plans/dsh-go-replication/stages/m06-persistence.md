# M6 Session 持久化、恢复与格式边界

## 阶段结果

关闭并重新打开一个会话，重建相同历史和待处理状态，并明确可见性、落盘、单写者与崩溃恢复承诺。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M5](m05-agent-loop.md) 的 G5。

交付 Go 自有格式与独立数据目录、持久化 seam、JSONL Provider、恢复和 checkpoint。DSH generation 互读及历史迁移在 M12 做独立兼容验收。

学习主题：C10，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages/session/session-persistence](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/session/session-persistence)
- [packages/session/session-persistence-jsonl](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/session/session-persistence-jsonl)
- [packages/session/session-checkpoint-policy](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/session/session-checkpoint-policy)
- [packages/session/session-format](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/session/session-format)
- [packages/session/session-format-catalog](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/session/session-format-catalog)
- [docs/subsystems/persistence.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/subsystems/persistence.zh.md)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M6.1 | G5 | 在 persistence 定义 create/open/stat/list 与逐会话 Handle；冻结只读、关闭和一致性规则 | 解释接口服务、单次打开句柄与单写者 | 只读写入、关闭后操作、第二写者都明确拒绝 |
| M6.2 | M6.1 | 实现 plugins/sessionjsonl 的 framing、header、append/flush 和进程间所有权 | 比较 append 可见、flush 持久、whenIdle 活动结束 | 失败不会报告已持久；并发 reader 可见前缀符合契约 |
| M6.3 | M6.2 | 定义自有格式版本、损坏尾部、seq 校验与恢复步骤 | 用损坏副本演示读取与修复 | 未知未来版本拒绝；中间损坏不当作可忽略尾部 |
| M6.4 | M6.3 | 连接 agent resume、持久 inbox 与 checkpoint 策略；明确中断轮次结算 | 重启前后预测哪些工作会继续 | 恢复不会盲目重放已经执行的工具；不确定副作用保持可观察 |
| M6.5 | M6.4 | 建立 session projection/cache 的重建边界和存储契约 fake | 解释派生缓存与权威日志 | 删除缓存后重建一致；缺少必要投影明确失败 |
| M6.6 | M6.5 | 交付 examples/session-replay、跨进程 fixture 与存储文档 | C10 在临时 home 完成创建、flush、关闭、重开、对照 | 恢复／双写者／future version／关闭错误均有实际证据 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `persistence/`
- `plugins/sessionjsonl/`
- `session/`
- `agentloop/`
- `examples/session-replay/`
- `testdata/parity/persistence/`
- `docs/persistence/`

## 读者实验

在临时 home 中运行 fake 轮次，显式 flush 后退出，再开新进程读取。比较历史与 pending work；另对复制的日志截断尾部，观察恢复诊断。

## 行为与失败检查

- 进程间同时抢同一写句柄；测试用屏障而非 sleep。
- 写入、flush、rename 或 close 注入失败，不能吞错误。
- 未知未来 header、序号间隙、中间损坏与可修复尾部分别处理。
- 崩溃前工具副作用已发生但结果未结算，恢复策略不自动重执行。
- 只读 open 不能发布迁移文件或改写用户原始数据。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./persistence/... ./plugins/sessionjsonl/... ./agentloop/...
go run ./examples/session-replay
make check
```

## 退出门禁与交接

**G6：临时目录跨进程重开一致；单写者与版本保护成立；checkpoint/idle 边界和未知副作用策略清楚；C10 实验可复制。**

为 M7 CLI 恢复、M8 压缩、M9 持久任务提供句柄和版本基础。storage JSON/SQLite 通用领域存储不与 Session 格式混为一套接口。

## 阶段内要冻结的选择

单写者方案、文件锁跨平台行为和 fsync 承诺在 M6.1 冻结并按 OS 验证。没有完成格式互操作前，不读取或改写原 DSH home。
