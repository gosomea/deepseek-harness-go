# M3 对话词汇、Agent Scope 与内存 Session

## 阶段结果

从权威事件日志重建同一份模型历史，建立后续流、工具与循环共享的数据契约。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M1](m01-cordis.md) 的 G1。

先交付主链所需的 text、reasoning、tool-call/result、来源与事件 envelope，明确不支持的模态。此时不调用真实模型、不接磁盘。

学习主题：C06 的数据模型、C07，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages/core/scope](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/core/scope)
- [packages/core/session](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/core/session)
- [packages/llm/llm](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/llm/llm)
- [packages/session/session-projection](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/session/session-projection)
- [docs/subsystems/session.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/subsystems/session.zh.md)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M3.1 | G1 | 在 llm 定义独立于 tRPC 的消息／内容块／工具结果、ID 与 JSON codec | C06 比较 Go 判别字段与 TS 可扩展联合类型 | 必需字段与无损 JSON 校验；未知内容不被静默转成 text |
| M3.2 | M3.1 | 在 scope 实现 Agent 可见层和贡献所有权；与 Cordis 服务隔离分开 | 通过两个 Agent 可见集合解释作用域身份 | Agent A 的工具／prompt 不泄漏给 B；全局层规则明确 |
| M3.3 | M3.1 | 在 session 定义事件 envelope、连续 seq、append 和事件扩展 codec | C07 从 slice 历史过渡到 append-only log | 追加失败不推进 seq；外部修改不能悄悄改变已提交事实 |
| M3.4 | M3.2、M3.3 | 实现消息投影和注册的纯 projection；区分模型 surface 与审计事件 | 同一日志导出模型历史与诊断视图 | 重复 fold 一致；缺失必要 projection 显式失败；审计事件不凭空进入模型 |
| M3.5 | M3.4 | 交付 Session replay 示例、固定事件 fixture 与文档 | 预测加入 tool/result、失败 attempt 后的历史差异 | 相同输入得到相同投影；序号间隙和未知必读事件被拒绝 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `llm/`
- `scope/`
- `session/`
- `examples/session/`
- `testdata/parity/session/`
- `docs/llm/`
- `docs/session/`

## 读者实验

从一组事件投影用户输入、assistant tool-call、tool result 和回答，再加入只用于审计的事件；解释日志长度变化而模型历史可能不变。

## 行为与失败检查

- 重复 seq、间隙、乱序、未知 codec 与缺失必需内容。
- 持有原始 map/slice 的调用方尝试修改已提交值。
- 两个独立读取器用相同 projection 得到同样历史。
- 工具调用与结果关联失败，不允许靠顺序猜测 ID。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./llm/... ./scope/... ./session/...
go run ./examples/session
make check
```

## 退出门禁与交接

**G3：模型词汇、事件与投影语义成文；确定性重放与隔离成立；支持／拒绝的事件和模态清楚；C06/C07 首批实验通过。**

向 M4 提供无框架类型的 llm 数据面和 scoped 注册；向 M5 提供追加与投影；持久化和格式互读由 M6/M12 承接。

## 阶段内要冻结的选择

本阶段冻结最小数据契约，扩展仍需版本规则。Go 的接口值不直接序列化；采用明确 codec，禁止把动态 any 作为所有协议的默认出口。
