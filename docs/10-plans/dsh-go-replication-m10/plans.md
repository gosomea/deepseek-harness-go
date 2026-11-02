# M10 协议、SDK 与远程能力

本页是 `plan.json` 的主题页，与它共同决定 revision。

## 现状

M9 已通过其执行契约：子代理、目标与持久任务在本进程内可运行。M10 把 Go Host 的能力**通过明确版本的外部协议**暴露出去，并用远程 Provider 替换本地执行世界。

## 目标

让 Go Host 通过明确版本的外部协议提供能力。三个必须成立的性质：

1. **wire DTO 不含内部类型**：协议边界拒绝把带 tRPC／session 内部类型的对象当 DTO。
2. **版本与方法冻结**：版本不符在分发前拒绝；未知方法返回明确 code；未知字段容忍。
3. **keyless 可验证**：基础协议全部用回环／mock 契约，无网络、无凭据；真实外部服务与联网 smoke 单列，缺凭据时记为未验证。

## 进入条件

必需前驱：[M9 的 G9](../dsh-go-replication-m09/plans.md)，已通过。

## 执行契约

见 [plan.json](plan.json)。九个节点：

| 节点 | 切片 | 交付 |
| --- | --- | --- |
| `m101-protocol-dto` | M10.1 | protocol：版本、Envelope、WireError、Response 与校验 |
| `m102-hostapi-gateway` | M10.2 | Host API 网关：分发、认证、订阅 |
| `m103-sdk-acp` | M10.3 | SDK 客户端（SendFunc）与 ACP bridge |
| `m104-mcp-lsp` | M10.4 | MCP／LSP stdio 客户端生命周期（keyless） |
| `m105-ssh-remote` | M10.5 | SSH 远程执行 Provider seam（keyless mock） |
| `m106-wire-hook` | M10.6 | 压缩／子代理／任务 wire 面与 hook 桥 |
| `m107-web-hook` | M10.7 | web fetch/search 有界 seam |
| `m108-compat-matrix` | M10.8 | 兼容矩阵与 SDK 示例 |
| `g10-acceptance` | G10 | 复刻、学习与证据同时验收 |

## 产物所有权与不变量

- 共享门禁文件 `scripts/doccheck/*` 由 `m101` 登记（新运行时包），`g10` 冻结。
- 新运行时包登记进 `coverageRequirement` 与其 fixture：`protocol`、`hostapi`、`sdk`、`mcp`、`lsp`、`ssh`、`hooks`、`web`。
- `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md` 只在 `g10` 写一次。

## 学习交付

| 笔记 | 主题 |
| --- | --- |
| [protocols/overview.md](../../protocols/overview.md) | 内部接口与 wire DTO、请求关联、版本协商 |
| [protocols/compat-matrix.md](../../protocols/compat-matrix.md) | 每个已承诺协议与其证据、未验证版本 |

可运行实验：[examples/sdk](../../../examples/sdk/README.md)。

## 本阶段实际运行的命令与结果

| 阶段 | 命令 | 退出码 | 结果 |
| --- | --- | --- | --- |
| 契约 | `plan.py validate docs/10-plans/dsh-go-replication-m10/plan.json` | 0 | 9 节点，结构合法 |
| M10.1 | `go test -race -count=1 ./protocol/...` | 0 | 覆盖率 96.3% |
| M10.2 | `go test -race -count=1 ./plugins/hostapi/...` | 0 | 覆盖率 94.4% |
| M10.3 | `go test -race -count=1 ./plugins/sdk/... ./protocol/...` | 0 | sdk 96.3%；ACP 在 protocol 内 |
| M10.4 | `go test -race -count=1 ./plugins/mcp/... ./plugins/lsp/...` | 0 | 94.1% / 91.2% |
| M10.5 | `go test -race -count=1 ./plugins/ssh/...` | 0 | 100.0% |
| M10.6 | `go test -race -count=1 ./protocol/... ./plugins/hooks/...` | 0 | hooks 100.0%；wire 守卫在 protocol 内 |
| M10.7 | `go test -race -count=1 ./plugins/web/...` | 0 | 91.7% |
| M10.8 | `go test -race -count=1 ./examples/sdk/...` | 0 | 输出与 expected.txt 逐字一致 |
| g10 | `make check`（从仓库根） | 0 | fmt/vet/test/coverage/docs/doc-examples/build 全部通过 |

**这些都是本机结果。** 本地通过不代表 CI 已通过。

### 尚未验证

- 真实 MCP／LSP 服务器、真实 SSH 主机与联网 web smoke：本阶段全部 keyless／mock，缺凭据记为未验证缺口。
- Python SDK 与发行：属 M12。

## 阶段内的真实失败与处置

随实现推进按真实输出补录与修正。
