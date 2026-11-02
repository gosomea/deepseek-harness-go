# 协议兼容矩阵

本文件记录 M10 承诺的每个协议、其 Go 入口、证据与未验证项。**未验证 ≠ 通过**：缺凭据或缺失外部服务时如实记为未验证缺口。

| 协议／能力 | Go 入口 | 证据 | 状态 |
| --- | --- | --- | --- |
| 原生 wire（version/Envelope/WireError） | [protocol](../../protocol/README.md) | `./protocol/...` 测试 | keyless 已验 |
| Host API 分发／认证／订阅 | [plugins/hostapi](../../plugins/hostapi/README.md) | `./plugins/hostapi/...` 测试 | keyless 已验 |
| Go SDK 客户端（SendFunc） | [plugins/sdk](../../plugins/sdk/README.md) | `./plugins/sdk/...` 测试 | keyless 已验 |
| ACP bridge（DTO 映射） | [protocol](../../protocol/README.md) `acp.go` | `./protocol/...` 测试 | keyless 已验 |
| MCP stdio 客户端生命周期 | [plugins/mcp](../../plugins/mcp/README.md) | `./plugins/mcp/...` 测试 | keyless 已验；真实 MCP server 未验证 |
| LSP stdio 客户端生命周期 | [plugins/lsp](../../plugins/lsp/README.md) | `./plugins/lsp/...` 测试 | keyless 已验；真实语言服务器未验证 |
| SSH 远程执行 Provider seam | [plugins/ssh](../../plugins/ssh/README.md) | `./plugins/ssh/...` 测试 | keyless 内存模型已验；真实 SSH 主机未验证 |
| hook 桥 | [plugins/hooks](../../plugins/hooks/README.md) | `./plugins/hooks/...` 测试 | keyless 已验 |
| web fetch（有界） | [plugins/web](../../plugins/web/README.md) | `./plugins/web/...` 测试 | keyless 已验；联网 smoke 未验证 |
| Python SDK／发行 | — | — | 属 M12 |

## 未验证项（如实声明）

- **真实 MCP／LSP 服务器**：本阶段以 reader/writer 对模拟 stdio，未连真实服务器。
- **真实 SSH 主机**：以内存模型验证路径边界与断连，未连真实主机。
- **联网 web smoke**：全部用函数 backend，未联网。
- **跨实现互操作**：未与参考 DSH 的 SDK／ACP 客户端逐行对照。

这些缺口在能力表中保留，M12 收口前不得据 M10 通过声称「完整协议兼容」。
