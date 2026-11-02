# 从 Go 理解事件溯源：为什么 Session 不直接保存一份 messages 数组

## 读完要解决的问题

你在 Go 里保存过一段对话。最直接的写法是：

```go fragment
type Session struct {
	Messages []Message
}

func (s *Session) Add(message Message) {
	s.Messages = append(s.Messages, message)
}
```

单轮对话时它完全够用。它会在四个地方出问题，而且这四处都是同一个根因：

1. **谁在改。** 调用方拿到 `s.Messages` 后可以直接 `s.Messages[0] = ...`，历史被悄悄改写，没有任何记录。
2. **失败的尝试往哪放。** 一次模型调用失败了，或者被取消了。它既不是一条成功的回答，也不应该消失——排查问题需要它。
3. **“模型看到了什么”和“发生过什么”不是一回事。** 审计记录、轮次边界、上下文注入都发生过，但不应进入模型输入。一份 `Messages` 数组没有地方表达这个区别。
4. **重启与重放。** 如果你想从磁盘恢复，你得序列化整个数组；一旦格式变了，旧文件就有歧义。

DSH 的答案是**事件溯源**：不存“对话长什么样”，而存**发生过哪些事实**，再从事实**投影**出对话。这一篇进入 DSH 的 **SessionEvent** 与 append-only 日志。前置知识是 Go 的切片、错误包装、闭包与 `sync` 的基本用法。关联行为见 [M3 执行契约](../10-plans/dsh-go-replication-m03/plans.md)；当前实现范围是 envelope、序号、append 语义与未知事件政策，surface 与投影由后续切片接入。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/core/session/src/types.ts` 与 `packages/core/session/src/index.ts`。

## 从熟悉的 Go 写法出发

把 `[]Message` 换成 `[]Event` 只是第一步，还不够。真正要决定的是**四个具体问题**：

**一、事件的身份是什么？** 你需要一个能寻址的位置。用时间戳会有重复，用一个独立分配的计数器会和日志长度分叉。DSH 的做法是把序号定义成**日志里的位置**：`seq == len(log)`。

**二、一次写入是原子的吗？** 如果校验发生在写入之后，那么“写入成功但校验失败”就成了一种状态。DSH 的做法是把所有能失败的检查放在写入之前，失败就什么都不发生。

**三、已提交的事实能被改吗？** 不能。所以写入时必须**拷贝**调用方的数据，而不是保存它的引用。

**四、遇到不认识的事件怎么办？** 这是最重要的一条，也是这一篇与 C06 的连接点：模型的输出可能在更新；如果旧读取器把不认识的块悄悄转成文本，它会重建出一段**错误但看起来正常**的历史。

关于第四点，DSH 的默认值是**保守的**：一个读取器遇到不认识的类型，**必须**拒绝重建整份日志，除非该事件明确标记为 `ignorable`（可以安全跳过）。理由写在参考的类型注释里：默认“必需”会让一个忘记标记的写入方过度拒绝（麻烦），而默认“可跳过”会让一个被掏空的会话被悄悄恢复（错误）。

哪里不等价：DSH 的 `SessionEvent` 是一个按 `type` 判别的联合类型，`switch (event.type)` 就能让 `data` 收窄，而且 `surfaceOp` 字段在某几个类型上是**类型级必填**的。Go 没有联合类型，本包用带判别字段的结构体加原始 JSON payload；surface 元数据的必填性只能在运行时校验（`ValidateEventEnvelope`）。这条差异是真实的：一个 Go 调用方可以构造出一个 TS 里编译不过的事件，代价是它在运行时报错。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面的完整程序写入三类事件，展示“日志变长但模型历史不变”，然后验证序号间隙被拒绝。

```go runnable=session-primer
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
	moment := time.UnixMilli(1700000000000)
	if err := log.SetClock(func() time.Time { return moment }); err != nil {
		return err
	}

	// The five message-producing types are the only ones that may join the model
	// surface. Everything else is log-only.
	fmt.Printf("surface-eligible types: %v\n", session.SurfaceEventTypes())

	steps := []session.AppendInput{
		{Type: "turn/start", Data: []byte(`{"turn":0}`)},
		{Type: "user/message", Data: []byte(`{"id":"u1","role":"user","content":[]}`),
			SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}},
		{Type: "assistant/attempt", Data: []byte(`{"turn":0,"step":0}`)},
		{Type: "turn/end", Data: []byte(`{"turn":0,"reason":"stop"}`)},
	}
	for _, step := range steps {
		event, err := log.Append(step)
		if err != nil {
			return err
		}
		fmt.Printf("seq=%d type=%-17s len=%d surface-eligible=%t\n",
			event.Seq, event.Type, log.Len(), event.IsSurfaceEligible())
	}

	// The log grew by four; only one of those events can become a model message.
	fmt.Printf("log length=%d message-producing=%d\n", log.Len(), countSurface(log))

	// A gap or a duplicate is refused: a position cannot be invented.
	gap := []byte(`[{"type":"turn/start","seq":0,"time":1,"data":{}},{"type":"turn/start","seq":2,"time":1,"data":{}}]`)
	_, err := session.UnmarshalLog(gap)
	fmt.Printf("gap refused=%t reason=%v\n", errors.Is(err, session.ErrSeqGap), errors.Is(err, session.ErrSeqGap))
	return nil
}

// countSurface counts the events that are eligible to produce a model message.
func countSurface(log *session.Log) int {
	count := 0
	for _, event := range log.Events() {
		if event.IsSurfaceEligible() {
			count++
		}
	}
	return count
}
```

预期输出：

```text output=session-primer
surface-eligible types: [assistant/message developer/message system/message tool/result user/message]
seq=0 type=turn/start        len=1 surface-eligible=false
seq=1 type=user/message      len=2 surface-eligible=true
seq=2 type=assistant/attempt len=3 surface-eligible=false
seq=3 type=turn/end          len=4 surface-eligible=false
log length=4 message-producing=1
gap refused=true reason=true
```

关键的一行是最后第二行：**日志长度 4，但只有一个事件可能成为模型消息**。`turn/start`、`assistant/attempt`、`turn/end` 都真实发生过、都不可删除，却都不进模型输入。这就是为什么不能用一份 `Messages` 数组当权威事实——它没有地方放这三条。

`assistant/attempt` 这一条尤其值得注意：它是**失败的模型尝试**。它记录在日志里，所以排查问题时你看得见它；它不在 surface 上，所以它**不会假装成一条成功的回答**。

## 先预测，再观察

先做一个预测：如果一次 `Append` 因为校验失败而被拒绝，下一个事件的序号是多少？

```go fragment
log.Append(session.AppendInput{Type: "turn/start", Data: []byte(`{`)})
next, _ := log.Append(session.AppendInput{Type: "turn/start", Data: []byte(`{}`)})
```

预测之后，运行

```sh
go test -run TestRejectedAppendLeavesTheLogUnchanged -v ./session
```

答案：序号**没有跳过**。被拒绝的 append 在写入之前就失败了，所以 `seq` 仍然是日志长度。这个测试还固定了第二件事：被拒绝的 append **不通知任何观察者**。

对比一下：如果把序号做成一个独立计数器，那么在失败路径上你会面对一个选择题——回滚计数器（那它就不是单调的）还是保留（那它就和日志长度分叉了）。把序号定义成位置，这个选择题不存在。

再预测一次：一个**已提交**事件的 payload 能被改写吗？试试这样：

```go fragment
data := []byte(`{"a":1}`)
log.Append(session.AppendInput{Type: "turn/start", Data: data})
data[3] = '9' // 调用方改自己手里的缓冲区
```

运行 `go test -run TestAppendCopiesCallerOwnedInput -v ./session`。答案是**不能**：`Append` 在写入时拷贝了 payload 与引用列表。这是“已提交事实不可被悄悄改写”在 Go 里的具体含义——不是靠约定，是靠拷贝。

最后一次预测：写入一个类型是 `future/note` 的事件会怎样？再写一个同样的、但带 `Ignorable: true` 的呢？

第一个被拒绝（`ErrUnknownRequiredEvent`），第二个被接受并**原样保留** payload。用 `go test -run TestUnknownEventPolicy -v ./session` 验证。这个政策是整个复刻里最需要记住的一条：**未知种类一律保留，除非写入方明确说它可以丢**。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | `SessionEvent` 是带 `type` 判别字段的联合，含 `seq`/`time`/`data` 与可选的 `ignorable`、`surfaceOp`、`sourceEventSeqs`；`Session.append` 在写入前完成全部校验；未知类型默认“必需” |
| TypeScript 入口 | `packages/core/session/src/types.ts`（`SessionEvent`、`SurfaceOp`、`SessionEventMap`）、`packages/core/session/src/index.ts`（`append`、`assertSessionEventEnvelope`）、`packages/core/session/src/known-event-types.ts` |
| Go 入口 | [session/types.go](../../session/types.go)（`Event`、`Seq`、`SurfaceOp`）、[session/event.go](../../session/event.go)（envelope 与引用规则）、[session/log.go](../../session/log.go)（`Log.Append`、两个 seam）、[session/registry.go](../../session/registry.go)（未知事件政策） |
| 保持的行为 | 序号是日志位置且密集；提交前完成全部校验；被拒绝的 append 不推进序号也不通知；payload 与引用被拷贝；未知类型默认拒绝、标记后可跳过并保留原文；`ignorable` 只接受字面 `true`；surface 事件必须带 `surfaceOp`，log-only 事件不得带 |
| Go 表达 | 带判别字段的结构体加 `json.RawMessage` payload（没有联合类型）；`Seq` 是 `uint64`；`OnValidate` 提交前 seam 与 `OnEvent` 提交后 seam；`Registry` 显式登记类型 |
| 不等价边界 | 见下表 |

### 对照实测得到的差异

下面每一条都由直接调用固定提交得出。注意参考有**两个**入口且严格程度不同：`snapshotSessionEvent` 只做浅层快照，真正带全部规则的边界是 `Session` 构造路径上的 `assertSessionEventEnvelope`。本包对齐的是后者。

| 项目 | 固定参考 | 本包 | 说明 |
| --- | --- | --- | --- |
| envelope 未知字段 | 拒绝 | 拒绝 | 一致 |
| `ignorable: false` | 拒绝 | 拒绝 | 一致；初版接受它，实测后修正 |
| 缺 `data` | 拒绝 | 拒绝 | 一致 |
| 负 `time` | 接受 | 接受 | 一致；只要求整数 |
| 序号间隙／重复 | 拒绝 | 拒绝 | 一致，都不重编号 |
| 未知类型无标记 | 拒绝整份日志 | 拒绝 | 一致 |
| payload 级规则（`request/header` 空字段、`developer/message` 的 `headerSeq`、`tool/result` 的 `isError`） | 校验 | **未实现** | 真实缺口，属 M4／M8；本包不声称已校验 |

最后一行必须如实对待：本包已保留这些事件的类型分类与 envelope 校验，但**没有**实现这三条 payload 级规则。它们断言的是 `headerSeq` 引用与工具错误语义，消费者在 M4／M8 才出现；在那之前声称“已复刻”是不诚实的。

### 第二处差异：并发

参考是单线程的 JavaScript：`append` 不能被重入，也只处理这一个问题。Go 的日志必须同时回答两个问题——**回入**（观察者在自己的发布过程中再 append）与**并发**（另一个 goroutine 同时 append）。本包用当前 goroutine 的身份把两者分开：回入返回 `ErrReentrantAppend`，并发则正常排队。这是 Go 特有的设计点，参考里没有对应物。

## 一次操作的时序与所有权

一次 append 的完整路径：

1. `Append` 先检查是不是当前 goroutine 的回入（是则返回 `ErrReentrantAppend`）。
2. 取得发布权，然后检查类型是否为空、类型是否已知（未知且无标记就拒绝）。
3. 在状态锁内构造事件：`seq = len(events)`、时间取时钟或显式值、**拷贝** payload 与引用列表。
4. 校验：payload 可无损序列化 → envelope 字段齐全 → surface 元数据与引用规则。
5. 取出 `OnValidate` 与 `OnEvent` 的快照并释放状态锁，然后依次跑校验器。任一失败即返回，**日志未变、观察者未通知**。
6. 重新取锁，把事件追加进切片，取出观察者快照，放锁。
7. 依次调用观察者；panic 被恢复成错误并记入 `ObserverErrors()`；**返回值仍是成功**。
8. 返回已提交事件的**拷贝**。

谁持有状态：`Log` 用一把状态锁保护事件切片与回调列表；发布权（`publish`）保证提交与通知的顺序。事件值是值类型，返回给调用方的是拷贝，所以调用方无法反写。没有 goroutine 由本包启动，也没有 `context`——取消不属于日志，日志只负责记住发生过的事。

## 失败与定位

**一、`session: event seq is not contiguous`。**
症状：`UnmarshalLog` 拒绝一份日志，报告某个下标期望的序号。原因：日志不是从 0 连续。可能是尾部被截断、或者是两次写入拼接时漏了一段。诊断入口：[session/codec.go](../../session/codec.go) 的 `UnmarshalLog`。恢复：不要“补”序号——找到缺失的那一段事件重新拼；如果确实丢了，那份日志就是不可信的。

**二、`session: unknown event type is not ignorable`。**
症状：写入或读取时遇到一个本 build 不认识的类型。原因：要么写入方来自更新的版本，要么它应该被标记为纯信息记录。诊断入口：[session/registry.go](../../session/registry.go) 的 `Classify` 与 `KnownTypes()`。恢复：如果是你的类型，用 `Registry.Register` 登记，或在 `KnownTypes()` 之上组合一个注册表；如果是别人写的、且它的丢失确实不影响重建，让写入方加 `ignorable`。**不要**为了让它通过而放宽 `KnownTypes()`。

**三、`session: append cannot reenter while another append is being published`。**
症状：`OnEvent` 回调里调 `Append` 得到这个错误。原因：观察者在提交后的发布窗口内运行；此时再 append 会重排日志。诊断入口：`Log.Append` 的回入检查。恢复：把想写的事件收集起来，在回调返回之后再 append；或者用 `OnValidate`（也在窗口内，同样不能 append）。

区分四类：**预期等待**（并发 append 会排队，不是错误）、**无效输入**（上面的第一、二条与 envelope 校验失败）、**执行失败**（`OnValidate` 返回的错误会原样成为 `Append` 的错误，且不留痕）、**释放失败**（本包没有资源要释放，所以没有这一类）。

## 回到源码与下一步

建议的阅读顺序：

1. [session/types.go](../../session/types.go) 的 `Event` 与 `Seq`——先建立“序号是位置”的认识。
2. [session/log.go](../../session/log.go) 的 `Append`——看校验为什么必须在写入之前。
3. [session/event.go](../../session/event.go) 的 `validateSurfaceMetadata` 与 `validateSources`——元数据的双向规则。
4. [session/registry.go](../../session/registry.go) 的 `Classify`——未知事件政策的落点。
5. 对照 `packages/core/session/src/index.ts` 的 `assertSessionEventEnvelope` 与 `append`。

对应测试：`TestAppendAssignsDenseSequencesFromTheLogLength`（位置就是长度）、`TestRejectedAppendLeavesTheLogUnchanged`（原子性）、`TestAppendCopiesCallerOwnedInput`（不可改写）、`TestUnknownEventPolicy`（未知事件政策）、`TestObserverFailureIsContained`（观察者失败被容纳）、`TestAppendIsNotReentrant`（回入拒绝）、`TestConcurrentAppendsAndReads`（并发下序号仍密集且唯一）、`TestLogRoundTripRefusesGapsAndDuplicates`（间隙与重复拒绝）。

读完你应该能回答：**这个机制解决什么问题**——把“发生过什么”和“模型看到了什么”分开，并让前者不可被悄悄改写；**谁拥有资源**——`Log` 拥有事件切片与回调列表，事件是值，返回的是拷贝；**为什么这样表达**——序号必须是位置（否则会和长度分叉），校验必须在写入前（否则会存在半完成状态），未知类型必须默认拒绝（否则会重建出错误但看起来正常的历史）。

下一篇是 [C06 消息模型](../llm/go-primer.md)：这些事件里承载的对话数据长什么样。

需要更细的字段与拒绝条件时，查 [envelope 与 seq 参考](event-log.md)。
