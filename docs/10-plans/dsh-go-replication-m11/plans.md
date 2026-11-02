# M11 Web 客户端与 Desktop 载体

本页是 `plan.json` 的主题页，与它共同决定 revision。

## 现状

M10 已通过其执行契约：Go Host 通过版本化协议暴露能力。M11 让多个客户端连接 Go Host，以权威 Session 和投影恢复界面。

## 目标与范围口径

让客户端以 host 投影恢复界面。**本阶段的诚实边界**：Go Host 侧行为（客户端投影、会话控制器、Desktop 启动握手）keyless 验证；真实 Web UI（React）、浏览器 E2E、Desktop 打包/签名在本环境无法交付，作为**明确缺口**记录，不伪装通过、也不降低门禁。

三个必须成立的性质：

1. **UI 不生成第二份权威日志**：客户端只持 host 投影。
2. **快照+增量衔接无缺失/重复**：重连按序列续接，空洞时回退快照。
3. **慢消费者丢事件**：订阅缓冲满时丢弃而非阻塞 host。

## 进入条件

必需前驱：[M10 的 G10](../dsh-go-replication-m10/plans.md)，已通过。

## 执行契约

见 [plan.json](plan.json)。七个节点：

| 节点 | 切片 | 交付 |
| --- | --- | --- |
| `m111-client-spike` | M11.1 | `plugins/clientview` seam：快照/增量投影 |
| `m112-snapshot` | M11.2 | 快照/增量构建与视图合成 |
| `m113-session-controller` | M11.3 | `plugins/hostapi` 会话/工作区控制器（认证、归属） |
| `m114-reconnect` | M11.4 | 断线恢复 Hub（增量续接/快照回退） |
| `m115-desktop-bridge` | M11.5 | `plugins/desktophost` Host 侧启动握手 |
| `m116-spike-evidence` | M11.6 | `docs/client/frontend-gap.md` 可行性证据与缺口 + `examples/client` |
| `g11-acceptance` | G11 | Go Host 侧验收；前端缺口如实记录 |

## 产物所有权与不变量

- 共享门禁文件 `scripts/doccheck/*` 由 `m111` 登记（新运行时包），`g11` 冻结。
- 新运行时包登记进 `coverageRequirement` 与 fixture：`clientview`、`desktophost`（`hostapi` 已在 M10 登记）。
- `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md` 只在 `g11` 写一次。

## 学习交付

| 笔记 | 主题 |
| --- | --- |
| [client/overview.md](../../client/overview.md) | Host 权威状态、客户端投影、快照/增量/重连 |
| [client/frontend-gap.md](../../client/frontend-gap.md) | 前端工具链可行性与未交付缺口 |

可运行实验：[examples/client](../../../examples/client/README.md)。

## 本阶段实际运行的命令与结果

| 阶段 | 命令 | 退出码 | 结果 |
| --- | --- | --- | --- |
| 契约 | `plan.py validate docs/10-plans/dsh-go-replication-m11/plan.json` | 0 | 7 节点，结构合法 |
| M11.1/2/4 | `go test -race -count=1 ./plugins/clientview/...` | 0 | 覆盖率 90.6% |
| M11.3 | `go test -race -count=1 ./plugins/hostapi/...` | 0 | 覆盖率 94.1% |
| M11.5 | `go test -race -count=1 ./plugins/desktophost/...` | 0 | 覆盖率 100.0% |
| M11.6 | `go test -race -count=1 ./examples/client/...` | 0 | 输出与 expected.txt 逐字一致 |
| g11 | `make check`（从仓库根） | 0 | fmt/vet/test/coverage/docs/doc-examples/build 全部通过 |

**这些都是本机结果。** 本地通过不代表 CI 已通过。

### 尚未验证（明确缺口）

- **真实 Web UI（React 组件、resources/store、chat/session 页面）**：未实现。
- **浏览器 E2E（Playwright）、可访问性、截图**：未运行。
- **Desktop（Electron/原生壳）打包、签名、私有 profile 实机验证**：未实现。
- **多客户端真机断线/重连联调**：仅以纯投影单元场景验证重连语义。
- **与参考 DSH 客户端复用可行性**：未验证。

详见 [docs/client/frontend-gap.md](../../client/frontend-gap.md)。M12 发行收口前不得据 M11 通过声称「Web/Desktop 客户端完整可用」。

## 阶段内的真实失败与处置

1. `docs/client/overview.md` 首版 `output-file` 块为空，doccheck 报「output differs」→ 填充为 expected.txt 全文后通过。
2. 其余文档（README 使用/验证节、链接深度）按 doccheck 提示逐条修正。
