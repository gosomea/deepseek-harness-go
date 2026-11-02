---
description: "渲染两次并比较，再用真实摘要路由压缩一段历史：演示先记录后渲染、被拒贡献不可引用、压缩三阶段与失败不伪造。"
kind: "command-example"
---

# examples/context

## 概述

这个程序不读写文件、不起进程：它先把三条贡献记录到一个内存日志，渲染两次并比较结果，再制造一次被日志拒绝的追加，证明**被拒绝的贡献不会进入任何可渲染的集合**；最后用一条 fake 摘要路由压缩一段历史，演示**三阶段（start／summary／end）**与**摘要失败绝不报告 end**。读它能让[贡献与渲染](../../docs/context/contributions.md)与[压缩](../../docs/context/compaction.md)的核心性质有可对照的实体。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

演示 M8.1 与 M8.7 的核心行为：

1. **渲染是纯函数**：同一集合渲染两次文本相同，因此请求可以逐字比较。
2. **被拒的贡献不可引用**：日志拒绝第二条贡献时，`Record` 返回错误且不返回集合，渲染无从读取它。
3. **压缩三阶段**：一条 fake 路由把区间替换成摘要，`phases` 显示 `start summary end`，工具调用与结果配对不被拆坏。
4. **摘要失败不伪造**：路由失败时 `summary failure reported: true`，`phases` 停在 `start summary`，**不含 end**。

它不读写真实 Session 日志、不解析引用路径、不接真实模型——那些属 M6 与真实摘要路由的联网 smoke。

## 使用

```sh
go run ./examples/context
```

输出逐字保存在 [expected.txt](expected.txt)，并由 `TestDemoMatchesDocumentedOutput` 比对。

## 验证

测试把运行输出与 `expected.txt` 逐字比较，因此任何行为变化都会以失败测试出现，而不是留成一份过期文档。先跑测试确认当前行为，再直接运行程序观察输出：

```sh
go test -count=1 ./examples/context/...
go run ./examples/context
```

两者都应退出 0；`go run` 的输出应与 `expected.txt` 完全一致。

## 限制

- **内存日志不是 Session 日志**。`memoryLog` 只实现 `context.MemoryLog` 这一个方法；真正的日志与恢复语义归 `session` 与 `persistence`。
- **不读取引用内容**。`spec` 只渲染成一行引用。
- **固定时间文本**。示例不依赖真实时钟，因此输出可逐字比较。
- **摘要路由是 fake**。示例用 `internal/testkit` 的 fake Provider，不产生真实模型调用；真实路由的联网 smoke 缺凭据时记为未验证，不是通过。
