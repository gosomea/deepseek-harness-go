# M4 tRPC 模型适配、工具管线与提示词

## 阶段结果

把同一套 DSH-Go 请求／响应契约同时接到 fake 和 tRPC 模型 Provider，并让 fake 工具经过可观测的准入与执行管线。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M3](m03-session.md) 的 G3。

实现自有流与 assembler、tRPC 模型层适配和回环测试、fake 工具与准入接口、prompt。SDK 版本在本阶段锁定；Agent/Runner/Session 仍由项目自有实现负责。

学习主题：C06 的流与适配、C08，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages/llm/llm](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/llm/llm)
- [packages/llm/llm-deepseek](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/llm/llm-deepseek)
- [packages/llm/llm-retry](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/llm/llm-retry)
- [packages/core/tools](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/core/tools)
- [packages/core/system-prompt](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/core/system-prompt)
- [docs/tool-execution-pipeline.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/tool-execution-pipeline.zh.md)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M4.1 | G3 | 冻结 llm 请求、流、完成／错误／usage 契约，建立 fake Provider 和可控流 | 把 HTTP chunk、delta、message、attempt 分别解释 | 正文／reasoning／工具参数可按任意分片测试；无正常终止也可诊断 |
| M4.2 | M4.1 | 实现 assembler 和 Provider 注册；固定路由能力与 replay 字段边界 | 比较流式显示和最终组装结果 | 多个 tool-call ID 不混合；unknown usage 与零 usage 区分 |
| M4.3 | M4.2 | 锁定 tRPC-Agent-Go 版本，实现 plugins/llmtrpc 请求／响应映射和回环 HTTP fixture | DSH↔Go↔tRPC 三列表；跟踪一次错误和取消 | 通过总方案的 tRPC 适配器门禁；核心包没有 tRPC 类型依赖 |
| M4.4 | G3 | 定义 tools 注册／schema、pre-execute、单调 guard、执行、post/finalize/result 顺序 | C08 通过被拒绝的 fake 工具解释能力与策略 | 参数错误或拒绝不进工具体；最终结果唯一；观察者看到权威结果 |
| M4.5 | M4.2、M4.4 | 实现 systemprompt 片段顺序、scope 与工具 schema 装配 | 预测新增 scoped 工具如何改变请求 | 裸请求和 Agent 请求的可见层一致；请求可确定性比较 |
| M4.6 | M4.3、M4.5 | 交付 examples/model、examples/tools、跨 Provider 契约与边界检查 | 相同请求分别通过 fake/tRPC 回环执行，解释转换损失如何暴露 | 所有必要测试无密钥无外网；重试所有者与取消清理可观察 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `llm/`
- `plugins/llmtrpc/`
- `tools/`
- `systemprompt/`
- `internal/testkit/`
- `examples/model/`
- `examples/tools/`
- `docs/llm/`
- `docs/tools/`
- `docs/systemprompt/`
- `go.mod`
- `go.sum`

## 读者实验

回环服务分多块返回 reasoning、文本和工具参数；tRPC 适配器转换后交给自有 assembler。再拒绝工具执行，观察工具体计数为零并产生一个错误结果。

## 行为与失败检查

- 连接建立失败与 Response.Error 的流内错误分别映射。
- 中途取消、消费者提前退出、慢读取，连接与 goroutine 都释放。
- 底层 SDK 重试设置可验证，不产生隐形重复请求。
- 新增模型扩展字段与缺少能力必须显式报错，不能静默丢失。
- 工具后置处理或快照失败仍生成规范化结果，finalize 的作用范围明确。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./llm/... ./plugins/llmtrpc/... ./tools/... ./systemprompt/...
go run ./examples/model
go run ./examples/tools
make check
```

## 退出门禁与交接

**G4：模型与工具两个分支都验收；tRPC 版本、Go 工具链与许可已记录；回环适配、schema、策略、prompt 和学习实验通过。**

M5 使用 fake Provider 建立循环；M7 使用已验 tRPC 适配器接凭据与真实端点。两者共享 llm 契约，不能各自定义消息格式。

## 阶段内要冻结的选择

参数映射、工具 schema 与 retry 不委托给另一套 Agent runtime。关键扩展不足时先实现显式适配或记录阻塞；不能通过丢字段使兼容测试变绿。
