---
description: "M8 上下文场景的确定性 fixture：超长输出 spill、压缩可重放、摘要失败不伪造、路由切换可见。"
kind: "package-library"
---

# testdata/parity/context

## 概述

本目录保存 M8 的**上下文场景**：每个 JSON 描述一种进入模型的内容如何被预算、缩短、替换或拒绝，以及要观察的结果。`internal/testkit` 的 `ReplayContextScenario` 把它回放成逐行 trace，因此「同一场景得到同一 trace」是可比较的文本。

与其他几族一样，这里的对照对象**不是** TypeScript 实现，而是**同一个 Go 实现两次运行的一致性**：本阶段的参考侧是 `packages/context`、`spill`、`compaction`，没有可离线调用的构建产物。这一点在 [DIVERGENCES.md](DIVERGENCES.md) 中如实说明。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- 用数据描述一次上下文处理：预算、内容、历史、替换区间、期望。
- 强调**不丢信息**而不只是结果：缩短必须留下可定位原文的引用。
- 不判断行为是否正确；断言由 `internal/testkit/context_scenario_test.go` 承担。

## 使用

```sh
go test -count=1 -run ContextScenario ./internal/testkit/...
```

fixture 由 `testkit.LoadContextScenario` 读取并校验：缺少 id、未知 kind、spill 缺预算或内容、compaction 缺条目或区间、route-switch 缺两条路由都会被拒绝。

## 理解实现

| 场景 | 覆盖的行为 | 断言点 |
| --- | --- | --- |
| `long-output-spill` | 超长工具输出超出预算时被缩短 | `shortened=true` 且 `hasReference=true`，且能由引用还原原文 |
| `compaction-replay` | 一段历史被摘要替换 | `applied=true` 且 `pairingPreserved=true` |
| `summary-failure` | 摘要调用失败 | `sawEnd=false`、`applied=false`、原文不变 |
| `route-switch` | 切换摘要路由 | `routeChanged=true`，Provider 身份不同 |

## 进一步阅读

- [上下文贡献可重建](../../../docs/context/contributions.md)
- [预算与 spill](../../../docs/context/budgets.md)
- [压缩与表面替换](../../../docs/context/compaction.md)

## 验证

```sh
go test -race -count=1 -timeout=240s -run ContextScenario ./internal/testkit/...
```

覆盖：四个 fixture 的解析、命名行为齐全、两次回放逐行一致、各自期望成立。

## 限制

- **不落盘**。替换结果如何持久化属 M6。
- **不做真实摘要路由**。真实模型 smoke 缺凭据时记为未验证，不是通过。
- **token 计量是估算**。与实测的差别见 [DIVERGENCES.md](DIVERGENCES.md) D2。
