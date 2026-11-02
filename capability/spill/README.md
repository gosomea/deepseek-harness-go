---
description: "spill seam：把内容移出请求、留在可寻址处，并给出能找回它的引用。"
kind: "package-library"
---

# capability/spill

## 概述

这个包定义 **spill seam**：内容被移出请求，留在某处可寻址，并被一个能重新找到它的引用替代。

原因是——**从请求内部看，「缩短」与「丢失」长得一模一样**。一条只写着「…」的截断工具输出，读者无法判断剩下的部分是丢了还是可检索。所以 spill **总是把缩短形式与指向原文的稳定引用配成一对**，且引用活得和记录一样久。

读者需要 Go 的接口与 map 基础。当前实现范围是 M8.5 的 seam 与内存实现；预算与 token 计量见 [plugins/spill](../../plugins/spill/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Reference` 标识被 spill 的内容：身份、来源、原始字节数。**它永远不是内容本身**。
- `Store` 是 seam 接口：`Spill`、`Resolve`、`Has`。
- `MemoryStore` 是内存参考实现，安全并发，**进出都拷贝**；另有 `Discard`、`Len`、`References`。

## 使用

把一个超长输出 spill 出去，拿到指向它的引用；需要原文时再按引用取回。下面的片段省略了逐处错误检查；`Spill` 会对重复引用与空内容失败，`Resolve` 会对未知引用失败。

```go fragment
store := spill.NewMemoryStore()
reference, err := store.Spill("r1", "tool-output", []byte("full content"))
record, err := store.Resolve("r1")   // 原文逐字取回
if store.Has("r1") { /* 引用仍然有效 */ }
```

## 理解实现

- **引用重复即拒绝，而非覆盖**：已出现在请求里的引用必须继续指向那次请求描述的内容。
- **空内容被拒绝**：空 spill 无可检索之物，会让「原文可用」变成假话。
- **引用的 `Size` 是原始字节数**：读者据此判断缩掉的是多少，而不是靠猜。
- **进出都拷贝**：调用方改自己那份字节不会污染 store。
- **`Discard` 返回布尔值**：让调用方区分「本来就没了」与「刚被移除」。
- **nil store 被拒**：`ErrMissingCapability` 而不是 panic。

## 进一步阅读

- [长输出如何缩短但仍可定位原文](../../docs/context/budgets.md)：引用如何与缩短形式配对进入请求。

## 验证

```sh
go test -race -count=1 -timeout=240s ./capability/spill/...
```

覆盖：存取往返与逐字取回、重复引用拒绝、空身份/空内容拒绝、未知引用、进出拷贝隔离、Has/Len/Discard/References 语义，以及 nil store 的每个方法。

## 限制

- **不含预算与 token 计量**。何时溢出、缩短多少归 [plugins/spill](../../plugins/spill/README.md)。
- **不做淘汰策略或落盘**。内存实现不持久化；生命周期由 provider 决定。
- **不读文件系统**。原文存放位置属 provider。
