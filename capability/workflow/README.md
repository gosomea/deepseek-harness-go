---
description: "workflow seam：脚本 meta/args 校验与执行 Provider，区分编排描述与执行引擎。"
kind: "package-library"
---

# capability/workflow

## 概述

这个包定义 **workflow seam**：一段多步编排被描述为脚本，由执行引擎运行。它把「编排描述」与「执行它的东西」分开，使调用方能在交给引擎前先校验脚本的 meta 与参数，并使 fake 引擎能在不改变编排的情况下替代真实引擎。引擎不认识某能力时在 seam 处拒绝，而不是跑到中途才失败。

读者需要 Go 结构体与 `context` 基础。fake 引擎见 [plugins/workflow](../../plugins/workflow/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Script` 带 `Capability`、`Args` 与 `Steps`；`Validate` 校验结构。
- `ValidateArgs` 要求所有声明的 arg 非空，且拒绝未声明的 arg。
- `Request.Validate` 要求 parent 存在。
- `Provider` 运行请求；`ProviderInfo` 声明引擎能力。

## 使用

下面构造一个两步脚本并交给引擎运行。

```go fragment
request := workflow.Request{Parent: "p", Script: workflow.Script{ID: "two-step", Capability: "two-step-delegation", Steps: []string{"a", "b"}}}
result, err := engine.Run(ctx, request)
```

## 理解实现

- **校验先于执行**：meta 与 args 不满足时任意步骤都不跑，避免半截 trace。
- **多余 arg 被拒**：接受未声明 arg 会让调用方以为某个值起了作用。
- **parent 必需**：编排无所有者则副作用不可归属。

## 进一步阅读

- [Workflow 与委派](../../docs/tasks/workflow.md)

## 验证

```sh
go test -race -count=1 -timeout=240s ./capability/workflow/...
```

覆盖：脚本校验、参数校验（缺／多）、parent 必需、能力声明。

## 限制

- **不解释步骤**：seam 只校验并委派。
- **生产 PTC Provider 属 M12.4**。
