---
description: "附件 seam：内容不可变、以身份而非路径寻址，读取时重验摘要。"
kind: "package-library"
---

# capability/attachment

## 概述

这个包定义**附件 seam**：内容以**身份**（identity）而非路径寻址，且存储后不可变。原因是路径不是身份——同一路径下的文件可以被替换、移动或删除，而一个引用了该路径的请求会静默地描述成另一个东西。

seam 刻意做得很小：它存字节并如实报告自己做不到什么；真实存储、淘汰与平台目录归 Provider。

读者需要 Go 的接口与 map 基础。当前实现范围是 M8.4 的 seam 与内存参考实现；请求可达映射见 [plugins/attachment](../../plugins/attachment/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Descriptor` 描述内容而不携带内容：身份、模态、大小、摘要。
- `Store` 是 seam 接口：`Put`、`Get`、`Describe`、`Supported`。
- `MemoryStore` 是内存参考实现，安全并发，**存入与取出都拷贝**内容。
- `Digest` 是内容摘要函数；Provider 与测试用它做同一件事，而不是各造一套比较。

## 使用

先建一个声明了模态的 store，再按身份存取。下面的片段省略了逐处错误检查；`Put` 会对重复身份、空内容与不支持的模态失败，`Get` 会对未知身份与摘要不符失败。

```go fragment
store := attachment.NewMemoryStore(attachment.ModalityText)
record, err := store.Put("notes", attachment.ModalityText, []byte("hello"))
descriptor, err := store.Describe("notes")   // 只要元数据时不必读内容
content, record, err := store.Get("notes")   // 读取时重验摘要
```

## 理解实现

- **身份重复即拒绝，而非覆盖**：已被请求引用的内容不得在它底下被换掉。
- **不支持的模态被拒绝**：接受自己无法提供的内容，等于承诺一个读不出来的引用。
- **读取时重验摘要**：内容在存储底下变化（例如文件被替换）时报 `ErrCorrupt`，而不是当作记录过的内容返回。
- **进出都拷贝**：调用方改自己那份字节不会污染 store，也不会被 store 影响。
- **空模态列表表示不支持任何模态**：一个什么都没声明的 store 没有声称能承载什么。
- **nil store 被拒**：`ErrMissingCapability` 而不是 panic。

## 进一步阅读

- [持久附件与本次请求可访问路径](../../docs/context/attachments.md)：存储身份与请求可达映射为何要分开。

## 验证

```sh
go test -race -count=1 -timeout=240s ./capability/attachment/...
```

覆盖：存取往返、重复身份拒绝、空身份/空内容拒绝、不支持模态拒绝、空模态列表语义、未知身份、摘要不符检出、进出拷贝隔离，以及 nil store 的每个方法。

## 限制

- **不含请求可达映射**。哪些附件对某次请求可见归 [plugins/attachment](../../plugins/attachment/README.md)。
- **不做落盘、淘汰或平台目录**。内存实现不持久化。
- **不含模态协商**。第三方（适配器、重放、消费者）共同支持前不声称支持某模态。
