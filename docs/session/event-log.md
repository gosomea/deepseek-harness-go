# 事件 envelope 与序号参考

## 读完要解决的问题

你已经知道日志是权威事实（见[事件溯源](go-primer.md)）。接下来的问题是：**一个事件到底长什么样，序号怎么算，哪些事件会被拒绝，遇到不认识的事件怎么办**。

这不是查表问题。第三问与第四问决定了复刻是否可信：如果校验发生在写入之后，就会存在“写进去了但没人能读”的状态；如果把不认识的类型悄悄跳过，就会重建出一段错误但看起来正常的历史。这一篇逐条给出字段、拒绝条件与两层未知事件政策。

前置知识：Go 的结构体、`encoding/json` 与 `errors.Is`。关联行为见 [M3 执行契约](../10-plans/dsh-go-replication-m03/plans.md)。

## 从熟悉的 Go 写法出发

普通 Go 做法是给事件定义一个结构体，用 `interface{}` 装 payload：

```go fragment
type Event struct {
	Type string
	Seq  int
	Data any
}
```

它简单、能序列化。三个边界：

1. **顺序不确定。** `any` 里的 map 编码顺序随机，同一份日志两次编码得到不同字节——而日志要比较与哈希。
2. **字段可以随手加。** 有一天有人加了 `Turn` 字段到 envelope 上，旧读取器读到它却不知道它影响什么。
3. **不认识的东西无法区分。** 旧读取器遇到新类型时，既不知道它是否影响重建，也没有字段表达“可以跳过”。

DSH 的 envelope 是一个**恰好七个字段的定形记录**：`type`、`seq`、`time`、`data`、`ignorable?`、`surfaceOp?`、`sourceEventSeqs?`。参考的 `assertSessionEventEnvelope` 会遍历自有键，**任何**不在这个集合里的字段都直接拒绝。

本包照搬这个形状，但有一处 Go 特有的处理：payload 保持 `json.RawMessage`（原始 JSON）。这样既解决了上面第 1 点（不需要通过 `any` 编解码），也让一个本 build 不认识的 payload **原样穿过**。

哪里不等价：参考的 `data` 字段在类型上是 `SessionEventMap[type]`，也就是按判别字段收窄的联合；`ignorable`、`surfaceOp`、`sourceEventSeqs` 的存在性是**类型级**约束。Go 没有联合类型，所以本包用带判别字段的结构体加 `json.RawMessage`，把这些约束移到运行时的 `ValidateEventEnvelope`。一个 Go 调用方可以构造出 TS 里编译不过的事件——代价是它在运行时报错。差异在[配置与装配的处置](../10-plans/dsh-go-replication-m03/plans.md)里也记了一笔。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面的完整程序打印两条合法 envelope 的编码，然后逐条展示拒绝条件。

```go runnable=session-event-log
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/session"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// A log-only event: no surface metadata is allowed on it.
	marker := session.Event{Type: "turn/start", Seq: 0, Time: 1700000000000, Data: []byte(`{"turn":0}`)}
	encoded, err := session.MarshalEvent(marker)
	if err != nil {
		return err
	}
	fmt.Println("log-only :", string(encoded))

	// A message-producing event must declare how it enters the surface.
	message := session.Event{
		Type:      "user/message",
		Seq:       1,
		Time:      1700000000001,
		Data:      []byte(`{"id":"u1","role":"user"}`),
		SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
	}
	encoded, err = session.MarshalEvent(message)
	if err != nil {
		return err
	}
	fmt.Println("surface  :", string(encoded))

	// A replacement cites the range it shadows, and must cite every node in it.
	replace := session.Event{
		Type:            "user/message",
		Seq:             5,
		Time:            1700000000002,
		Data:            []byte(`{"id":"summary","role":"user"}`),
		SurfaceOp:       &session.SurfaceOp{Op: session.SurfaceReplace, StartSeq: 1, EndSeq: 3},
		SourceEventSeqs: []session.Seq{1, 3},
	}
	encoded, err = session.MarshalEvent(replace)
	if err != nil {
		return err
	}
	fmt.Println("replace  :", string(encoded))

	// Now the rejections. Each is refused rather than repaired.
	rejects := []struct {
		label string
		event session.Event
	}{
		{"surface event without marker", session.Event{Type: "user/message", Seq: 0, Time: 1, Data: []byte(`{}`)}},
		{"log-only event with marker", session.Event{Type: "turn/start", Seq: 0, Time: 1, Data: []byte(`{}`),
			SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}}},
		{"missing payload", session.Event{Type: "turn/start", Seq: 0, Time: 1}},
		{"replacement cites the future", session.Event{Type: "user/message", Seq: 2, Time: 1, Data: []byte(`{}`),
			SurfaceOp: &session.SurfaceOp{Op: session.SurfaceReplace, StartSeq: 0, EndSeq: 2}}},
	}
	for _, reject := range rejects {
		err := session.ValidateEventEnvelope(reject.event)
		fmt.Printf("%-30s refused=%t\n", reject.label, err != nil)
	}
	// An extra envelope key can only be seen by the decoder: a Go struct has no
	// field for it, so this check belongs at the decode boundary.
	_, err = session.UnmarshalEvent([]byte(`{"type":"turn/start","seq":0,"time":1,"data":{},"turn":0}`))
	fmt.Printf("%-30s refused=%t\n", "extra envelope key", err != nil)

	// Unknown events: required by default, skippable only when marked.
	log := session.NewLog(session.KnownTypes())
	_, err = log.Append(session.AppendInput{Type: "future/note", Data: []byte(`{}`)})
	fmt.Printf("%-30s refused=%t\n", "unknown without marker", errors.Is(err, session.ErrUnknownRequiredEvent))
	note, err := log.Append(session.AppendInput{Type: "future/note", Data: []byte(`{"x":1}`), Ignorable: true})
	if err != nil {
		return err
	}
	fmt.Printf("%-30s accepted=%t payload=%s\n", "unknown with marker", note.Ignorable.IsSet(), note.Data)
	return nil
}
```

预期输出：

```text output=session-event-log
log-only : {"type":"turn/start","seq":0,"time":1700000000000,"data":{"turn":0}}
surface  : {"type":"user/message","seq":1,"time":1700000000001,"data":{"id":"u1","role":"user"},"surfaceOp":"append"}
replace  : {"type":"user/message","seq":5,"time":1700000000002,"data":{"id":"summary","role":"user"},"surfaceOp":{"op":"replace","startSeq":1,"endSeq":3},"sourceEventSeqs":[1,3]}
surface event without marker   refused=true
log-only event with marker     refused=true
missing payload                refused=true
replacement cites the future   refused=true
extra envelope key             refused=true
unknown without marker         refused=true
unknown with marker            accepted=true payload={"x":1}
```

## 先预测，再观察

先做一个预测：下面这份日志会被接受吗？

```text
[
  {"type":"turn/start","seq":0,"time":1,"data":{"turn":0}},
  {"type":"turn/start","seq":2,"time":1,"data":{"turn":1}}
]
```

预测之后运行：

```sh
go test -run TestLogRoundTripRefusesGapsAndDuplicates -v ./session
```

答案：**拒绝**，错误是 `ErrSeqGap`，信息里指出下标 1 期望序号 1 却拿到 2。间隙**不修复**、**不重编号**。理由：序号是位置，位置不能凭空发明。如果允许“补齐”，那么所有按序号寻址的东西（引用、替换范围、投影的 watermark）都会指向错误的记录。

再预测一次：`ignorable` 写 `false` 会怎样？

你可能觉得“false 就是不需要跳过，等价于不写”。**不是**。参考的权威校验要求这个字段**要么不存在，要么是字面 `true`**；写 `false` 会被拒绝。用 `go test -run TestIgnorableMarkerRoundTrip -v ./session` 验证。

为什么这么严？因为一个标记如果既能用“不存在”又能用“false”表示“不需要跳过”，读取器就要调和两种表达。只接受 `true` 让语义只有一个方向：**写下来就是承诺它可以丢**。

最后一次预测：一个已提交事件的 payload，被调用方后来改掉自己手里的缓冲区，日志里的值会变吗？

答案是**不会**：`Append` 在写入时拷贝 payload 与引用切片（`go test -run TestAppendCopiesCallerOwnedInput -v ./session`）。没有这一条，“已提交事实不可被悄悄改写”只是一句约定。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 七个字段的定形 envelope；`ignorable` 默认“必需”；surface 元数据的双向约束；`sourceEventSeqs` 必须覆盖全部被遮蔽节点 |
| TypeScript 入口 | `packages/core/session/src/types.ts`（`SessionEvent`、`SurfaceOp`、`SurfaceIntent`）、`packages/core/session/src/index.ts`（`assertSessionEventEnvelope`、`validateSessionEventData`） |
| Go 入口 | [session/types.go](../../session/types.go)（`Event`、`Seq`、`SurfaceOp`、`Ignorable`）、[session/event.go](../../session/event.go)（envelope 与引用规则）、[session/codec.go](../../session/codec.go)（JSON 编解码）、[session/registry.go](../../session/registry.go)（类型分类） |
| 保持的行为 | envelope 恰好七个字段，未知字段拒绝；`data` 必填；`seq` 非负整数；`time` 整数（可为负）；`ignorable` 只接受字面 `true`；surface 事件必须带 `surfaceOp`，log-only 事件不得带；替换范围必须已存在于 surface 且严格早于自身；`sourceEventSeqs` 非空、无重复、更早、且覆盖全部被遮蔽节点；助手消息不得引用来源 |
| Go 表达 | 判别字段结构体加 `json.RawMessage`；`SurfaceOp` 用结构体加判别字段（判别变体的 key 集在解码时严格检查）；字段顺序固定在编码器里 |
| 不等价边界 | 见下表 |

### 字段表

**未知字段只能由解码器发现。** `ValidateEventEnvelope` 接收的是一个已经成形的 Go 结构体——额外字段根本没有落脚点，所以“拒绝未知 envelope 字段”这条规则落在 `UnmarshalEvent` 上，而不是结构体校验上。这也是为什么上面的实验把这一条单独用原始 JSON 演示。

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `type` | string | 是 | 判别字段；决定 payload 怎么解释、能否进 surface |
| `seq` | 非负整数 | 是 | 事件在日志里的位置；必须密集，等于日志长度 |
| `time` | 整数（毫秒，可为负） | 是 | 记录时的墙上时间；**不检查单调性**，同一毫秒内两个事件是正常的 |
| `data` | JSON | 是 | 类型相关的 payload；本包保持原始 JSON |
| `ignorable` | 字面 `true` | 否 | 标记“读取器遇到不认识的类型可以安全跳过”；缺席表示必需 |
| `surfaceOp` | `"append"` 或 `{op,startSeq,endSeq}` | 条件必填 | 只有会产生模型消息的五个类型可以带；`append` 不得带范围，`replace` 的 key 集必须恰好三个 |
| `sourceEventSeqs` | 非负整数数组 | 否 | 本条事件引用的更早事件；非空、无重复、必须全部更早、必须覆盖替换范围里的每个节点 |

### 五个会进 surface 的类型

`system/message`、`developer/message`、`user/message`、`assistant/message`、`tool/result`。只有这五个可以带 `surfaceOp`；其他类型（`turn/start`、`turn/end`、`step/start`、`step/end`、`assistant/attempt`、`tool/call`、`request/header`、`request/context`、`session/end-seed`）都不得带。这条约束是双向的：漏了标记会让一条消息对历史不可见，多带了标记会让一条审计记录污染历史。

### 未知事件政策

两层，缺一不可：

| 情况 | 处置 | 理由 |
| --- | --- | --- |
| 类型未知 + 无 `ignorable` | **拒绝整份日志** | 一个不认识的必需事件可能改变其余日志的解释方式；静默跳过会重建出被掏空的会话 |
| 类型未知 + `ignorable: true` | 接受，原样保留 payload | 写入方明确声明它不影响重建 |
| 类型已知 | 按已知规则解释 | `ignorable` **不**给已知类型豁免 |

最后一行容易被忽略：一个已知的 log-only 事件即使被标记 `ignorable`，也**不**允许带 `surfaceOp`。豁免只针对“本 build 不认识的类型”。

## 一次操作的时序与所有权

`UnmarshalEvent` 的路径决定了拒绝落在哪里：

1. 解析成一个 `map[string]json.RawMessage`，逐键检查是否属于七个合法字段；不属于就拒绝。
2. 读 `type`、`seq`、`time`、`data`，各自做类型检查；缺失或多态就拒绝。
3. 读可选的 `ignorable`（只接受 `true`）、`surfaceOp`（字符串或恰好三个键的对象）、`sourceEventSeqs`（整数数组）。
4. 返回事件；payload 是**独立拷贝**，不引用调用方的缓冲区。

`Append` 的路径决定提交边界：

1. 回入检查与类型分类（未知且无标记 → 拒绝）。
2. 在状态锁内构造事件：`seq = len(events)`、取时间、**拷贝** payload 与引用。
3. `ValidateEventEnvelope`：payload 可无损序列化、envelope 字段齐全、surface 元数据与引用规则。
4. 跑全部 `OnValidate`；任一失败就返回，**日志未变**。
5. 写入切片，然后跑 `OnEvent` 观察者；观察者的 panic 被恢复成错误并记录，**不影响成功返回**。

谁持有状态：`Log` 持有事件切片与回调列表；`Event` 是值类型，返回给调用方的是拷贝。本包不启动 goroutine、不使用 `context`：取消不属于日志。

## 失败与定位

**一、`session: invalid event: unknown envelope field "turn"`。**
症状：解码一份带额外字段的 envelope 失败。原因：envelope 是定形的七个字段；`turn`/`step` 属于 payload。诊断入口：`UnmarshalEvent` 的键检查。恢复：把该字段移到 `data` 里。**不要**为了让它通过而放宽字段集合——那会让“envelope 是什么”变成依赖输入。

**二、`session: invalid event: "user/message" is surface-eligible and requires a surfaceOp`。**
症状：写入一条消息事件却没有声明它怎么进 surface。原因：surface 是模型历史的唯一来源；一条消息不声明位置就无法被重建。恢复：传 `SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}`。反向错误（log-only 事件带了标记）报 `cannot carry surfaceOp`，恢复是删掉标记。

**三、`sourceEventSeqs must include every shadowed surface node; missing 3`。**
症状：替换范围 `[1,3]` 但只引用了 `[1]`。原因：替换必须能追溯到它遮蔽的**每一个**节点，否则压缩之后无法解释内容去了哪里。恢复：把范围里的每个序号都列进 `SourceEventSeqs`；或者缩小替换范围。

**四、`session: unknown event type is not ignorable: "future/note"`。**
症状：写入或读取一个本 build 不认识的类型。原因与恢复见[事件溯源](go-primer.md#失败与定位)的第二条。关键点：**不要**为了让数据通过而把类型塞进 `KnownTypes()`——那等于声称本 build 能解释它。

区分四类：**预期等待**（并发 append 排队）、**无效输入**（上面四条与全部 envelope 校验）、**执行失败**（`OnValidate` 的错误原样返回且不留痕）、**释放失败**（本包没有资源，无此类）。

## 本切片未实现的部分

必须如实写明，不能读成已完成：

- **payload 级校验。** 参考在 append 前还检查 `request/header` 的空字段（`header.system` 必须省略、空的 `tools`／`adapterDefaults` 必须省略）、`developer/message` 的 `headerSeq`（恰在有工具新增时出现）、`tool/result` 的 `error` 只在 `isError: true` 时允许。本包已保留这些类型的分类与 envelope 校验，但**尚未实现这三条**。它们断言的是请求头引用与工具错误语义，消费者在 M4／M8 才出现。
- **surface 节点列表。** 本包校验 `surfaceOp` 的形状与引用规则，但**不维护**有序节点列表——那需要知道全部已有事件，属后续切片接入的组件。
- **磁盘与恢复。** 没有文件、句柄、flush、单写者或崩溃尾部策略。属 M6。
- **格式互读。** 编解码对照的是 envelope 的语义，不是 DSH 的会话文件格式。
- **`session-projection` 客户端读模型。** 属 M11。

## 回到源码与下一步

建议的阅读顺序：

1. [session/types.go](../../session/types.go) 的 `Event`——先看字段集合为什么必须固定。
2. [session/event.go](../../session/event.go) 的 `validateSurfaceMetadata` 与 `validateSources`——双向约束与引用规则。
3. [session/codec.go](../../session/codec.go) 的 `UnmarshalEvent`——键检查与严格的对象形状。
4. [session/registry.go](../../session/registry.go) 的 `Classify` 与 `KnownTypes`——未知事件政策。
5. 对照 `packages/core/session/src/index.ts` 的 `assertSessionEventEnvelope`。

对应测试：`TestEnvelopeRejectsUnknownFields`、`TestEnvelopeRequiresMandatoryFields`、`TestReplaceSurfaceOpRejectsAliasedObjects`、`TestSurfaceEligibilityRules`、`TestSourceCitationRules`、`TestUnknownEventPolicy`、`TestIgnorableMarkerRoundTrip`、`TestLogCodecRoundTripIsByteExact`、`TestEventCloneIsDetached`。

读完你应该能回答：**这个机制解决什么问题**——把“发生过什么”编成一个可逐字节比较、可拒绝坏数据的定形记录；**谁拥有资源**——`Log` 拥有切片，事件是值，payload 在写入时被拷贝；**为什么这样表达**——字段集合必须固定（否则 envelope 的含义依赖输入），`ignorable` 必须默认“必需”（否则会重建错误历史），`ignorable` 只接受 `true`（否则读取器要调和两种“不跳过”的表达）。

下一步是 surface 与投影（`docs/session/projection.md`，由后续切片创建）：这些事件怎样按顺序变成模型历史，以及审计事件如何做到“日志变长但历史不变”。
