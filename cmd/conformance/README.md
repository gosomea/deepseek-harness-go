---
description: "conformance 命令：校验全域账本每组有处置并打印统计。"
kind: "command-tool"
---

# cmd/conformance

## 概述

这个命令读取 `source-inventory.json` 与 `dispositions.json`，校验 54 包组 + 9 vendor 每组都有处置，并打印 verified／adapted／gap 统计；任何未归属或不合规的处置都以非零退出。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

- 校验账本完整性；失败即非零退出，使「全覆盖」不可虚报。

## 使用

```sh
go run ./cmd/conformance \
  -inventory docs/10-plans/dsh-go-replication/source-inventory.json \
  -dispositions docs/10-plans/dsh-go-replication-m12/dispositions.json
```

预期输出：`conformance ledger: OK (63 groups, verified=39 adapted=16 gap=8)`。

## 验证

```sh
go test -count=1 ./cmd/conformance/...
```

（本命令的逻辑由 `internal/conformance` 的测试覆盖。）

## 限制

- **只校验账本结构**，不判断某组是否真的行为等价；那由该组的测试与评审承担。
