# 长输出如何缩短但仍可定位原文

## 读完要解决的问题

一个工具调用返回了 200KB 日志。整段塞进请求会撑爆上下文；随便截断又会**丢掉信息且无迹可寻**——模型看到「…」，日志里也查不到原文去哪了。更要命的是，从请求内部看，**「缩短了」与「丢了」长得完全一样**。

还有一个同样隐蔽的坑：预算判断需要一个 token 数，可这个数有两个来源——**Provider 报回的 usage（实测）**，和**本办事先算出的估算**。把估算当实测呈现，就是一个没人能为之负责的数字。

需要的 Go 知识：结构体、接口、切片。不需要 TypeScript 或 Cordis 基础。当前实现范围是 M8.5 的 token 计量、预算与 spill；**历史压缩与摘要属 M8.6，本文不声称支持**。完成实验后你能做到：**预测一段超长输出被缩短成什么、原文去哪找，并说清那个 token 数是实测还是估算。**

## 从熟悉的 Go 写法出发

普通做法是 `if len(s) > N { s = s[:N] }`。这能工作，但有三个边界：

1. **丢的部分无处可寻**。截断后原文没了，读者无从判断损失了多少。
2. **没有标注**。调用方拿到一个字符串，不知道它是原文还是被改过。
3. **token 数来历不明**。如果预算用的是某个数字，没人知道那是量出来的还是猜的。

于是引入三个概念：

- **引用（Reference）**：指向被移出请求的内容：身份、来源、原始字节数。
- **计量来源（MeterKind）**：`usage`（实测）或 `estimate`（估算）——**数字与来源一起返回**。
- **预算决策（Included）**：包含后的文本、成本、是否缩短、以及缩短时指向原文的引用。

类比之后要说清哪里不等价：这与"截断"不同。截断是有损且不可追溯的；spill **把损失变成可追溯的**——缩短形式一定配一个能找回原文的引用。

## 最小实验

准备条件：仓库根目录，Go 1.22+。不需要网络与密钥。

```go runnable=spill-budget
package main

import (
	"fmt"
	"strings"

	capspill "github.com/gosomea/deepseek-harness-go/capability/spill"
	"github.com/gosomea/deepseek-harness-go/plugins/spill"
)

func main() {
	store := capspill.NewMemoryStore()
	meter := spill.Meter{Kind: spill.MeterEstimate, TokensPerByte: 0.25}

	// A small output fits verbatim: no citation, no spill.
	small := spill.Budget{MaxTokens: 100, Meter: meter, Store: store}
	kept, err := small.Fit("small", "tool-output", []byte("short content"))
	fmt.Printf("small: shortened=%t tokens=%d err=%v\n", kept.Shortened, kept.Tokens, err)

	// A large output does not fit, so it is spilled and cited.
	big := spill.Budget{MaxTokens: 40, Meter: meter, Store: store}
	long := []byte(strings.Repeat("log line\n", 500))
	shortened, err := big.Fit("big", "tool-output", long)
	fmt.Printf("big: shortened=%t reference=%s tokens=%d err=%v\n",
		shortened.Shortened, shortened.Reference, shortened.Tokens, err)
	fmt.Println("citation present:", strings.Contains(shortened.Text, "spilled"))

	// The original comes back byte for byte through the citation.
	record, err := store.Resolve(shortened.Reference)
	fmt.Printf("original recovered verbatim: %t (%d bytes)\n", string(record.Content) == string(long), len(record.Content))

	// The token count carries its source, so an estimate is never read as measured.
	tokens, kind := meter.Count(long, nil)
	fmt.Printf("count=%d kind=%s\n", tokens, kind)

	// A store that refuses the spill must not yield content claiming retrievability.
	noStore := spill.Budget{MaxTokens: 5, Meter: meter}
	_, err = noStore.Fit("big", "tool-output", long)
	fmt.Println("without a store the budget refuses:", err != nil)
}
```

```text output=spill-budget
small: shortened=false tokens=3 err=<nil>
big: shortened=true reference=big tokens=40 err=<nil>
citation present: true
original recovered verbatim: true (4500 bytes)
count=1125 kind=estimate
without a store the budget refuses: true
```

观察重点：**缩短后 `tokens=40` 恰好压在预算上**（引用先占预算、再选前缀长度）；`reference=big` 与 `original recovered verbatim: true` 成对出现——**缩短与可追溯是同时发生的**；`kind=estimate` 明说了这个数不是量出来的。

## 先预测，再观察

**先写下你的预期**：把大输出的预算从 `MaxTokens: 40` 调到 `1`，`Fit` 会返回一个更短的文本，还是报错？再把预算调到 `1000`（仍低于原文 1125 token 估算），`Shortened` 是真是假？

观察方法：改两行后重跑。

结果：预算 `1` 时 **`Fit` 报错**（`cannot fit even the citation in 1 tokens`）——引用本身就要 13 个 token 左右，装不下引用就选不出任何前缀；预算 `1000` 时因原文需 1250 token 仍超预算，`Shortened` 仍为 `true`。恢复：还原改动即可。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | token 计量分层（usage 实测 vs 估算）；输出保留策略、spill 与可追溯引用；截断必须留下「截了什么、原文在哪」 |
| TypeScript 入口 | 固定提交 `00102833` 的 [packages/spill](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/spill) |
| Go 入口 | [spill.go](../../capability/spill/spill.go) 的 `Store`/`MemoryStore`/`Reference`；[store.go](../../plugins/spill/store.go) 的 `Meter`/`Budget`/`Fit`/`Render`；测试见 [spill_test.go](../../capability/spill/spill_test.go) 与 [store_test.go](../../plugins/spill/store_test.go) |
| 保持的行为 | 估算与实测分开标注；缩短必配引用；引用先占预算；不能 spill 就不能静默截断；原文可逐字取回 |
| Go 表达 | `Count` 返回 `(int, MeterKind)` 把来源与数字绑定；`Included.Shortened` 与 `Reference` 成对出现；不变量在 `Render` 兜底以覆盖手工拼装 |
| 不等价边界 | **不做真实 tokenizer**（估算器是公开比率的确定性实现）；**不落盘**（内容留在 store）；**不含压缩与摘要**（属 M8.6） |

## 一次操作的时序与所有权

1. `Budget.Validate` 先校验预算与计量器。
2. `Fit` 用 `Meter.Estimate` 算总成本；不超预算就原样返回 `Shortened=false`。
3. 超出时先**构造引用文本并估算其 token 成本**，用 `MaxTokens - citationTokens` 作为前缀预算。
4. 前缀预算为负 ⇒ 报 `ErrOverBudget`（连引用都装不下）；否则按比率换算可用字节数并截取。
5. 调 `store.Spill` 存全文；成功后才把引用写进 `Included`。
6. `Render` 拼接前检查：**缩短却无引用即报错**。

所有权要点：**spill store 拥有原文**，预算只产出引用。因为引用是值、内容是 store 里的字节，缩短形式无法"顺手带走"原文；也正因为如此，`Resolve` 每次回 store 取回，原文不会因为某次截断而消失。

## 失败与定位

**失败一：预算装不下引用。** 症状是 `spill: content does not fit the budget: "big" cannot fit even the citation in 1 tokens`。原因：`MaxTokens` 小于引用本身成本。诊断入口：`Fit` 的 `prefixBudget < 0` 分支。恢复：放大预算——**引用先占预算**，这是顺序问题不是 bug。

**失败二：需要 spill 却没配 store。** 症状是 `spill: content does not fit the budget: ... and no store is configured`。原因：`Budget.Store` 为空。诊断入口：`Fit` 的 `b.Store == nil` 分支。恢复：配置 store——**静默截断与"内容本来就这么短"无法区分**，所以这里必须拒绝。

**失败三：缩短却无引用。** 症状是 `spill: content does not fit the budget: "a" is shortened but cites no reference`。原因：手工拼装了 `Included`。诊断入口：`Render` 的检查。恢复：补上 `Reference`——**这正是让缩短安全的不变量**。

**失败四：引用重复。** 症状是 `spill: invalid spill: "r1" is already stored`。原因：同一引用身份 spill 了两次。诊断入口：store 的 `Spill`。恢复：换一个新引用——已出现在请求里的引用必须继续指向那次请求描述的内容。

## 回到源码与下一步

建议阅读顺序：

1. [store.go](../../plugins/spill/store.go) 的 `Meter.Estimate` 与 `Count` —— 先看数字如何与来源绑定。
2. 同文件的 `Budget.Fit` —— 再看引用先占预算的顺序。
3. 同文件的 `Render` —— 确认"无引用的缩短条目"被拒绝。
4. [spill.go](../../capability/spill/spill.go) 的 `MemoryStore.Spill`/`Resolve` —— 最后看原文如何逐字取回。

对应测试名：`TestOversizedContentIsSpilledWithACitation`、`TestShortenedContentFitsItsBudget`、`TestCitationTooLargeForTheBudgetIsRefused`、`TestRenderRefusesAShortenedEntryWithoutAReference`、`TestUsageIsUsedAsMeasured`。

读完应能回答：**机制解决什么问题**（缩短可追溯；估算不冒充实测）；**谁拥有资源**（store 拥有原文，预算只产出引用）；**Go 与 DSH 的差异在哪**（无真实 tokenizer、不落盘、不含压缩）。

可运行实验见 [examples/context](../../examples/context/README.md)。下一篇：先预测压缩后 history，再从日志重建（`docs/context/compaction.md`，属 M8.6）。
