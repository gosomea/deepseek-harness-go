# M10 协议、SDK 与远程能力

## 阶段结果

让 Go Host 通过明确版本的外部协议提供能力，并用远程 Provider 替换本地执行世界。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M7](m07-local-product.md) 的 G7。

基础协议在 G7 后启动；压缩、子代理和任务相关协议分别等待 G8/G9 对应 gate。SDK、ACP、MCP、LSP、SSH、Web 与 hooks 各自验收，不合并成一个泛称 RPC 的承诺。

学习主题：C14，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages/api](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/api)
- [packages/host](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/host)
- [packages/sdk](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/sdk)
- [packages/acp](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/acp)
- [packages/typert](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/typert)
- [packages/mcp](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/mcp)
- [packages/lsp](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/lsp)
- [packages/ssh](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/ssh)
- [packages/hooks](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/hooks)
- [packages/web](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/web)
- [packages/subagent](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/subagent)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M10.1 | G7 | 盘点外部 endpoint、DTO、错误、流、权限与版本；建立 protocol 及 JSON fixture | 区分内部 Go 接口与稳定 wire 数据 | 冻结范围和版本；未知字段／方法／版本规则清楚 |
| M10.2 | M10.1 | 实现 Host API gateway 与核心 session/workspace 控制器、认证和订阅 | 比较请求结果、实时通知和恢复快照 | 跨会话授权、request ID、慢消费者、断连取消有契约 |
| M10.3 | M10.2 | 实现 SDK server/client、ACP bridge；各自做跨实现契约 | 同一 Agent 经 Go client 与参考客户端请求 | 分别证明 SDK/ACP 的 stream、取消、错误；不共享错误假设 |
| M10.4 | M10.1 | 实现 MCP client/resources 与 LSP stdio 生命周期 | 发现能力、调用、进程与取消分别解释 | 外部进程退出、能力变化、坏帧、超限输出均回收资源 |
| M10.5 | M10.4 | 实现 SSH 的 fs/subprocess/sandbox Provider；再接 PTY terminal | 将 M7 的同一工具换到远程世界 | 路径／cwd／凭据／重连一致；连接断开后的进程状态可观察 |
| M10.6 | M10.2、G8/G9 对应功能 | 增加压缩／子代理／任务 wire 面、外部 subagent Provider 与 hook 桥 | 区分协议适配与产品功能适配 | 每项依赖已有 owner gate；跨产品语义差异记录 |
| M10.7 | M10.1 | 接入 web fetch/search 与 webhook 边界，限制外部输入、输出和重试 | 外部内容与可信指令、结果结构与缓存 | 受控 mock HTTP、取消、限额与 webhook 重复投递有测试 |
| M10.8 | M10.3–M10.7 | 汇总兼容矩阵与 SDK 示例，为 M11 提供确定协议 | C14 同时运行 Host 和 client，制造断线后解释恢复 | 每个已承诺协议有自身契约与证据；未验证版本明确 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `protocol/`
- `plugins/hostapi/`
- `plugins/sdk/`
- `plugins/acp/`
- `plugins/mcp/`
- `plugins/lsp/`
- `plugins/ssh/`
- `plugins/terminal/`
- `plugins/web/`
- `plugins/hooks/`
- `examples/sdk/`
- `docs/protocols/`

## 读者实验

通过 SDK 启动 fake Agent 并接收流；中途断开后按协议恢复。另启动本地 fake MCP/LSP server，观察取消时 request 与进程资源的清理。

## 行为与失败检查

- 认证缺失、跨 Session 访问、迟到 response、重复 request ID。
- 订阅消费者过慢、缓冲达到上限、断线重放导致重复。
- MCP server 重启、LSP 坏帧、SSH 中断时远程命令状态未知。
- 把带 tRPC 内部类型的对象暴露为协议 DTO，边界检查拒绝。
- 外部 Provider 声称可继续而缺少必要状态，能力声明不能掩盖差异。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./protocol/... ./plugins/hostapi/... ./plugins/sdk/... ./plugins/acp/...
go run ./examples/sdk
make check
```

## 退出门禁与交接

**G10：承诺的协议逐项通过 mock 与跨实现契约；每个外部 Provider 有生命周期证据；C14 教程可运行。子协议 gate 分别记录，不能用一个成功 RPC 代替。**

M11 只接入已通过的 wire 能力；Python SDK 与发行包在 M12 收口。压缩／子代理等尚未通过时只能交付明确受限的核心协议切片。

## 阶段内要冻结的选择

实施时锁定 MCP/ACP/SDK 版本和参考客户端。Typert 可以换生成实现，但必须证明 wire 契约；SSH 和真实外部服务 smoke 独立报告，keyless mock 为必要门禁。
