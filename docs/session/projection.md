# 模型 surface 与消息投影

## 读完要解决的问题

你已经有一份权威事件日志（见[事件溯源](go-primer.md)与 [envelope 参考](event-log.md)）。接下来的问题是：**日志怎样变成模型输入**，以及**审计事件怎么做到“日志变长但历史不变”**。

这是整个 M3 的核心。有一个容易踩的坑：把“模型历史”实现成“按类型过滤日志”。那样能跑，但会在两处出错——失败模型尝试会产生一条不该有的空消息；一次内容改写（比如把图片换成占位文本）会没有地方落。这一篇给出**两层**机制：**surface**（哪些事件按什么顺序变成消息）与**消息投影**（某条已有消息的内容被改写）。

前置知识：Go 的切片、map、接口与 JSON 编解码。关联行为见 [M3 执行契约](../10-plans/dsh-go-replication-m03/plans.md)。

## 从熟悉的 Go 写法出发

普通 Go 做法是按类型过滤：

```go fragment
func history(events []Event) []Message {
	var out []Message
	for _, event := range events {
		if isMessageEvent(event.Type) {
			out = append(out, decode(event))
		}
	}
	return out
}
```

它简单、直观。三个边界：

1. **顺序只是日志顺序。** 正常情况下没问题，但压缩会把一段历史替换成一条摘要。替换之后“模型看到什么”不再等于“日志里有哪几条消息事件”。
2. **空的 system／assistant 消息会变成空消息。** 一个空的 system 渲染表示“本次没有 system prompt”；一个空 assistant 消息承载 `max-tokens` 步骤的 usage。它们都**存在**于日志里，却**不该**产生一条 wire 消息。
3. **内容改写无处安放。** 图片被 offload 之后，模型应该看到占位文本；但产生这个决定的是一条独立事件，它不是一条新消息。

DSH 用两层解决：

- **surface**：一份**有序的事件位置列表**，只包含会产生模型消息的那五类。`append` 加在尾部，`replace` 用一条事件遮蔽一段现有节点。
- **消息投影**：一个事件类型对应一个**纯解释器**，它改写**已有**节点的消息内容，不能增删节点。

两者合起来给出那句关键性质：**审计事件让日志变长、让 surface 不变**。

哪里不等价：参考的 surface 维护是增量的（`SurfaceManager` 带 watermark 与 pending plan），本包同时提供**离线 fold**（`FoldSurface`，从完整日志重放）与**实时 surface**（`Log` 在 append 时同步维护）。两者共用全部规则，并有测试钉住它们一致。这是 Go 侧的显式选择：参考为一个单线程运行时优化，本包必须让两条路径可比较。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面的完整程序注册一个把图片换成占位文本的投影，然后打印 surface 与模型历史。

```go runnable=session-projection
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
)

// redactProjection replaces the content of one existing message with a
// placeholder, keeping its identity. It stands in for a real interpreter such as
// the offload plugin's.
type redactProjection struct{ placeholder string }

// Type reports the event type this interpreter owns.
func (redactProjection) Type() string { return "image/offload" }

// Project rewrites the target message's content.
func (p redactProjection) Project(event session.Event, context session.ProjectionContext) (map[session.Seq]json.RawMessage, error) {
	var payload struct {
		Target session.Seq `json:"target"`
	}
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, err
	}
	var target session.Event
	for _, candidate := range context.Events {
		if candidate.Seq == payload.Target {
			target = candidate
		}
	}
	message, err := llm.UnmarshalMessage(target.Data)
	if err != nil {
		return nil, err
	}
	rewritten, err := llm.NewMessage(message.Role, []llm.ContentBlock{llm.TextBlock{Text: p.placeholder}}, message.Source)
	if err != nil {
		return nil, err
	}
	rewritten.ID = message.ID
	encoded, err := llm.MarshalMessage(rewritten)
	if err != nil {
		return nil, err
	}
	return map[session.Seq]json.RawMessage{payload.Target: encoded}, nil
}

func user(seq session.Seq, id string) session.Event {
	data, _ := json.Marshal(map[string]any{"id": id, "role": "user",
		"content": []any{map[string]any{"type": "text", "text": id}},
		"source":  map[string]any{"kind": "user"}})
	return session.Event{Type: "user/message", Seq: seq, Time: session.TimeMS(seq),
		Data: data, SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend}}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	events := []session.Event{
		{Type: "turn/start", Seq: 0, Time: 0, Data: []byte(`{}`)},
		user(1, "u1"),
		user(2, "u2"),
		{Type: "image/offload", Seq: 3, Time: 3, Data: []byte(`{"target":1}`)},
	}
	surface, err := session.FoldSurface(events, redactProjection{placeholder: "[image omitted]"})
	if err != nil {
		return err
	}
	fmt.Printf("surface nodes=%v replacements=%d content-generation=%d\n",
		surface.Nodes, surface.ReplaceGeneration, surface.ContentGeneration)
	messages, err := surface.DeriveMessages(events)
	if err != nil {
		return err
	}
	for _, message := range messages {
		fmt.Printf("message id=%s text=%q\n", message.ID, llm.ContentText(message.Content))
	}
	return nil
}
```

预期输出：

```text output=session-projection
surface nodes=[1 2] replacements=0 content-generation=1
message id=u1 text="[image omitted]"
message id=u2 text="u2"
```

三行是重点：

- `surface nodes=[1 2]`：`image/offload` 事件**没有**加入 surface（它不在那五类里），也没有带走任何节点——投影是**改写**，不是替换。
- `content-generation=1` 而 `replacements=0`：投影改变了**已有内容**，但没有发生位置替换。这两个计数器是不同的信号，派生缓存必须两个都看。
- `text="[image omitted]"`：模型看到的是改写后的内容，而 `id=u1` 说明这条消息还是原来那一条。

## 先预测，再观察

先做一个预测：把 `turn/start`、`assistant/attempt` 这类审计事件插到消息之间，模型历史会变吗？

```go fragment
events := []Event{
	{Type: "turn/start", Seq: 0, Time: 0, Data: []byte(`{}`)},
	user(1, "u1"),
	{Type: "assistant/attempt", Seq: 2, Time: 2, Data: []byte(`{}`)},
	{Type: "turn/end", Seq: 3, Time: 3, Data: []byte(`{}`)},
}
```

预测之后运行：

```sh
go test -run TestAuditEventsLengthenTheLogWithoutChangingHistory -v ./session
```

答案：历史**完全相同**——都是那一条 `u1`。但日志从 1 条变成 4 条，而 surface 仍然只有一个节点。这个测试同时断言了这两件事，因为只断言“历史一样”会漏掉“日志确实变长了”。

再预测一次：一条**内容为空**的 system 消息会怎样？

它**占据一个 surface 节点**，但**不产生**模型消息。用 `go test -run TestEmptyMessagesKeepTheirPlaceButDeriveToNothing -v ./session` 验证。这不是优化，是行为：正式参考里空 system 消息就是“本次没有 system prompt”的记录方式；如果把它当成一条空消息下发，会凭空插入一个空的 system turn。

最后一次预测：一个**必须**有解释器的类型（本包里是 `image/offload`）在没有注册解释器时会怎样？

答案是**整份日志被拒绝**（`requires a message projection`），不是被跳过。用 `go test -run TestRequiredProjectionIsRefusedWithoutAnInterpreter -v ./session` 验证。理由：跳过它意味着重建出的历史**缺了一次内容改写**，那和“历史正确”是两件事。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | surface 是“会产生 LLM 消息的事件位置”的有序视图；只有五类事件可以带 `surfaceOp`；`append` 加尾、`replace` 遮蔽一段现有节点；消息投影是某个事件类型的**纯**解释器，返回被改写消息的 map |
| TypeScript 入口 | `packages/core/session/src/surface.ts`（`foldSurface`、`SurfaceManager`、`deriveEventMessage`、`SessionMessageProjection`、`MESSAGE_PROJECTION_EVENT_TYPES`） |
| Go 入口 | [session/surface.go](../../session/surface.go)（`FoldSurface`、`Surface`、`DeriveMessages`）、[session/projection.go](../../session/projection.go)（`MessageProjection`、`ProjectionContext`、`RequiresProjection`）、[session/log.go](../../session/log.go)（实时 surface 与两个 generation） |
| 保持的行为 | 只有 surface 节点产生消息；空内容消息占位但不产生消息；`replace` 的端点必须是**当前** surface 节点且严格更早；必须引用全部被遮蔽节点；工具结果改写只能改内容；节点 0 在承载 system 提示时受保护；缺少必需投影即拒绝整份日志；投影不得增删节点、必须保留消息身份 |
| Go 表达 | 离线 `FoldSurface` 与实时 surface 共用规则；`SurfaceOp` 是带判别字段的结构体；投影接口是 `Project(event, context) (map[Seq]json.RawMessage, error)`，返回原始 JSON 以避免接口值序列化问题 |
| 不等价边界 | 见下表 |

### 参考里同名但不同的两套机制

这是复刻时最容易混淆的一点，必须写清楚：

| 机制 | 位置 | 产生模型历史吗 |
| --- | --- | --- |
| `foldSurface` + `SessionMessageProjection` | `packages/core/session/src/surface.ts` | **是**，`deriveMessages()` 的唯一来源 |
| `ProjectionDefinition` 注册表 | `packages/session/session-projection` | **否**，它提供宿主与客户端**读模型** |

本切片实现的是**前者**。后者的客户端读模型属 M11。M3 细化方案把两者并称为“投影”，本执行契约已在主题页做出裁决并记录了理由。

### fold 的失败条件

全部来自参考，逐条有测试：

| 条件 | 处置 |
| --- | --- |
| 位置不连续（间隙或重复） | 拒绝 |
| surface 事件缺少 `surfaceOp` | 拒绝 |
| log-only 事件携带 `surfaceOp`／`sourceEventSeqs` | 拒绝 |
| 未知**且**带 `ignorable` 的记录 | 保留，**永不影响 surface**（即使它带了元数据） |
| 替换端点不在**当前** surface 上 | 拒绝 |
| 替换端点不早于自身 | 拒绝 |
| `startSeq` 在 surface 顺序上晚于 `endSeq` | 拒绝 |
| `sourceEventSeqs` 未覆盖全部被遮蔽节点 | 拒绝 |
| 工具结果改写跨越 ≠1 个节点、目标不是当前工具结果、或改了内容以外的字段 | 拒绝 |
| 替换覆盖节点 0 且该节点是 system 消息，而本条不是恰好覆盖该节点的 system 消息 | 拒绝 |
| 必需投影未注册 | 拒绝 |

第二条与第三条是**双向**的：漏了标记会让消息对历史不可见，多带了标记会让审计记录污染历史。

## 一次操作的时序与所有权

实时路径（`Log.Append`）的所有者很清楚：

1. 事件通过 `buildLocked` 的 envelope 校验。
2. **在锁内**把当前 surface 复制一份，把候选事件接在日志副本尾部，让副本 `apply` 一次。失败就返回，**日志与 surface 都没变**。
3. 跑 `OnValidate`。
4. 写入事件切片，并用第 2 步验证过的 surface 替换当前 surface——两者在同一个临界区里一起更新。
5. 跑 `OnEvent` 观察者。

第 2 步的“先规划、后提交”是关键：surface 校验必须发生在写入**之前**，否则会存在“事件在日志里但 surface 解释不了它”的状态。

谁持有状态：`Log` 持有事件切片与 `Surface`（节点、两个 generation、投影缓存）；`Surface` 在离线路径上是纯值。投影是**借用**的——注册返回一个移除函数，移除后同一份日志会在下一次 fold 时被拒绝（这正是“所属插件不在了”的可检测形式）。

## 失败与定位

**一、`session: invalid event: "image/offload" requires a message projection`。**
症状：读取或写入一份含该事件的日志失败。原因：它有模型可见的效果，但本 build 没有解释器。诊断入口：`RequiresProjection` 与 `FoldSurface`。恢复：注册该事件类型的 `MessageProjection`；如果这份日志来自更新版本，而该类型确实不影响重建，那么**写入方**应该标记 `ignorable`——而不是让读取方放宽。

**二、`session: invalid event: surface replace: start seq 3 not found in surface`。**
症状：压缩想遮蔽的区间里有一个位置**已经被更早的替换遮掉了**。原因：端点的合法条件是“是**当前** surface 节点”，不是“在日志里存在”。诊断入口：`FoldSurface` 的端点查找。恢复：改成引用当前 surface 上的节点；这通常意味着压缩范围要基于**上一次压缩之后**的 surface 计算。

**三、`session: invalid event: surface replace: node 0 holds the system prompt ...`。**
症状：一个压缩区间覆盖了节点 0，而节点 0 是 system 消息。原因：system 提示受保护，只有“恰好覆盖该节点的一条 system 消息”可以改写它，否则压缩会悄悄抹掉指令。诊断入口：`checkSystemHeadRewrite`。恢复：把区间从 1 开始，或者用一条新的 system 消息精确替换节点 0。

**四、`session: invalid event: tool/result surface replacement may change only content`。**
症状：工具结果改写改了 `callId` 或 `isError`。原因：改写只能改“结果说了什么”，不能改“它回答哪个调用、是否失败”。恢复：只改 `message.content`。

区分四类：**预期等待**（无）、**无效输入**（上面四条与全部 surface 规则）、**执行失败**（投影返回的错误会原样中止这次 fold／append，且不留痕）、**释放失败**（没有资源要释放）。

## 本切片未实现的部分

- **压缩本身。** 本包实现 `replace` 的语义与全部约束，但没有产生压缩区间的组件。属 M8。
- **真实的 `image/offload` 投影。** 测试里用的是一个等价的替身；图片附件服务不存在，所以真实的投影没有归属。属 M8。
- **`session-projection` 客户端读模型。** 属 M11。
- **增量 watermark 与持久化缓存。** 参考用 watermark 与 checkpoint 行加速从磁盘恢复；本包没有磁盘，所以只有一个内存 fold。属 M6。
- **`request/header` 的 payload 级校验。** 见 [envelope 参考](event-log.md#本切片未实现的部分)。

## 回到源码与下一步

建议的阅读顺序：

1. [session/surface.go](../../session/surface.go) 的 `Surface.apply`——先看“log-only 事件直接返回”那一行，它单独解释了审计事件为何不进历史。
2. `DeriveMessages` 与 `DeriveEventMessage`——空内容为什么返回 nil。
3. `checkToolResultRewrite` 与 `checkSystemHeadRewrite`——两条最容易被忽略的保护规则。
4. [session/projection.go](../../session/projection.go) 的 `MessageProjection` 与 `RequiresProjection`。
5. 对照 `packages/core/session/src/surface.ts` 的 `planSurfaceEvent`、`applySurfacePlan` 与 `deriveEventMessage`。

对应测试：`TestAuditEventsLengthenTheLogWithoutChangingHistory`（日志变长、历史不变）、`TestEmptyMessagesKeepTheirPlaceButDeriveToNothing`（空内容占位不产出）、`TestReplacementShadowsOnlyItsRange`（替换与遮蔽报告）、`TestSystemHeadIsProtected`（system 头保护）、`TestToolResultReplacementChangesOnlyContent`（只能改内容）、`TestRequiredProjectionIsRefusedWithoutAnInterpreter`（缺解释器即拒绝）、`TestLiveLogComputesTheSameSurfaceAsAFold`（离线与实时一致）、`TestGenerationsDistinguishGrowthFromRewrite`（两个 generation 的信号）。

读完你应该能回答：**这个机制解决什么问题**——把“发生过什么”和“模型看到什么”分开，并让内容改写有落脚点；**谁拥有资源**——`Log` 拥有日志与 surface，投影是借用并随所有者释放；**为什么这样表达**——历史必须来自 surface 而非类型过滤（否则替换与空消息都错），投影必须只改写不增删（否则它就能绕开 surface 的位置声明）。

下一步是 M4：把这份数据契约接到 fake 与 tRPC 模型提供方，并建立工具管线。
