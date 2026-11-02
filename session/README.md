---
description: "把 Agent 的历史存成只能追加的事件日志，并用连续序号与未知事件政策保证同一日志得到同一历史。"
kind: "package-library"
---

# session

## 概述

`session` 回答一个问题：Agent 的历史应该怎么存，才能保证“同一份日志永远还原出同一段对话”？

它给出的答案是**只能追加的事件日志**，而不是一份 `messages` 数组。每条事实是一个带连续序号的不可变事件；日志里的位置就是序号，所以**间隙与重复是缺陷**，一律拒绝、绝不重编号。调用方之后再改自己手里的切片，也改不动已经写入的事实。

第二条规则是**只有会产生模型消息的事件进入有序 surface**。边界标记、失败的 attempt、审计记录都会让日志变长，却不改变模型看到的历史。本切片交付 envelope、序号与 append 语义；surface 与投影由后续切片接入同一份日志。

本包是**内存**实现。磁盘文件、句柄、单写者规则与格式互读属后续阶段。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

本包负责三件事：定义事件 envelope 与 JSON 编解码；按事件类型分类并执行未知事件政策；提供 append-only 日志、连续序号、提交前校验与提交后通知。

它不负责：把事件变成模型消息（surface 与投影由后续切片实现）、落盘、会话命名与清理策略、以及调用模型。这些都可以通过日志暴露的两个 seam 接入——`OnValidate` 在提交前拒绝候选，`OnEvent` 在提交后观察。

## 使用

在仓库根目录保持终端位置，把下方完整程序保存为 `bin/session-demo.go`（先创建 `bin` 目录），执行 `go run ./bin/session-demo.go`。

```go runnable=session-append
package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gosomea/deepseek-harness-go/session"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	log := session.NewLog(session.KnownTypes())
	// A fixed clock keeps this program's output identical on every run.
	moment := time.UnixMilli(1700000000000)
	if err := log.SetClock(func() time.Time { return moment }); err != nil {
		return err
	}

	// A log-only boundary marker lengthens the log without producing a message.
	if _, err := log.Append(session.AppendInput{Type: "turn/start", Data: []byte(`{"turn":0}`)}); err != nil {
		return err
	}
	// A message-producing event declares how it joins the ordered surface.
	user, err := log.Append(session.AppendInput{
		Type:      "user/message",
		Data:      []byte(`{"id":"u1","role":"user"}`),
		SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	})
	if err != nil {
		return err
	}

	// A failed model attempt is recorded without pretending to be a message.
	attempt, err := log.Append(session.AppendInput{Type: "assistant/attempt", Data: []byte(`{"turn":0,"step":0}`)})
	if err != nil {
		return err
	}

	fmt.Printf("user    seq=%d surface=%v\n", user.Seq, user.HasSurface())
	fmt.Printf("attempt seq=%d surface=%v\n", attempt.Seq, attempt.HasSurface())
	fmt.Printf("log length=%d next seq=%d\n", log.Len(), log.Seq())

	// An unknown event type is refused unless it is a purely informational record.
	_, err = log.Append(session.AppendInput{Type: "future/note", Data: []byte(`{"x":1}`)})
	fmt.Printf("unknown required refused=%t\n", errors.Is(err, session.ErrUnknownRequiredEvent))
	note, err := log.Append(session.AppendInput{Type: "future/note", Data: []byte(`{"x":1}`), Ignorable: true})
	if err != nil {
		return err
	}
	fmt.Printf("unknown ignorable recorded=%t ignored-by-history=true\n", note.Ignorable.IsSet())

	// The whole log survives a byte-exact JSON round trip.
	encoded, err := session.MarshalLog(log.Events())
	if err != nil {
		return err
	}
	decoded, err := session.UnmarshalLog(encoded)
	if err != nil {
		return err
	}
	again, err := session.MarshalLog(decoded)
	if err != nil {
		return err
	}
	fmt.Printf("round trip byte-identical=%t events=%d\n", string(again) == string(encoded), len(decoded))

	// A gap is refused, never repaired or renumbered.
	gap := []byte(`[{"type":"turn/start","seq":0,"time":1,"data":{}},{"type":"turn/start","seq":2,"time":1,"data":{}}]`)
	_, err = session.UnmarshalLog(gap)
	fmt.Printf("gap refused=%t\n", errors.Is(err, session.ErrSeqGap))
	return nil
}
```

预期输出：

```text output=session-append
user    seq=1 surface=true
attempt seq=2 surface=false
log length=3 next seq=3
unknown required refused=true
unknown ignorable recorded=true ignored-by-history=true
round trip byte-identical=true events=4
gap refused=true
```

几行是重点：

- `attempt seq=2 surface=false`：失败的模型尝试**进入日志**但**不进 surface**，所以它不会伪装成一条成功的回答。
- `log length=3 next seq=3`：下一个序号就是日志长度。这不是计数器，所以不存在“跳号”这件事。
- `unknown ignorable recorded=true`：未知类型被保留原文；如果它**没有**标记 `ignorable`，整份日志会被拒绝重建，而不是被悄悄跳过。
- `gap refused=true`：间隙被拒绝，不修复也不重编号。

`Append` 的失败发生在事件进入切片之前，所以一次被拒绝的 append 不会推进序号，也不会通知任何观察者。

## 理解实现

<details>
<summary>为什么序号是位置而不是计数器</summary>

如果序号是一个独立分配的计数器，就有两种“事实”：日志的长度，和计数器的值。它们会分叉——一次失败的 append 已经推进了计数器，或者一次恢复把日志截断了但计数器没动。之后所有按序号寻址的东西都会指向错误的记录。

把它定义成**位置**（`seq == len(log)`）以后，这些情况都不存在：间隙与重复成为可检查的缺陷，而不是需要调和的状态。代价是恢复时必须保持密集——这正是参考实现要求“seed 必须从 0 连续”的原因，也是本包 `UnmarshalLog` 逐位检查的原因。

第二条规则与它配合：**提交前把所有能失败的检查做完**。`Append` 会先分类类型、检查 payload 可无损序列化、校验 envelope 与 surface 元数据、再跑全部 `OnValidate`；只有都通过才写入切片。任何一个环节失败，序号、surface 与观察者都保持原样。

## 提交与通知的边界

`OnEvent` 的观察者在事件**已经提交之后**运行。所以观察者抛出的 panic 被捕获并记录，却**不会**把一次成功的 append 变成失败——已经写入的事实不能因为一个下游读者出错而被撤销。这一点与“提交前校验”正好互补：`OnValidate` 能拒绝，`OnEvent` 只能观察。

`Append` 是串行的：同一时刻只有一个 append 在发布，观察者也在这段时间里被通知，所以通知顺序与提交顺序一致。一个观察者在自己的发布过程中再调 `Append` 会得到 `ErrReentrantAppend`，而不是死锁——本包用当前 goroutine 的身份区分“回入”与“另一个 goroutine 的并发 append”，后者正常排队。
</details>

## 进一步阅读

从[从 Go 理解事件日志](../docs/session/go-primer.md)开始：它从 `[]Message` 的边界进入事件溯源，讲清楚为什么日志是权威而消息数组是投影。之后读 [envelope 与 seq 参考](../docs/session/event-log.md)了解每个字段、拒绝条件与未知事件政策。

要回到固定参考，看 `packages/core/session/src/types.ts`（`SessionEvent` 与 `SessionEventMap`）、`packages/core/session/src/index.ts`（`append` 与 `assertSessionEventEnvelope`）与 `packages/core/session/src/known-event-types.ts`。固定提交为 `00102833dfaee1da9f48a3a8eae9d34005a75218`。

## 验证

从项目根目录运行 `go test -race -count=1 ./session` 验证本包；阶段交付执行 `make check`。`session` 已登记在[覆盖门禁](../scripts/doccheck/main.go)的受控清单中，语句覆盖率下限为 90%；上面的完整程序由 `make doc-examples` 执行并逐字比较输出。行为测试集中在 append 的原子性、序号密集、未知事件政策、调用方无法改写已提交事实、观察者失败被容纳、并发 append 与读取、以及 envelope/log 的逐字节往返。

## 限制

- **只有内存日志。** 没有文件、句柄、flush、单写者或恢复；那些属 M6。本包不冒用 DSH 的会话文件格式名，也不读写用户原有的 DSH 数据。
- **不能读取原 DSH Session 文件。** 编解码对照的是 envelope 的语义，不是文件格式互操作。
- **本切片只有日志。** surface 有序列表、消息投影与 `deriveMessages` 由后续切片接入；当前 `AppendInput.SurfaceOp` 只校验元数据的形状与引用规则，不维护节点列表。
- **未知事件类型必须显式登记。** `KnownTypes()` 列出本 build 能解释的类型；一个更新的写入方产生的类型要么带 `ignorable` 标记，要么整份日志被拒绝。这是刻意的“宁可多拒绝”默认值。
- **append 是串行的。** 观察者在提交后同步运行，所以一个慢观察者会拖慢 append。本包不做异步投递，因为那会让“通知顺序等于提交顺序”不再成立。
- **时间不做单调性检查。** 只记录整毫秒；同一毫秒内的两个事件是正常的。参考实现同样只校验“是整数”。

## 与固定参考的实测差异

下面每一条都由直接调用固定提交的实现得出。注意参考有两个入口，宽松程度不同：`snapshotSessionEvent` 只做浅层快照，而 `Session` 构造路径上的 `assertSessionEventEnvelope` 才是有全部规则的边界。本包对齐的是**后者**。

| 行为 | 本包 | 固定参考（authoritative） | 处置 |
| --- | --- | --- | --- |
| envelope 出现未知字段 | 拒绝 | 拒绝（`invalid event envelope`） | 一致 |
| `ignorable` 写 `false` | 拒绝 | 拒绝（只接受字面 `true`） | 一致；这是本轮实测修正的一处 |
| `ignorable` 写 `true` 且类型未知 | 保留原文并接受 | 接受 | 一致 |
| 缺少 `data` | 拒绝 | 拒绝（`data === undefined`） | 一致 |
| `seq` 为负或非整数 | 拒绝 | 拒绝 | 一致 |
| `time` 非整数 | 拒绝 | 拒绝 | 一致 |
| `time` 为负 | 接受 | 接受（只要求 `Number.isSafeInteger`） | 一致 |
| 序号间隙／重复 | 拒绝，绝不重编号 | 拒绝 | 一致 |
| 未知类型无 `ignorable` | 拒绝整份日志 | 拒绝 | 一致 |
| surface 事件缺 `surfaceOp` | 拒绝 | 拒绝 | 一致 |
| 已知 log-only 事件带 `surfaceOp` | 拒绝 | 拒绝 | 一致 |
| `sourceEventSeqs` 为空／重复／指向未来 | 拒绝 | 拒绝 | 一致 |
| 助手消息带 `sourceEventSeqs` | 拒绝 | 拒绝 | 一致 |
| `replace` 的 key 集不恰好是 3 个 | 拒绝 | 拒绝（`invalid replace surfaceOp`） | 一致 |

一条**未实现的对齐**需要写明：参考在 append 前还会检查 `request/header` 的空字段规则、`developer/message` 的 `headerSeq` 规则与 `tool/result` 的 `isError` 一致性。本包已保留这些事件的类型分类，但**尚未实现这三条 payload 级校验**——它们产出的是 `headerSeq` 引用与工具错误语义，属于 M4／M8 的消费者范围。在那之前，本包不会声称校验了它们；`session/README.md` 与 [envelope 参考](../docs/session/event-log.md)都不把这三条列为已完成。
