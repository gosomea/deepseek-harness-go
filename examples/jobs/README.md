---
description: "在可控时钟上驱动持久 job，展示触发、结算与 unknown 状态不重放。"
kind: "command-example"
---

# examples/jobs

## 概述

这个程序用可控时钟驱动一个固定间隔调度器，展示一个 job 被触发、认领并结算，并展示结算丢失的 job 被记为 unknown 而不再被认领。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

- 演示固定间隔 entry 到期触发一次、同一 tick 不重复。
- 演示 job 的 claim／settle 状态转移。
- 演示结算丢失的 job 被记为 unknown 且不可再 claim。

## 使用

```sh
go run ./examples/jobs
```

输出逐字保存在 [expected.txt](expected.txt)，并由 `TestDemoMatchesDocumentedOutput` 比对。

## 验证

```sh
go test -count=1 ./examples/jobs/...
go run ./examples/jobs
```

两者都应退出 0；输出应与 `expected.txt` 完全一致。

## 限制

- **内存实现**。不落盘、不接真实引擎或真实时钟。
- **fake 夹具**。示例用进程内 stub，不产生真实外部副作用。
