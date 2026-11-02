---
description: "把已存储附件映射成某次请求可以读取的东西，失败都给诊断。"
kind: "package-library"
---

# plugins/attachment

## 概述

这个包回答：**这一次请求到底能读到哪些附件？** 已存储的内容与请求可达的内容是两个不同的事实。附件在 store 里按身份存一次；请求能读什么，是叠在 store 之上的**每次请求一份的映射**。如果请求能读 store 里的任何东西，"这次请求被允许看到什么"就没有答案。

因此映射是显式的、按身份解析的，而且**每一种失败都给出诊断**——身份不存在、模态不支持、执行世界不同、内容损坏——而不是返回一个看起来像"附件不存在"的空结果。

读者需要 Go 的结构体与接口基础。附件 seam 与内存实现见 [capability/attachment](../../capability/attachment/README.md)。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `World` 命名映射所属的执行世界；同一身份在不同世界里是不同对象。
- `Mapping` 持有本次请求可读的**身份集合**，不持有内容。
- `Expose` 把一个身份加入本次请求；暴露前先确认 store 确实能承载该模态。
- `Resolve` 在世界匹配且身份已暴露时读取内容；`Diagnose` 逐个报告为何读不到。
- `Descriptors` 返回已暴露附件的元数据（按身份排序），**不返回内容**。

## 使用

映射叠在 store 之上：先 `Expose` 本次请求可读的身份，再在世界匹配的前提下 `Resolve`。下面的片段假定错误会被检查；`Expose` 会对未知身份与未开放的模态失败。

```go fragment
store := attachment.NewMemoryStore(attachment.ModalityText, attachment.ModalityImage)
store.Put("notes", attachment.ModalityText, []byte("notes body"))

mapping := attachment.NewMapping("local", store, attachment.ModalityText)
err := mapping.Expose("notes")
content, descriptor, err := mapping.Resolve("notes", "local")
diagnostics := mapping.Diagnose([]string{"notes", "ghost"}, "local")
```

## 理解实现

- **暴露的模态要显式列出**：一个"暴露 store 恰好支持的一切"的映射，会让 store 升级静默扩大请求可读范围。
- **存储可达 ≠ 本次请求可达**：store 里的附件若未 `Expose`，`Resolve` 报 `ErrUnknownAttachment`。
- **执行世界必须匹配**：在本机解析、到远端读取是不同对象；不匹配报 `ErrWorldMismatch`。
- **暴露前确认 store 承载能力**：身份不靠名字就成立。
- **内容损坏报诊断**：store 读出的摘要不符时如实上报，绝不当作记录内容返回。
- **映射只持身份**：所以复制映射不会连带复制内容。

## 进一步阅读

- [持久附件与本次请求可访问路径](../../docs/context/attachments.md)：三种失败为何要分开诊断。

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/attachment/...
go test -race -count=1 -timeout=240s -run Corrupt|Missing|World|Modality ./plugins/attachment/...
```

覆盖：暴露后解析、存储可达但未暴露的拒绝、缺失身份、世界不匹配、模态不暴露与 store 无力承载、损坏内容诊断、诊断排序与理由、重复暴露幂等、元数据不含内容，以及 nil/空输入。

## 限制

- **不落盘**。内容留在 store 中，本包只维护映射。
- **不读文件系统**。路径转身份属 Provider，本包不含路径解析。
- **不实现模态协商**。适配器、重放与消费者三方共同支持前不开放某模态。
