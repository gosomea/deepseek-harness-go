# 前端工具链可行性与缺口

本文件如实记录 M11 在本环境中**可验证的部分**与**未交付的缺口**，避免把「Go Host 侧通过」读成「完整 Web/Desktop 客户端可用」。

## 环境事实

- node `v24.19.0`、pnpm `11.28.5`、npm `11.17.0` 可用（已探测）。
- 本仓库是单一 Go module，尚无 `web/`、`desktop/` 前端工程。

## 已交付（Go Host 侧，keyless 已验证）

| 能力 | 包 | 证据 |
| --- | --- | --- |
| 客户端投影（快照/增量/重连） | `plugins/clientview` | `./plugins/clientview/...` 测试 |
| 会话/工作区控制器（认证、按 principal 归属） | `plugins/hostapi` controller | `./plugins/hostapi/...` 测试 |
| Desktop Host 启动握手（就绪/失败/注入/关闭） | `plugins/desktophost` | `./plugins/desktophost/...` 测试 |
| 客户端接入示例 | `examples/client` | 输出与 expected.txt 一致 |

## 未交付的缺口（如实声明，不伪造通过）

- **真实 Web UI（React 组件、resources/store、chat/session 页面）**：未实现。需要一个完整前端工程与构建。
- **浏览器 E2E（Playwright）、可访问性、截图**：未运行。
- **Desktop（Electron/原生壳）打包、签名、私有 profile 实机验证**：未实现。
- **多客户端真机断线/重连联调**：仅以 `plugins/clientview` 的纯投影单元场景验证重连语义，未做真网络多客户端联调。
- **与参考 DSH 客户端的复用可行性**：未验证。

这些缺口在能力表中保留；M12 发行收口前不得据 M11 通过声称「Web/Desktop 客户端完整可用」。
