---
description: "在临时目录写一个会话、落盘、关闭，再由新的 Provider 重开：演示单写者、恢复状态与两类损坏判定。"
kind: "command-example"
---

# examples/session-replay

## 概述

这个程序不联网、不需要密钥：它在**自己的临时目录**里写一个走到一半的会话——轮次开着、工具调用已记录但没结果——然后 `Flush`、关闭，再用一个**新的 Provider 实例**打开它，打印三件事：重开后的历史、还欠什么工作、以及一个被损坏的副本会得到哪种恢复判定。读它能把[恢复与 checkpoint](../../docs/persistence/recovery.md) 里的每条规则对应到一次真实输出。

它对用户既有数据是安全的：只写自己创建的临时目录，并在结束时删除。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

演示 M6 的五条核心行为：

1. **单写者**：写句柄持有会话期间，第二次写打开被拒绝（`persistence.ErrWriterExists`）。
2. **落盘与重开**：`Flush` 之后用**新** Provider 读回同一份历史——用新实例是刻意的，同一实例缓存着自己的视图，证明不了字节真的落盘。
3. **恢复只描述**：重开后 `openTurn=true`、`unsettled=[call-1]`、`needsAttention=true`，而工具体**没有**被重新执行。
4. **撕裂尾部可修复**：末尾只有半条记录时 `repairable=true`。
5. **中间损坏不可修复**：改动中间记录的校验和后 `repairable=false`，`reason` 指出具体位置与原因。

它不负责真实模型、压缩或附件——那些属后续里程碑。

## 使用

```sh
go run ./examples/session-replay
```

输出逐字保存在 [expected.txt](expected.txt)，由 `TestReplayMatchesDocumentedOutput` 比对。程序使用固定时钟，因此两次运行输出相同。

## 验证

```sh
go test -count=1 ./examples/session-replay/...
go run ./examples/session-replay
```

测试逐字比对快照、两次运行的一致性，并断言示例留下的是「拒绝第二个写者、未结算调用可见、两类损坏判定不同」这些具名行为。另有一条安全断言：示例不会在系统临时目录留下 `session-replay-*` 残留。

## 限制

- **没有真实模型**：会话事件是脚本化的；真实端点属后续里程碑。
- **不读 DSH 会话文件**：程序只操作自己格式的 artifact，跨实现互读属后续里程碑。
- **不做跨进程崩溃注入**：崩溃用「损坏副本」模拟，不声称覆盖所有平台的中断电。
- **损坏的是副本**：程序不改写自己创建的正常 artifact，损坏只发生在临时副本上。
