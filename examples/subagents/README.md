---
description: "通过进程内 Provider spawn 与 fork 子 Agent，展示谱系与继承切点。"
kind: "command-example"
---

# examples/subagents

## 概述

这个程序用进程内 subagent Provider 创建两个子 Agent（一个 spawn、一个 fork），读出并打印它们的谱系，证明 fork 记录的是继承切点而非复制父会话。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

- 演示 spawn 与 fork 的谱系记录。
- 演示 fork 的继承切点（inherit）取自 spec，而非父会话复制。
- 演示 Provider 的默认路由与子数量。

## 使用

```sh
go run ./examples/subagents
```

输出逐字保存在 [expected.txt](expected.txt)，并由 `TestDemoMatchesDocumentedOutput` 比对。

## 验证

```sh
go test -count=1 ./examples/subagents/...
go run ./examples/subagents
```

两者都应退出 0；输出应与 `expected.txt` 完全一致。

## 限制

- **内存实现**。不落盘、不接真实引擎或真实时钟。
- **fake 夹具**。示例用进程内 stub，不产生真实外部副作用。
