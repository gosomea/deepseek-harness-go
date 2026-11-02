# 先预测压缩后 history，再从日志重建

## 读完要解决的问题

一个会话跑了几十轮，历史长得放不下。要压缩，就得用一段摘要替换掉一部分历史。替换有两个**看起来像成功**的失败：

1. **边界落在一次工具调用与它的结果之间**——模型看到「我调用了 read_file」，可结果不见了。这个调用永远等不到答案。
2. **摘要失败了，调用方却以为替换完成**——历史里标记着「已压缩」，可摘要从没写进去。

两者都比"压缩失败"更糟：**失败会报错，而这两种不会**。本包要解决的就是让这两种失败不可能被误认为成功。

需要的 Go 知识：结构体、切片、错误。不需要 TypeScript 或 Cordis 基础。当前实现范围是 M8.6 的配对规则与三段流程；**真实摘要路由属 M8.7，本文不声称支持**。完成实验后你能做到：**预测一个边界会不会被拒，并从阶段序列判断替换到底完成了没有。**

## 从熟悉的 Go 写法出发

普通做法是算一个切点，然后 `history = append(history[:i], summary, history[j:]...)`。这能工作，但有三个边界：

1. **切点可以落在任意位置**。落在调用与结果之间时，代码不会报错——它只是留下一个悬空调用。
2. **没有阶段可观察**。摘要调用的成功与失败，从最终的历史里看不出来。
3. **替代来源丢失**。压缩完只剩摘要文本，答不出"它替代了哪几条"。

于是引入三个概念：

- **配对（pair）**：一次工具调用与它的结果靠 `CallID` 关联，被视为**不可分割**。
- **边界（boundary）**：区间的两端。起点边界与终点边界都要检查，任何一个拆对就拒。
- **阶段（Phase）**：`start` → `summary` → `end`；**`end` 只在替换真正完成时出现**。

类比之后要说清哪里不等价：这与"切片拼接"不同。拼接只看索引合法；这里还要求**语义完整**——一对调用与结果不能分开。

## 最小实验

准备条件：仓库根目录，Go 1.22+。不需要网络与密钥。

```go runnable=compaction-phases
package main

import (
	"fmt"

	"github.com/gosomea/deepseek-harness-go/capability/compaction"
	plug "github.com/gosomea/deepseek-harness-go/plugins/compaction"
)

func history() []compaction.Entry {
	return []compaction.Entry{
		{ID: "m1", Kind: compaction.KindMessage, Text: "please read the file"},
		{ID: "c1", Kind: compaction.KindToolCall, Text: "read_file", CallID: "call-1"},
		{ID: "r1", Kind: compaction.KindToolResult, Text: "file contents", CallID: "call-1"},
		{ID: "m2", Kind: compaction.KindMessage, Text: "thanks"},
	}
}

func main() {
	// A legal region covers the call and its result together.
	ok := plug.NewCompactor(plug.FuncProvider(func(region []compaction.Entry) (string, error) {
		return "the file was read", nil
	}))
	result, err := ok.Compact(history(), compaction.Plan{Start: 1, End: 3, Summary: "ignored"})
	fmt.Println("legal region: applied=", result.Applied, "phases=", ok.Phases(), "err=", err)

	// A boundary between the call and its result is refused before any phase.
	splitting := plug.NewCompactor(plug.FuncProvider(func(region []compaction.Entry) (string, error) {
		return "unused", nil
	}))
	_, err = splitting.Compact(history(), compaction.Plan{Start: 1, End: 2, Summary: "x"})
	fmt.Println("split boundary refused:", err != nil, "phases=", splitting.Phases())

	// A failed summary reports no end and changes nothing.
	failing := plug.NewCompactor(plug.FailingProvider{Reason: "route down"})
	before := len(history())
	failed, err := failing.Compact(history(), compaction.Plan{Start: 1, End: 3, Summary: "x"})
	fmt.Println("failed summary: applied=", failed.Applied, "phases=", failing.Phases(), "err=", err != nil)
	fmt.Println("entries unchanged:", len(failed.Entries) == before)

	// Legal bands show which regions are legal before a caller picks one.
	for _, b := range compaction.LegalBands(history()) {
		fmt.Printf("band [%d,%d) pairs=%d\n", b.Start, b.End, b.Pairs)
	}
}
```

```text output=compaction-phases
legal region: applied= true phases= [start summary end] err= <nil>
split boundary refused: true phases= []
failed summary: applied= false phases= [start summary] err= true
entries unchanged: true
band [0,1) pairs=0
band [1,3) pairs=1
band [3,4) pairs=0
```

观察重点：**`split boundary refused: true` 时 `phases= []`**——被拒的计划连 start 都没报，不会看起来像"已开始压缩"；**`failed summary` 时 `phases= [start summary]`**——没有 `end`，且 `applied=false`、条目数不变。三段区间把一对调用与结果完整包在 `[1,3)` 里，两侧各剩一段。

## 先预测，再观察

**先写下你的预期**：把失败摘要那次改成"Provider 返回空字符串"（而不是报错），`phases` 会是什么？`applied` 呢？再把合法区间的 `End` 从 3 改成 4，会怎样？

观察方法：改两行后重跑。

结果：空摘要**同样算失败**——`phases= [start summary]`、`applied=false`，因为**空摘要不是替换**；把 `End` 改成 4 后区间变成 `[1,4)`，终点边界落在 `r1` 与 `m2` 之间（两者 `CallID` 不同），**不拆对，所以合法**，会正常替换两条。恢复：还原改动即可。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 压缩不拆坏工具对；摘要失败不伪造成功；表面替换按固定源码核对 |
| TypeScript 入口 | 固定提交 `00102833` 的 [packages/compaction](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/compaction)；子系统说明见 [docs/subsystems/compaction.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/subsystems/compaction.zh.md) |
| Go 入口 | [compaction.go](../../capability/compaction/compaction.go) 的 `CheckPairing`/`Apply`/`LegalBands`；[compact.go](../../plugins/compaction/compact.go) 的 `Compactor`/`Phase`/`Project`；测试见 [compaction_test.go](../../capability/compaction/compaction_test.go) 与 [compact_test.go](../../plugins/compaction/compact_test.go) |
| 保持的行为 | 一对调用与结果同进同出；被拒计划零改动；摘要失败不报 end；`Applied` 显式；替代来源被记录 |
| Go 表达 | `CheckPairing` 独立导出以便区间预检；`Result.Applied` 让"是否替换"成为可读字段而非靠推断；`Project` 返回副本 |
| 不等价边界 | **不做真实摘要路由**（属 M8.7）；**不落盘**；**不含预算决策**（何时压缩归调用方与 M8.5） |

## 一次操作的时序与所有权

1. `Compact` 先校验全部条目与计划（区间在界内、摘要非空）。
2. 再做 `CheckPairing`——**在报告任何阶段之前**。被拒时原文副本返回、`Applied=false`，`Phases()` 为空。
3. 报 `start`。
4. 调 `provider.Summarize(region)`。失败或返回空文本 ⇒ 报 `summary` 并**停止**，不报 `end`。
5. 成功则递增生成 `summary-N`，调 `Apply` 替换。
6. 替换成功才报 `end`；`Apply` 意外失败则**不报 end**。

所有权要点：**`Compactor` 拥有阶段序列，`Apply` 产出新历史，Provider 拥有摘要的生成**。因为阶段序列只在压缩器里累积，调用方能事后核对；也因为替换文本取自 Provider 输出而非 `Plan.Summary`，请求文本与"计划"不会被混淆。

## 失败与定位

**失败一：边界拆开一对。** 症状是 `compaction: boundary splits a tool call from its result: the start boundary falls between "c1" and "r1" (call "call-1")`。原因：区间起点落在调用与结果之间。诊断入口：`CheckPairing`（起点与终点边界各报一条）。恢复：改区间覆盖整对——**部分替换正是悬空调用状态**。

**失败二：摘要失败。** 症状是 `compaction: summary provider failed: route down`，且 `Phases()` 只有 `start summary`。原因：Provider 报错。诊断入口：`Compact` 的 `Summarize` 分支。恢复：修好路由后重试——**没有任何东西被替换**。

**失败三：空摘要。** 症状同上但原因是 `the provider returned an empty summary`。原因：Provider 没报错却返回空文本。诊断入口：`strings.TrimSpace(summary) == ""` 判断。恢复：检查 Provider——**空摘要不是替换**。

**失败四：摘要替代来源不可见。** 不是报错，而是**难查**：压缩后的历史答不出"这条摘要替代了哪几条"。诊断入口：`Project` 的 `Citations`。恢复：从替换时记录——缩短后的历史**无法反推**，所以必须在压缩时记下来。

## 回到源码与下一步

建议阅读顺序：

1. [compaction.go](../../capability/compaction/compaction.go) 的 `CheckPairing` —— 先看配对规则与两个边界。
2. 同文件的 `Apply` —— 再看"被拒时零改动"的落点。
3. [compact.go](../../plugins/compaction/compact.go) 的 `Compact` —— 确认三段顺序与失败时不含 `end`。
4. 同文件的 `Project` —— 最后看替代来源如何被记录。

对应测试名：`TestFailedSummaryNeverReportsEnd`、`TestPairSplitIsRefusedBeforeAnyPhase`、`TestBoundarySplittingAPairIsRefused`、`TestEmptySummaryIsAFailure`、`TestSurfaceRecordsWhatASummaryReplaced`。

读完应能回答：**机制解决什么问题**（两种"像成功的失败"都不可能被误认）；**谁拥有资源**（Compactor 拥有阶段序列，Apply 产出新历史，Provider 产出摘要）；**Go 与 DSH 的差异在哪**（无真实摘要路由、不落盘、不含预算）。

可运行实验见 [examples/context](../../examples/context/README.md)。**C12 五篇到此交付完毕**（contributions、settings-scope、skills、attachments、budgets、compaction）。
