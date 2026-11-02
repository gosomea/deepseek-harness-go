---
description: "CLI 端到端场景的确定性 fixture：拒绝先于副作用、无人应答不授权、只读拒绝命令与退出释放。"
kind: "package-library"
---

# testdata/parity/cli

## 概述

本目录保存 M7 的 **CLI 场景**：每个 JSON 描述一串步骤（播种文件、调用工具、给出审批结果）以及要观察的结果。`internal/testkit` 的 `ReplayCLIScenario` 把它回放成逐行 trace，因此"同一场景得到同一 trace"是可比较的文本。

与其他三族一样，这里的对照对象**不是** TypeScript 实现，而是**同一个 Go 实现两次运行的一致性**：本阶段的参考侧是 CLI 应用，没有可离线调用的构建产物。这一点在 [DIVERGENCES.md](DIVERGENCES.md) 中如实说明。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- 用数据描述一次命令行会话：播种、工具调用、审批结果、期望。
- 强调**顺序**而不只是结果：拒绝必须发生在副作用之前。
- 不判断行为是否正确；断言由 `internal/testkit/cli_scenario_test.go` 承担。

## 使用

```sh
go test -count=1 -run CLIScenario ./internal/testkit/...
```

fixture 由 `testkit.LoadCLIScenario` 读取并校验：缺少 id、缺少步骤、步骤类型未知、工具缺名、审批结果不在词汇表、政策未知都会被拒绝。

## 理解实现

| 场景 | 覆盖的行为 | 断言点 |
| --- | --- | --- |
| `read-write-refuse.json` | 拒绝先于副作用 | `deniedWrites=1`、`allowedWrites=1`、拒绝当下的文件内容未变、随后允许才改变 |
| `headless-no-responder.json` | 无人应答不授权 | `deniedWrites=1`、`allowedWrites=0`、文件未变 |
| `read-only-command.json` | 只读策略拒绝命令 | `commandDenied=1`、拒绝里点名需要 `workspace-write` |
| `exit-cleanup.json` | 退出后世界可重开 | `secondRunCanOpen=true` |

`outcome` 字段是声明式的四种审批结果之一（或 `none` 表示没有应答者），由回放器施加；`fileAfterDenial` 在**拒绝当下**取样，与最终状态分开，因为后续允许的写入本来就会改变文件。

## 进一步阅读

- [CLI 与会话](../../../docs/cli/session-cli.md)：一次受控修改与恢复的完整路径。
- [能力与执行世界](../../../docs/execution/capabilities.md)：策略为什么必须发生在副作用之前。
- [对照工具](../../../internal/testkit/README.md)：本目录的回放器与其他三族场景。

## 验证

```sh
go test -count=1 ./internal/testkit/...
go run ./scripts/doccheck
```

后一条检查本目录的 README 章节与本地链接完整性。

## 限制

- **不是跨实现差分**：没有可离线调用的参考侧构建产物；这里对照的是同一实现两次运行的一致性。
- **不启动真实 CLI 进程**：场景通过工具与审批 seam 回放；端到端进程行为由 `examples/cli` 演示。
- **无联网与凭据**：真实端点 smoke 单列，缺凭据时记为未验证。
- **不覆盖 Windows**：进程与路径行为只在本机（macOS）验证过。
