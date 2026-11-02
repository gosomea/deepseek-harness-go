---
description: "compaction seam：历史的一段被缩短形式替换，工具调用与结果必须同进同出。"
kind: "package-library"
---

# capability/compaction

## 概述

这个包定义 **compaction seam**：历史的一段被替换成更短的形式，且替换结果可回报。

替换历史有两种**看起来像成功**的失败。第一种：**边界落在一次工具调用与它的结果之间**，留下一个答案已消失的调用。第二种：**摘要失败了，调用方却被告知替换完成**。两者在这里都被写成规则而非约定，因此 Provider 无法在满足接口的同时破坏它们。

**一次工具调用与它的结果被视为不可分割的一对**：任何会拆开它们的边界都在写入之前被拒绝。

读者需要 Go 的结构体与切片基础。三阶段流程与 fake 摘要 Provider 见 [plugins/compaction](../../plugins/compaction/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Entry` 是历史的一项：种类（message／tool-call／tool-result／summary）、文本与 `CallID`。
- `Plan` 描述被替换的区间；`Validate` 检查区间是否落在历史内且摘要非空。
- `CheckPairing` 检查区间的**两个边界**是否会拆开一对调用与结果。
- `Apply` 先查配对规则再替换；被拒的计划**什么都不改**。
- `LegalBands` 列出不会拆对的最大区间，让调用方不会误选非法区间。

## 使用

先看合法区间，再提交计划。下面的片段假定错误会被检查；`Apply` 会在区间非法或**边界拆开一对调用与结果**时失败，且被拒时不改动任何条目。

```go fragment
plan := compaction.Plan{Start: 1, End: 3, Summary: "the file was read"}
result, err := compaction.Apply(entries, plan, "sum-1")   // 被拒时 result.Applied == false
bands := compaction.LegalBands(entries)                   // 合法区间先看再用
```

## 理解实现

- **配对规则先于任何写入**：`Apply` 先 `CheckPairing`，被拒时返回原文且 `Applied=false`。部分替换正是这条规则要防止的悬空状态。
- **两个边界都查**：起点边界（`Start-1` 与 `Start`）与终点边界（`End-1` 与 `End`）。
- **区间用索引而非 id**：避免调用方命名出跳过某项的区间，也让边界检查能对着相邻项做。
- **`Result.Applied` 是显式字段**：拿到错误的调用方不得把条目当作已替换，拿到 `false` 的也不得报告替换。
- **返回的切片是副本**：调用方改自己那份不会污染已提交的历史。

## 进一步阅读

- [先预测压缩后 history，再从日志重建](../../docs/context/compaction.md)：三阶段流程与摘要失败的处置。

## 验证

```sh
go test -race -count=1 -timeout=240s ./capability/compaction/...
```

覆盖：条目校验、整对区间替换、两个边界拆对被拒、被拒时零改动、区间校验、`LegalBands` 不拆对、无对历史与副本隔离。

## 限制

- **不产出摘要**。摘要来自 `SummaryProvider`，属 [plugins/compaction](../../plugins/compaction/README.md)。
- **不含 token 预算**。何时该压缩、压多少归 M8.5 与调用方。
- **不写 Session 日志**。替换结果如何落盘属后续阶段。
