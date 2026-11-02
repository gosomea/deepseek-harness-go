# 执行管线：一次工具调用按什么顺序发生

## 读完要解决的问题

你已经读过[从 Go 理解工具](go-primer.md)，知道工具三角色为什么分开。下一个问题是：**顺序究竟保证了什么？**

如果只把“校验参数、判断策略、执行、返回结果”写在一个函数里，你能看到代码顺序，却无法断言任何事：无法证明被拒绝的调用没有执行，无法证明每条路径只产出一条结果，也无法证明一个失败的调用不会被呈现成成功。这一篇把顺序变成可观察的**阶段序列**，让这四条断言各自有对应的测试。

前置知识是 Go 的接口、`error` 与 `context.Context`。关联行为见 [M4 执行契约](../10-plans/dsh-go-replication-m04/plans.md)。固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`docs/tool-execution-pipeline.zh.md` 与 `packages/core/tools/src/index.ts`。

## 从熟悉的 Go 写法出发

普通 Go 做法是把四件事写成一个函数：

```go fragment
func RunTool(name string, raw string) (string, error) {
	body, ok := tools2[name]
	if !ok {
		return "", fmt.Errorf("unknown tool")
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", err
	}
	if !allowed(name) {
		return "", fmt.Errorf("denied")
	}
	return body(args)
}
```

它清楚、够用。边界在于**顺序只存在于阅读顺序里**。想验证“被拒绝的调用没有执行”，你得读代码；想在被拒绝时也给模型一条规范化结果，你得再加分支；而每加一个分支，“恰好一条结果”这条性质就更难确认。

参考实现把阶段显式化：每个阶段都有名字、都在决定之后通知观察者，而观察者**不能**影响结果。

## 最小实验

准备条件：仓库根目录，Go 1.22，无需密钥或网络。

```go runnable=pipeline-stages
package main

import (
	"context"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/tools"
)

func main() {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.Definition{
		Name:        "read_file",
		Description: "Read a file.",
		Schema: tools.InputSchema{
			Type:       tools.SchemaTypeObject,
			Properties: map[string]tools.PropertySchema{"path": {Type: tools.SchemaTypeString}},
			Required:   []string{"path"},
		},
		Execute: func(execution tools.Context) (tools.Result, error) {
			return tools.Result{Content: "contents"}, nil
		},
	})

	var stages []string
	pipeline := &tools.Pipeline{
		Registry: registry,
		Guards:   tools.NewGuardChain(tools.DenyTools("read_file")),
		Observer: tools.ObserverFunc(func(o tools.Observation) {
			stages = append(stages, string(o.Stage))
		}),
	}
	result, err := pipeline.Execute(context.Background(), tools.Call{ID: "c1", Name: "read_file", Arguments: `{"path":"a.txt"}`})
	fmt.Printf("outcome=%s attempts=%d error=%v\n", result.Outcome, result.Attempts, err != nil)
	fmt.Println("stages=" + fmt.Sprint(stages))
	fmt.Println("reachedExecute=" + fmt.Sprint(contains(stages, "execute")))
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
```

```text output=pipeline-stages
outcome=denied attempts=0 error=true
stages=[lookup validate pre-execute]
reachedExecute=false
```

观察重点：`stages` 里**没有** `execute`。这就是“拒绝不会到达工具体”的证据——不是错误文本声称如此，而是阶段序列里确实没有那一步。

## 先预测，再观察

先写下预期：`post-execute` 能不能把一个失败的结果改成成功？

不能。`PostExecute` 可以替换模型可见的 `Content`，但 `CallID`、`Name`、`Outcome`、`IsError`、`Attempts` 与 `Err` 会被强制还原。原因是：如果 hook 能把 `OutcomeFailed` 改写成 `OutcomeSuccess`，同一次调用就会出现“结果说成功、错误说失败”的矛盾；而 `Err` 被抹掉时，调用方会依据丢失的错误把失败当成功。`TestPostExecuteCannotRewriteTheOutcome` 固定这条规则，[examples/tools](../../examples/tools/README.md) 也能观察到 `post-execute` 在失败路径上仍然运行。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 管线阶段（准入 → 执行 → 结果规范化）；guard 顺序单调；观察者不能左右结果 |
| TypeScript 入口 | `packages/core/tools/src/index.ts`、`docs/tool-execution-pipeline.zh.md` |
| Go 入口 | [tools/pipeline.go](../../tools/pipeline.go)、[tools/guard.go](../../tools/guard.go) |
| 保持的行为 | 每条路径恰好一条 `Result`；参数错误与拒绝都发生在 `execute` 之前且 `Attempts=0`；取消在运行前被观察；`post-execute` 不能改写结果归属 |
| Go 表达 | `Pipeline` 不持有工具也不持有策略，只拥有顺序；`Observer` 在决定之后被通知，其 panic 被隔离 |
| 不等价边界 | 无审批交互、无沙箱、无按路径规则（属 M7）；`post-execute` 只有单一 hook，未实现参考的多阶段结果快照链 |

### 阶段与产出

| 阶段 | 输入 | 成功时的产出 | 失败时的产出 |
| --- | --- | --- | --- |
| `lookup` | 调用名 | 解析出的 `Definition` | 未知工具或无可执行体 → `OutcomeFailed`、`Attempts=0` |
| `validate` | 定义 schema + 原始参数串 | 解码后的参数对象 | `OutcomeInputInvalid`、`Attempts=0`、`ErrToolInputInvalid` |
| `pre-execute` | 定义 + 调用 | 放行 | `OutcomeDenied`、`Attempts=0`、`ErrToolDenied` |
| `execute` | 已检查取消的 `Context` | `OutcomeSuccess`、`Attempts=1` | 已取消 → `OutcomeCanceled`；工具体报错 → `OutcomeFailed`（`Attempts=1`） |
| `post-execute` | 上一阶段的结果 | 规范化后的内容 | 不改变结果归属 |
| `finalize` | 结果 | 标注 | 不改变结果归属 |

## 一次操作的时序与所有权

1. `Pipeline.Execute` 先校验调用本身：缺 id 或名字直接返回 `OutcomeInputInvalid`。
2. `lookup` 解析定义；未知工具在此结束，工具体不可见。
3. `validate` 用定义的 schema 校验原始参数串；非法参数不进入工具体。
4. `pre-execute` 按顺序询问 guard；第一个拒绝胜出，后续 guard 不再被询问。这是副作用之前的最后一道门。
5. `execute` 先检查 `ctx.Err()`，再运行工具体。取消在此被观察到，因此取消不会启动工作。
6. `post-execute` 在工具体运行过的每条路径上运行，只允许替换模型可见内容。
7. `finalize` 只做标注。

所有权：`Registry` 拥有定义并保证并发安全；`Pipeline` 只拥有顺序；调用方拥有观察者注入与 result 的后续处理（写日志属 M6）。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| `tools: tool input invalid: …` 且 `attempts=0` | 参数不满足 schema | `TestPipelineNeverReachesTheBodyForRejectionsAndBadArguments` | 修正参数；不要为了让请求通过而放宽 schema |
| `tools: tool denied by policy` | guard 拒绝 | 同上；看阶段序列里没有 `execute` | 调整策略或换工具；不要让后加入的 guard“纠正”先前拒绝 |
| `tools: tool not found` | 名字不在该作用域 | `TestPipelineRefusesUnknownToolAndMissingRegistry` | 检查注册与 scope 可见性 |
| 结果说成功但错误非 nil | 自定义 `PostExecute` 抹掉了 `Err` | `TestPostExecuteCannotRewriteTheOutcome` | 实现已强制还原 `Err`；不要在自定义管线里绕过 |

## 回到源码与下一步

阅读顺序：`tools/pipeline.go` 的 `Execute` → `emit` → `tools/guard.go` 的 `Check`。对应测试：`tools/pipeline_test.go`、`tools/tools_coverage_test.go`、`tools/schema_test.go`。

读者应能回答：机制解决什么问题（把顺序变成可断言的事实）、谁拥有资源（Registry 拥有定义，调用方拥有观察者）、Go 与 DSH 的差异在哪里（无审批与沙箱、结果快照链未实现）。

下一步：[提示词装配](../systemprompt/assembly.md)——工具 schema 怎样与提示词片段一起装配成模型可见的请求。
