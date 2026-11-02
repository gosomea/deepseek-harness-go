---
description: "Agent 轮次的确定性场景：同一份 fixture 回放出同一条 trace。"
kind: "package-library"
---

# testdata/parity/agentloop

## 概述

本目录保存 M5 的**轮次场景**：每个 JSON 描述一条完整轮次——用户输入、fake Provider 的分片脚本、注册的工具，以及轮次进行中要执行的动作（取消、追加输入、等待某次尝试）。`internal/testkit` 的 `ReplayHarnessScenario` 把它回放成一条逐行 trace，因此“同一场景得到同一 trace”是可比较的文本，而不是口头承诺。

与 Cordis 和 Session 的差分不同，这里的对照对象**不是** TypeScript 实现，而是**同一个 Go 实现两次运行的一致性**：M5 的参考侧还没有可离线调用的构建产物。这一点在 [DIVERGENCES.md](DIVERGENCES.md) 中如实说明，避免把它读成跨实现差分。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- 用数据描述轮次行为：输入、脚本、工具、动作。
- 让 trace 可比较：事件顺序、请求次数、重建出的历史。
- 不判断行为是否正确；断言由 `internal/testkit/harness_scenario_test.go` 与 `agentloop` 的测试承担。

## 使用

```sh
go test -count=1 -run HarnessScenario ./internal/testkit/...
```

fixture 由 `testkit.LoadHarnessScenario` 读取并校验：缺少输入、空脚本、脚本没有 `finish` chunk、动作类型未知都会被拒绝，因此一个坏场景不会悄悄停止覆盖它声称的行为。

## 理解实现

| 场景 | 覆盖的行为 | 断言点 |
| --- | --- | --- |
| `full-turn.json` | 两次模型请求与一次工具调用 | `requests=2`；`tool/call` 先于 `tool/result`；重建历史含工具结果 |
| `queued-input.json` | 轮次开始前排队的输入由同一次 claim 取走 | `requests=1`；两条 `user/message` |
| `cancel-mid-stream.json` | 流进行中的取消 | 轮次仍然 `turn/end` 关闭 |
| `rejected-input.json` | 被拒绝的工具调用 | 仍然产生 `tool/result`，且轮次不失败 |

`actions` 里 `cancel` 与 `send` 表示“在轮次进行中”发生的动作；驱动在后台跑轮次，场景等待指定的尝试次数后再执行动作，因此不依赖计时。

## 进一步阅读

- [turn 与 step](../../../docs/agentloop/turns.md)：事件顺序的读者说明。
- [取消与关闭](../../../docs/agentloop/cancellation.md)：取消与销毁的区别。
- [对照工具](../../../internal/testkit/README.md)：Cordis 与 Session 的差分回放器，以及本目录的场景回放器。

## 验证

```sh
go test -count=1 ./internal/testkit/...
go run ./scripts/doccheck
```

后一条检查本目录的 README 章节与本地链接完整性。

## 限制

- **不是跨实现差分**：没有可离线调用的参考侧构建产物，这里对照的是同一实现两次运行的一致性；真正的跨实现对照仍只在 Cordis 与 Session 两族场景中成立。
- **不覆盖真实网络与进程**：M5 全部使用 fake Provider 与可控时钟，真实端点属 M7。
- **场景不做性能断言**：trace 只记录顺序与次数，不记录耗时。
