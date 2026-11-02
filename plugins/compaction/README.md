---
description: "三阶段压缩：start／summary／end，摘要失败绝不报告替换完成。"
kind: "package-library"
---

# plugins/compaction

## 概述

这个包用三段可观察的阶段替换历史的一段：**start → summary → end**，并且**绝不报告没有发生的替换**。

之所以要分成三段，是因为摘要本身是一次模型调用，而模型调用会失败。危险的形状是**部分完成**：区间被标记为已替换，可摘要从没到；或者摘要写下了，替换却失败了。两种都会留下自相矛盾的历史。

因此中间那段可以失败而**不冒充成功**，调用方总能从返回结果判断替换是否完成。

读者需要 Go 的接口与错误基础。配对规则与区间合法性见 [capability/compaction](../../capability/compaction/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `SummaryProvider` 产出区间摘要；`FuncProvider` 与 `FailingProvider` 是两个现成实现，`Route` 是接入 `llm.Provider` 的真实路由。
- `Compactor.Compact` 按 start → summary → end 推进，失败时**不报 end**。
- `Compactor.Steps`／`Phases` 回报已发生的阶段，供调用方观察而非推测。
- `Project` 构建请求可见的 surface，并记录**每个摘要替代了哪些条目**。
- `Summaries` 列出历史中的摘要条目。
- `Route`（`route.go`）把区间渲染成一个 `llm.Request` 交给 Provider，回收为一段文本；它拒绝超预算区间（`ErrRouteBudget`）、取消时不产生摘要（`ErrSummaryProviderFailed`），并暴露 `ProviderID` 让路由切换可见。

## 使用

给压缩器一个摘要 Provider，再提交计划；三段阶段可事后查看。下面的片段省略了错误检查；摘要失败时 `Compact` 返回错误且 `Applied=false`。

```go fragment
compactor := compaction.NewCompactor(compaction.FuncProvider(
	func(region []compaction.Entry) (string, error) { return "summary text", nil },
))
result, err := compactor.Compact(entries, compaction.Plan{Start: 1, End: 3, Summary: "ignored"})
phases := compactor.Phases()   // start, summary, end —— 失败时不含 end
surface := compaction.Project(result.Entries, map[string][]string{result.SummaryID: {"c1", "r1"}})
```

## 理解实现

- **end 只在替换真正完成时出现**：摘要失败只报 start 与 summary，原文原样返回。
- **空摘要也算失败**：`ErrSummaryProviderFailed`，因为空摘要不是替换。
- **配对规则在报任何阶段之前检查**：被拒的计划不会看起来像"已开始压缩"。
- **摘要 id 逐次递增**：两次压缩不产生同一个 id，否则引用会有歧义。
- **`Project` 记录替代来源**：摘要替代了哪些条目无法从缩短后的历史反推，所以必须记录。
- **替换文本用 Provider 的输出**，不用 `Plan.Summary`——后者只是请求，不是结果。

## 进一步阅读

- [先预测压缩后 history，再从日志重建](../../docs/context/compaction.md)：三阶段与表面替换如何配合。

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/compaction/...
go test -race -count=1 -timeout=240s -run Pair|Boundary|Summary|Surface ./plugins/compaction/...
```

覆盖：三阶段顺序、摘要失败不报 end 且零改动、空摘要视为失败、拆对被拒且不报任何阶段、nil／无 Provider／非法区间拒绝、surface 记录与副本隔离、摘要排序，以及两次压缩的 id 不重复。

## 限制

- **不落盘**。替换结果如何持久化属后续阶段。
- **真实模型路由的联网 smoke 单列**。`Route` 已接入 `llm.Provider`，但真实端点需要凭据；缺凭据时 smoke 记为未验证，不是通过。
- **不含预算决策**。何时触发压缩由调用方与 M8.5 预算决定；`Route` 只拒绝超过自身 `MaxRegionTokens` 的区间。
