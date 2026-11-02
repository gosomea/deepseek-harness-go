---
description: "演示客户端投影：快照+delta 等于全快照，落后客户端回退快照 resync。"
kind: "command-example"
---

# examples/client

## 概述

这个程序用 `clientview` 展示客户端投影的两个性质：快照加增量与直接全快照得到同一视图；客户端落后到 hub 保留范围之外时回退全快照（`resynced=true`）而非展示不完整视图。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

- 演示快照+delta 与全快照 `SameView`。
- 演示重连的增量续接与快照回退。

## 使用

```sh
go run ./examples/client
```

输出逐字保存在 [expected.txt](expected.txt)，并由 `TestDemoMatchesDocumentedOutput` 比对。

## 验证

```sh
go test -count=1 ./examples/client/...
go run ./examples/client
```

两者都应退出 0；输出应与 `expected.txt` 完全一致。

## 限制

- **纯投影**。不涉及真实前端 UI 或浏览器。
