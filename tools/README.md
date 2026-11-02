---
description: "注册工具定义、校验参数与准入策略，让副作用在明确顺序下发生。"
kind: "package-library"
---

# tools

## 概述

这个包回答三个问题：工具是什么（名称、说明、参数 schema）、谁可以调用它（准入策略），以及一次调用从模型请求到规范化结果之间**按什么顺序**发生。它不执行本机操作，也不认识任何具体 Provider：工具体由别处的能力接口提供，这个包只管住“工具体到底有没有被调用”。

读者需要 Go 的结构体、接口与 error 包装基础。当前实现范围是 M4.4 的注册、schema、准入与执行管线；审批、沙箱与真实文件系统属 M7。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Registry` 持有某个作用域可见的工具定义，保证并发安全，并让“哪个工具被调用”可追溯。
- `InputSchema` 描述可被强制执行的参数约束；`ValidateArguments` 负责真正校验，模型产的原始 JSON 从不进入工具体。
- `GuardChain` 按顺序做准入判断，且**单调**：后加入的 guard 只能让准入更难，不能推翻先前的拒绝。
- `Pipeline` 按六个阶段执行：`lookup → validate → pre-execute → execute → post-execute → finalize`，每条路径都恰好产出一个 `Result`。

## 使用

注册一个工具并执行一次调用。工具体只有在参数通过 schema 且策略放行之后才会运行。

```go fragment
registry := tools.NewRegistry()
err := registry.Register(tools.Definition{
	Name:        "read_file",
	Description: "Read a file from the workspace.",
	Schema: tools.InputSchema{
		Type:       tools.SchemaTypeObject,
		Properties: map[string]tools.PropertySchema{"path": {Type: tools.SchemaTypeString}},
		Required:   []string{"path"},
	},
	ReadOnly: true,
	Execute: func(execution tools.Context) (tools.Result, error) {
		return tools.Result{Content: "read " + execution.Arguments["path"].(string)}, nil
	},
})
pipeline := &tools.Pipeline{Registry: registry, Guards: tools.NewGuardChain(tools.DenyPrefix("mcp."))}
result, err := pipeline.Execute(ctx, tools.Call{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.txt"}`})
```

被拒绝或被 schema 拒绝的调用会返回 `ErrToolDenied` 或 `ErrToolInputInvalid`，`Result.Attempts` 为 `0`，工具体一次都不会运行。观察者可通过 `Pipeline.Observer` 收到每个阶段的事件；被拒绝的调用**不会**出现 `StageExecute` 事件。

## 理解实现

- `Executor` 只接收一个 `tools.Context`，里面已经包含调用 id、工具名、原始参数串与解码后的参数值。把 `Call` 再传一次会让工具体有两个地方读同一事实。
- `Pipeline.Execute` 总是同时返回 `Result` 与（非成功时的）`error`：拒绝也必须留下一条模型可见的结果，否则模型不知道发生了什么。
- `post-execute` 可以替换模型可见的 `Content`，但 `CallID`、`Name`、`Outcome`、`IsError`、`Attempts` 与 `Err` 会被强制还原——否则一个失败的调用可能被规范化成一个“成功结果”。
- 取消在工具体之前检查，因此取消不会启动工作，只产生 `OutcomeCanceled`。
- 观察者 panic 会被吞掉：一个 tracing 插件不应能让执行失败。
- `Registry.List` 与 `Names` 按名称排序，使提示词装配与诊断输出可确定性比较。

## 进一步阅读

- [Go 概念对照](../docs/tools/go-primer.md)：从函数与接口出发理解工具三角色。
- [执行管线参考](../docs/tools/execution-pipeline.md)：六阶段顺序、拒绝路径与结果唯一性。
- [llm 包](../llm/README.md)：`ToolCallBlock` 是模型请求工具调用的词汇。

## 验证

```sh
go test -race -count=1 ./tools/...
```

行为测试覆盖：合法调用只运行一次工具体、八类拒绝路径都不运行工具体、guard 链的单调性、未知 guard 决策视为拒绝、取消不启动工作、post-execute 不能改写结果、观察者顺序与 panic 隔离、`ExecuteAll` 一调用一结果。

## 限制

- **没有真实执行**。本阶段的工具体都是测试替身；文件系统、子进程、沙箱与审批属 M7。
- **schema 是受约束的子集**：`object`、`string`、`number`、`integer`、`boolean`、`array`，加 `properties`、`required`、`additionalProperties`、`items`、`enum`。`oneOf`、`$ref`、`pattern` 等关键字未实现，且不会静默忽略 —— 未知的 `type` 会返回 `ErrInvalidValue`。
- **准入策略只有 guard 链**，没有审批交互、没有沙箱、也没有按路径的规则；这些都是 M7 的范围。
- 本包不产生模型可见的持久记录；把 `Result` 写进 Session 日志属后续阶段。
