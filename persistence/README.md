---
description: "定义 Session 的存储 seam：服务负责创建、打开、统计与列举，句柄负责追加与读取，并把单写者与只读规则写进契约。"
kind: "package-library"
---

# persistence

## 概述

`persistence` 回答一个问题：**一次打开会话到底得到了什么权限？**

它给出的答案是**窄契约 + 明确所有权**。`Service` 只做四件事——创建、打开、统计、列举；`Handle` 是**单所有权**状态，不是共享服务：`read` 句柄从不取得所有权，可以在别人持有 `write` 时工作；`write` 句柄是会话唯一的追加者；句柄关闭后每个操作都失败。这些不是建议，而是接口两侧都要守的规则。

它刻意**不认识文件、JSON Lines 或锁**。那些是 Provider 的事：本包拥有 seam 和句柄必须守的规则，所以换一个后端不需要动 Agent 或日志。第二个后端要研究的是[插件目录](../plugins/sessionjsonl/README.md)。

**本包与 DSH 的格式无关**：它定义的是 Go 侧的存储契约，不声称能读写参考实现的会话文件。原始日志互读属后续里程碑。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

本包负责：

- **seam**：`Service`（创建/打开/统计/列举/关闭）与 `Handle`（读取/追加/落盘/关闭）。
- **所有权规则**：`HandleBase` 把「关闭后拒绝一切」与「只读句柄拒绝写入」实现一次，避免每个后端各写一遍、各漏一处。
- **单写者登记**：`BaseService` 持有 `writers` 集合，第二条写声明被明确拒绝，而不是等两个写者交错追加时才出问题。
- **恢复语义**：`Resume` 从日志重建「还在飞行中」的工作（未结算的工具调用、未关闭的轮次、未被 step 取走的输入），并把它**报告**出来而不是重新执行。`WriteCheckpoint`/`ReadCheckpoint` 记录并读回持久边界。
- **派生缓存边界**：`Cache` 是可丢弃的派生视图，`Restore` 同时返回状态与历史，避免调用方拿到空视图继续跑。

本包不负责：真实文件格式（见 `plugins/sessionjsonl`）、压缩、附件、跨进程文件锁的具体机制、以及任何模型或工具行为。

## 使用

```go fragment
store, err := sessionjsonl.New(sessionjsonl.Options{DataDir: dir})
if err != nil {
	return err
}
if _, err := store.Create(ctx, persistence.CreateOptions{ID: "s-1"}); err != nil {
	return err
}
handle, err := store.Open(ctx, persistence.OpenOptions{ID: "s-1", Access: persistence.AccessWrite})
if err != nil {
	return err // 第二个写者会在这里被拒绝
}
defer handle.Close()

event, err := handle.Append(ctx, session.AppendInput{
	Type: "turn/start", Data: []byte(`{"turn":0,"input":"hello"}`),
})
if err != nil {
	return err
}
// Append 只承诺「进入本句柄的缓冲并对自己可见」；Flush 才承诺落盘。
if err := handle.Flush(ctx); err != nil {
	return err
}
_ = event
```

重开并判断「还欠什么工作」：

```go fragment
state, messages, err := persistence.Restore(ctx, store, "s-1", persistence.NewCache())
if err != nil {
	return err
}
if state.NeedsAttention() {
	// 崩溃前请求过、但没有结果的调用在这里被列出，而不是被重新执行。
	return fmt.Errorf("unsettled: %v", state.UnsettledCalls)
}
_ = messages
```

## 理解实现

三处设计值得单独说明。

**1. `HandleBase` 是共享的，不是继承的装饰。** 关闭与只读两条规则如果用注释约定，两个后端里必然有一个漏掉。把它做成内嵌类型并让 `CheckOpen(writing bool)` 同时判断，规则就只剩一个实现点。写入操作先判只读、再判关闭，所以「用只读句柄写」报告的是更具体的原因。

**2. `wrap` 而不是复制。** 参考实现里 `Handle` 是单所有权状态：`read` 不回溯到本句柄已观察位置之下，`write` 句柄读得到自己成功追加的记录。本包把这条要求写进接口注释，并在测试里断言——写得出来但测不出来的一致性规则等于没有。

**3. `Resume` 只描述、不行动。** 崩溃可能发生在工具体之前、之中或之后，因此「结果没结算」并不等于「副作用没发生」。把未结算调用**列出来**交给调用方决定，是唯一不会自作主张重放副作用的做法。

## 进一步阅读

- [JSONL 存储](../plugins/sessionjsonl/README.md)：本包的第一个后端实现。
- [会话事件日志](../session/README.md)：被持久化的那份日志本身。
- [持久化入门](../docs/persistence/go-primer.md)：从 Go 视角理解 seam、句柄与所有权。
- [格式与损坏](../docs/persistence/format.md)：记录边界、校验与恢复。
- [恢复与 checkpoint](../docs/persistence/recovery.md)：重开之后哪些工作会继续。
- [派生缓存](../docs/persistence/cache.md)：为什么缓存不能是权威。

## 验证

第一条命令用竞态检测跑本包的全部行为测试：句柄规则、单写者、恢复状态、checkpoint 与缓存重建。第二条命令确认本包出现在覆盖率门禁清单里且不低于 90% 语句覆盖率。

```sh
go test -race -count=1 ./persistence/...
go run ./scripts/doccheck -coverage coverage.out
```

## 限制

- **本包不含文件格式**：它定义契约；字节格式在 `plugins/sessionjsonl`。
- **不读 DSH 会话文件**：Go 自有格式与参考实现的 generation 不同，互读属后续里程碑。
- **不是分布式协调**：单写者是「同一数据目录内的句柄与进程」之间的规则，不跨机器。
- **内存 Store 不承诺持久**：`MemoryStore` 用于无文件系统的测试与故障注入，不保存字节。
- **无压缩与附件**：属后续里程碑。
