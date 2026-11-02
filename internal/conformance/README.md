---
description: "conformance：54 包组 + 9 vendor 的处置账本，未归属项由校验器拒绝。"
kind: "package-library"
---

# internal/conformance

## 概述

这个包记录固定基线每个源包组的处置（verified／adapted／gap），使「全覆盖」成为**机器可查**的主张：无处置的组不会被默认当作完成。它服务于 `cmd/conformance` 与 `docs/conformance/ledger.md`。

读者需要 Go 的 JSON 与 map 基础。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Inventory`／`Disposition`／`Ledger` 表达清单与处置。
- `Validate` 检查：无缺处置、无处置指向不存在的组、gap 必须带说明。
- `Summarize` 统计三种处置数量。

## 使用

下面的片段加载清单与账本并校验；校验失败会返回错误。

```go fragment
inv, _ := conformance.LoadInventory(invData)
ledger, _ := conformance.LoadLedger(dispositionsData)
err := conformance.Validate(inv, ledger)
```

## 理解实现

- **未归属即拒绝**：缺处置返回 `ErrMissingDisposition`，不会静默当作完成。
- **未知组即拒绝**：处置拼错的组名返回 `ErrUnknownGroup`，避免真缺口被笔误掩盖。
- **gap 需说明**：无 note 的 gap 返回 `ErrInvalidDisposition`。

## 进一步阅读

- [全域行为账本](../../docs/conformance/ledger.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./internal/conformance/...
```

覆盖：vendor 名归一、缺处置／未知组／gap 无 note／非法状态、统计、解析错误。

## 限制

- **以包组为分母**，不等于逐包行为等价。
