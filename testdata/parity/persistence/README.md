---
description: "持久化场景的确定性 fixture：单写者、重开一致、撕裂尾部与未来版本四类行为被写成数据并可回放。"
kind: "package-library"
---

# testdata/parity/persistence

## 概述

本目录保存 M6 的**持久化场景**：每个 JSON 描述一个会话的初始事件、要施加的损坏，以及期望的观察结果。`internal/testkit` 的 `ReplayPersistenceScenario` 把它回放成逐行 trace，因此「同一场景得到同一 trace」是可比较的文本。

与 Cordis、Session 和 Agent 轮次三族一样，这里的对照对象**不是** TypeScript 实现，而是**同一个 Go 实现两次运行的一致性**：M6 的参考侧没有可离线调用的构建产物。这一点在 [DIVERGENCES.md](DIVERGENCES.md) 中如实说明。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- 用数据描述持久化行为：初始事件、损坏类型、期望结果。
- 让结论可比较：事件数、可修复性、恢复状态、错误类别。
- 不判断行为是否正确；断言由 `internal/testkit/persistence_scenario_test.go` 承担。

## 使用

```sh
go test -count=1 -run PersistenceScenario ./internal/testkit/...
```

fixture 由 `testkit.LoadPersistenceScenario` 读取并校验：缺少 id、缺少事件、损坏类型未知都会被拒绝，因此一个坏场景不会悄悄停止覆盖它声称的行为。

## 理解实现

| 场景 | 覆盖的行为 | 断言点 |
| --- | --- | --- |
| `reopen-consistency.json` | 落盘后用新实例重开 | 事件数不变、`openTurn=true`、`unsettled=["call-1"]`、重建历史含 user 消息 |
| `second-writer-refused.json` | 单写者 | 第二个写者被拒、只读句柄仍可用、关闭后释放 |
| `truncated-tail.json` | 撕裂尾部 | `repairable=true`、修复后 `complete` 不变、前缀逐字节相同 |
| `future-version.json` | 未知版本 | 打开被拒、错误类别为 unsupported-version、文件未改动 |

场景里的 `damage` 字段是**声明式**的：它说「在末尾追加这些字节」或「把版本改成 99」，由回放器施加到临时副本上，fixture 本身不含二进制内容。

## 进一步阅读

- [恢复与 checkpoint](../../../docs/persistence/recovery.md)：重开之后哪些工作会继续。
- [格式与损坏](../../../docs/persistence/format.md)：记录边界、版本与两类损坏。
- [对照工具](../../../internal/testkit/README.md)：本目录的回放器与其他三族场景。

## 验证

```sh
go test -count=1 ./internal/testkit/...
go run ./scripts/doccheck
```

后一条检查本目录的 README 章节与本地链接完整性。

## 限制

- **不是跨实现差分**：没有可离线调用的参考侧构建产物；这里对照的是同一实现两次运行的一致性。
- **不注入真实断电**：崩溃用「临时副本 + 声明式损坏」模拟，不声称覆盖所有平台的中断电。
- **不测性能**：trace 只记录事件数、可修复性与错误类别。
