# 从 Go 理解工具：为什么“能调用”不等于“该调用”

## 读完要解决的问题

你已经写过一个函数，也从接口的角度理解过“同一契约多种实现”。现在的问题是：模型请求“读一下 a.txt”时，从这句话到一个真实副作用之间，应该发生什么？

最直接的想法是把工具体直接交给调用方：模型给出名字和参数，你 `json.Unmarshal` 之后调用对应函数。它在演示时够用。它会崩在四处：参数可能是**模型给错的**（类型不对、缺必填字段）；有些调用**不该执行**（策略不允许）；被拒绝的调用如果什么都不返回，模型不知道发生了什么；一次调用如果产出两条结果或零条结果，日志就对不上。

这一篇从这个问题进入 DSH 的 **工具三角色**：定义（definition）、提供方（provider）与执行管线（pipeline）。前置知识是 Go 的接口、闭包与 `error`。关联行为见 [M4 执行契约](../10-plans/dsh-go-replication-m04/plans.md)；当前实现范围是注册、schema、准入与执行顺序，真实文件系统、沙箱与审批属 M7。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/core/tools/src/index.ts`、`packages/core/tools/src/schema.ts` 与 `docs/tool-execution-pipeline.zh.md`。

## 从熟悉的 Go 写法出发

最直白的做法是一个函数表：

```go fragment
var tools = map[string]func(args map[string]any) (string, error){
	"read_file": readFile,
}
```

它清楚、好调用。边界在于**这张表同时承担了四个职责**：描述工具（给模型看）、校验参数、准入判断、执行。于是任何一项要变，都要动同一处；而且没有任何地方能保证“被拒绝的调用没有执行”——因为拒绝逻辑和执行逻辑在同一段代码里。

第二个边界是 `map[string]any`。参数的形状被推迟到运行时，模型给一个字符串而你期望整数时，你只能在工具体内部处理，而那时副作用已经在眼前。

## 最小实验

准备条件：仓库根目录，Go 1.22，不需要密钥或网络。

```sh
go run ./examples/tools
```

[examples/tools](../../examples/tools/README.md) 注册一个 `read_file`，然后执行四次调用：一次合法、一次缺必填参数、一次工具名不存在、一次参数类型不对。完整输出由 [expected.txt](../../examples/tools/expected.txt) 保存，并由该示例的测试逐字比对。

观察重点：三次失败的调用都打印 `attempts=0`，而且观察事件里**没有** `stage=execute`。这不是“错误信息里写着没执行”，而是事件流里确实没有那一阶段。

## 先预测，再观察

先写下预期：如果先注册一个拒绝 `read_file` 的 guard，再追加一个**允许一切**的 guard，调用会被放行吗？

答案是不放行。guard 链是**单调**的：它在遇到第一个拒绝时就停止，后续 guard 根本不会被询问。可以用下面的程序验证。

```go runnable=tools-monotonic
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
			return tools.Result{Content: "should never run"}, nil
		},
	})

	chain := tools.NewGuardChain(tools.DenyTools("read_file"))
	asked := false
	chain.Append(tools.GuardFunc(func(definition tools.Definition, call tools.Call) (tools.GuardDecision, string) {
		asked = true
		return tools.GuardAllow, ""
	}))

	pipeline := &tools.Pipeline{Registry: registry, Guards: chain}
	result, err := pipeline.Execute(context.Background(), tools.Call{ID: "c1", Name: "read_file", Arguments: `{"path":"a.txt"}`})
	fmt.Printf("outcome=%s attempts=%d denied=%v laterGuardAsked=%v\n", result.Outcome, result.Attempts, err != nil, asked)
}
```

```text output=tools-monotonic
outcome=denied attempts=0 denied=true laterGuardAsked=false
```

`attempts=0` 说明工具体没跑；`laterGuardAsked=false` 说明后来的放行 guard **从未被询问**——单调性是可观察的，不是文档承诺。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 工具定义（名称+schema）、提供方（工具体）、执行管线（准入 → 执行 → 结果规范化）；guard 顺序单调 |
| TypeScript 入口 | `packages/core/tools/src/index.ts`（注册与执行）、`schema.ts`（参数约束） |
| Go 入口 | [tools/types.go](../../tools/types.go)、[tools/schema.go](../../tools/schema.go)、[tools/guard.go](../../tools/guard.go)、[tools/pipeline.go](../../tools/pipeline.go) |
| 保持的行为 | 参数错误与拒绝都在进入工具体之前发生；每条路径恰好一条规范化结果；被拒绝的调用 `Attempts=0`；`post-execute` 可改内容但不可改结果归属 |
| Go 表达 | `Executor` 只接收一个 `Context`（含调用 id、原始串与解码值），避免同一事实两处读取；`Guard` 是小接口，用 `GuardFunc` 适配闭包；`Pipeline` 不持有工具也不持有策略 |
| 不等价边界 | schema 是受约束子集（无 `oneOf`/`$ref`/`pattern`），未知 `type` 报错而非忽略；没有审批交互与沙箱；工具体是测试替身，不产生真实副作用 |

## 一次操作的时序与所有权

管线六个阶段，顺序固定：

1. **lookup**：按名字解析定义；未知工具在此失败，`Attempts=0`。
2. **validate**：用定义的 schema 校验**原始参数串**；失败在此结束，工具体不可见非法参数。
3. **pre-execute**：guard 链按顺序判断；第一个拒绝胜出，后续 guard 不再询问。这是**副作用之前的最后一道门**。
4. **execute**：检查 context 是否已取消，然后运行工具体。取消在此被观察到，因此取消不会启动工作。
5. **post-execute**：无论成功失败都会运行，可替换模型可见的 `Content`。
6. **finalize**：只做标注，不能改变结果。

所有权：`Registry` 拥有定义（并发安全）；`Pipeline` 拥有顺序但**不拥有**工具与策略；观察者由注入方拥有，且其 panic 被隔离——一个 tracing 插件不应能让执行失败。

## 失败与定位

| 症状 | 原因 | 诊断入口 | 恢复 |
| --- | --- | --- | --- |
| `tools: tool input invalid: …` 且 `attempts=0` | 参数不满足 schema | `TestPipelineNeverReachesTheBodyForRejectionsAndBadArguments` | 修正模型给的参数；不要放宽 schema 来迁就 |
| `tools: tool denied by policy` | guard 拒绝 | 同上；诊断时看观察事件里没有 `stage=execute` | 调整策略或换工具；不要让后加入的 guard 去“纠正” |
| `tools: tool not found` | 工具名不在该作用域 | `TestPipelineRefusesUnknownToolAndMissingRegistry` | 检查注册与作用域可见性 |
| 失败却报成功 | `post-execute` 抹掉了 `Err` | `TestPostExecuteCannotRewriteTheOutcome` | 实现已强制还原 `Err`；自定义管线时不要绕过 |

## 回到源码与下一步

阅读顺序：`tools/types.go` 的 `Definition`/`Result` → `tools/schema.go` 的 `ValidateArguments` → `tools/guard.go` 的 `GuardChain.Check` → `tools/pipeline.go` 的 `Execute`。对应测试：`tools/pipeline_test.go`、`tools/tools_coverage_test.go`。

读者应能回答：机制解决什么问题（把“能调用”和“该调用”分开，并在副作用前收口）、谁拥有资源（Registry 拥有定义，调用方拥有观察者）、Go 与 DSH 的差异在哪里（schema 子集、无审批与沙箱）。

下一步：[执行管线参考](execution-pipeline.md)——每个阶段的输入输出、拒绝路径与结果唯一性。
