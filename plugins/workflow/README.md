---
description: "fake workflow 引擎：确定性逐步执行已校验脚本，能力不符则拒绝。"
kind: "package-library"
---

# plugins/workflow

## 概述

这个包提供**确定性 fake workflow 引擎**。它在进程内逐步运行已校验脚本并记录每步结果，使测试或示例无需生产引擎即可观察编排。它是 seam 的 fake 一半：seam 校验脚本与参数，这个引擎执行步骤，让调用方在真实引擎存在前先证明编排形状。引擎拒绝自己未声明的能力，因此「引擎跑不了这个」从声明处判定，而不是中途才发现。

读者需要 Go 接口与 `context` 基础。seam 见 [capability/workflow](../../capability/workflow/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `New` 构造引擎，声明能力（默认 `two-step-delegation`）。
- `Run` 先校验请求，再逐步执行，记录带 `step:` 前缀的 trace。
- `OnStep` 安装每步回调，供测试观察。
- `TraceString` 渲染 trace。

## 使用

下面用一段脚本驱动 fake 引擎并读取 trace。

```go fragment
engine := workflow.New(workflow.Options{})
result, err := engine.Run(ctx, request)
// result.Trace == ["step:a", "step:b"]
```

## 理解实现

- **能力先声明**：未声明能力的脚本返回 `ErrUnknownCapability`，不执行任何步骤。
- **校验先于执行**：parent 或 args 不满足时 trace 为空。
- **每步检查取消**：ctx 取消时在下一步前终止并返回错误。

## 进一步阅读

- [Workflow 与委派](../../docs/tasks/workflow.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/workflow/...
```

覆盖：逐步执行与 trace、未知能力拒绝、坏参数先拒、Info。

## 限制

- **fake**。生产 PTC Provider 属 M12.4。
- **不真正委派子 Agent**：步骤是命名的，执行副作用属生产引擎。
