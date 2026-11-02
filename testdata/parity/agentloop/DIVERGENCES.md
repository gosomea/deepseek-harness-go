# 已声明的差异：Agent 轮次场景

本文件记录 M5 的场景回放与「跨实现差分」之间的差别，避免读者把它读成与参考实现逐行对照过。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`。

| # | 差异 | 原因 | 影响 |
| --- | --- | --- | --- |
| D1 | 本目录的 trace 是**同一 Go 实现两次运行**的比较，不是与 TypeScript 参考的逐行比较 | M5 的参考侧是 Cordis 插件与 Agent 运行时，没有像 Session 的 `lib/` 那样可直接调用的离线构建产物；强行对照会得到不稳定的结果 | 本目录证明「行为确定」而不是「行为与参考一致」；后者需要先具备可离线调用的参考入口 |
| D2 | 事件集合只到 M5 为止 | 参考的实时侧还有 `agent/inbox/spliced`、`agent/status`、`agent/assistant-stream` 等流式事件 | 本目录只比较权威日志（turn/step/request/attempt/tool）的顺序，不比较实时投影 |
| D3 | 取消语义以 Go 的 `context` 为准 | Go 用 `context.WithCancelCause` 表达原因，参考用 `AbortSignal` 与 finish reason `aborted` | 「取消是独立于失败的事实」两侧一致；具体携带原因的机制不同 |
| D4 | 未实现 hook waterfall | 属后续阶段 | `agent/pre-step`、`agent/request` 等可插拔阶段没有对应物 |

## 未验证项

- **没有调用任何真实模型或本机工具**：全部场景使用 fake Provider、fake 工具与固定时钟。
- **没有与参考的跨实现对照**：如 D1 所述，这需要先具备可离线调用的参考构建产物；在那之前，本目录的结论范围仅限「确定性」。
