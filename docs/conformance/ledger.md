# 全域行为账本

本文件是对固定基线 54 个包组 + 9 个 vendor 包的处置账本，由 `internal/conformance` 与 `cmd/conformance` **机器校验**：每组必须有 `verified`／`adapted`／`gap` 三种处置之一，未归属项被校验器拒绝。

## 怎样读

- **verified**：行为等价，有测试与证据。
- **adapted**：以 Go seam／fake 适配实现，有显式差异记录。
- **gap**：本环境未交付，属明确缺口（未验证，不声称兼容）。

## 机器校验

```sh
go run ./cmd/conformance -inventory docs/10-plans/dsh-go-replication/source-inventory.json -dispositions docs/10-plans/dsh-go-replication-m12/dispositions.json
```

预期输出：`conformance ledger: OK (63 groups, verified=30 adapted=25 gap=8)`。校验器拒绝：缺处置、处置指向不存在的组、gap 无说明。

## 统计（当前）

| 处置 | 数量 |
| --- | --- |
| verified | 30 |
| adapted | 25 |
| gap | 8 |

8 个 gap 组：`browser-use`、`computer-use`、`document`、`experimental`、`ptc-runtime`、`session-query`、`typert`、`webhook`。它们本环境未交付，详见 [兼容缺口](../compatibility/gaps.md) 与 [发行缺口](../release/gaps.md)。

## 判定口径

`verified` 只给**确有 Go 包且有测试覆盖所交付子集**的组；仅 seam／fake 实现或无对应 Go 包的组一律记 `adapted`（并写明差异），未实现者记 `gap`。此口径避免了「名字在名单里就算验证」的诚实风险，因此 verified 数量较宽松判定更少（30 而非 39）。

## 边界

账本以**包组**为分母，不等于逐包行为等价。组内部分包仍未逐项审计，M12 数字不能当作「完整复刻」的证据；见 [范围说明](scope.md)。
