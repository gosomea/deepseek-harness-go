---
description: "按 token 预算收内容：超出的部分 spill 出去，并留下可追溯引用。"
kind: "package-library"
---

# plugins/spill

## 概述

这个包处理两件事：**算 token** 与 **按预算收内容**。

算 token 时它坚持区分**两个容易混淆的数字**：Provider 报回的 usage（实测）与本实现事前算出的估算。把估算当实测呈现，就是一个没人能为之负责的数字；所以每个预算决定都标明自己用的是哪一个。

收内容时它坚持**缩短必留引用**：一个承载缩短输出的请求，同时承载找回原文的引用，读者才能区分「缩短了但仍可取回」与「已经丢掉」。

读者需要 Go 的结构体与接口基础。spill seam 与内存实现见 [capability/spill](../../capability/spill/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Meter` 计 token 并标明来源：`MeterUsage`（实测）或 `MeterEstimate`（估算）。`Count(content, usage)` 在给了 usage 时按实测返回。
- `Budget` 是一次请求的内容预算：`MaxTokens`、`Meter` 与 spill `Store`。
- `Fit` 在预算内包含内容；放不下的部分 spill 出去，返回带引用的缩短形式。
- `Render` 拼接已包含内容，并**拒绝**一个「自称缩短却没引用」的条目。
- `Total` 汇总已包含内容的 token 成本。

## 使用

建一个带存储的预算，把内容交给 `Fit` 决定原样包含还是缩短并 spill。下面的片段假定错误会被检查；`Fit` 会在预算装不下引用、或需要 spill 却没配 store 时失败。

```go fragment
budget := spill.Budget{
	MaxTokens: 40,
	Meter:     spill.Meter{Kind: spill.MeterEstimate, TokensPerByte: 0.25},
	Store:     capspill.NewMemoryStore(),
}
included, err := budget.Fit("big", "tool-output", longContent)
text, err := spill.Render([]spill.Included{included})
tokens, kind := budget.Meter.Count(longContent, nil)   // 标明是实测还是估算
```

## 理解实现

- **估算不等于实测**：`Count` 返回 `(int, MeterKind)`，把来源和数字一起交出去。
- **估算器是可确定性测试的比率**，不是某个 tokenizer；测试能断言精确数字，误差边界是读者能推理的东西。
- **非空内容至少算 1 token**：否则内容可以免费进入请求。
- **引用先占预算，再选前缀长度**：先截后补会让缩短形式超出它本该遵守的预算。
- **不能 spill 就不能静默截断**：静默截断与「内容本来就这么短」无法区分，故报 `ErrOverBudget`。
- **`Render` 拒绝无引用的缩短条目**：这是让缩短安全的不变量，放在渲染层以覆盖手工拼装的调用方。

## 进一步阅读

- [长输出如何缩短但仍可定位原文](../../docs/context/budgets.md)：预算、spill 与引用三者如何配合。

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/spill/...
go test -race -count=1 -timeout=240s -run Token|Budget|Truncate|Spill ./plugins/spill/...
```

覆盖：估算确定性与标签、实测优先、非空至少 1 token、零比率、预算内原样包含、超预算 spill 且原文可逐字取回、缩短形式确实落在预算内、无 store 时拒绝、非法预算拒绝、无引用的缩短条目被 Render 拒绝、spill store 失败上报、前缀随预算缩小，以及 Total/Render 边界。

## 限制

- **不落盘**。spill 内容留在 store 中。
- **不做真实 tokenizer**。估算器是公开比率的确定性实现，误差边界如实记录。
- **不含压缩**。历史压缩与摘要属 M8.6。
