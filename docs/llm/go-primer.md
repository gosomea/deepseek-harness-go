# 从 Go 理解对话消息：为什么一条 assistant message 不等于一次 HTTP chunk

## 读完要解决的问题

你写过一个结构体，用 `encoding/json` 系列化它，也定义过接口来让不同的实现满足同一个契约。现在的问题是：当你说“模型回了一句话”时，这句话在内存里到底是什么？

你的第一反应可能是 `string`，或者收 HTTP 流时把每个 chunk 直接拼起来。这在只做一次对话时够用。它会崩在四个地方：模型说的话可能包含“想过但没说出口”的 reasoning；模型可能请求调用工具，而工具参数是**分片**到达的；一次失败的重试不应该假装成一次成功的回答；同一条消息要同时被存储、被重放、被送去下一次请求。用 `string` 表示，这四件事都无处安放。

这一篇从这个问题进入 DSH 的 **Message** 与 **ContentBlock**。前置知识是 Go 的结构体、接口、`encoding/json` 与错误包装。关联行为见 [M3 执行契约](../10-plans/dsh-go-replication-m03/plans.md)；当前实现范围是消息词汇与编解码，流与组装属 M4。读完你能解释：为什么消息既要是一个结构体、又要有一层接口，以及这个选择让什么行为成为可能。

固定参考：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`，`packages/llm/llm/src/message.ts` 与 `packages/llm/llm/src/types.ts`。

## 从熟悉的 Go 写法出发

最直白的表示是一条消息一个字符串：

```go fragment
type Message struct {
	Role    string
	Content string
}
```

它清楚、能序列化、够用。边界在于 `Content string` 把**所有内容压成一个维度**。reasoning 和可见文本混在一起，工具调用只能塞进字符串再靠正则解析出来，图片和文件根本无法表达。更麻烦的是：一旦为了区分这些而加上可选字段，每个字段都变成“有时有效”——

```go fragment
type Message struct {
	Role       string
	Text       string
	Reasoning  string // 只有 assistant 可能有
	ToolCallID string // 只有 tool 可能有
	ImagePath  string // 只有 user 可能有
}
```

调用方再也无法从类型看出哪个字段在什么时候有意义，校验散落在每个使用者里。

DSH 的答案是把内容拆成**有判别字段的块列表**，并让消息本身携带角色：

- **Message（消息）**：`id`、`role`、`content`（块的列表）、`source`（谁产生的）。
- **ContentBlock（内容块）**：带 `type` 判别字段的联合，当前的种类是 `text`、`reasoning`、`image`、`file`、`tool-call`、`tool-addition`、`tool-removal`。

关键不是“拆得多”，而是**每条信息的归属清楚**：想说的话进 `text` 块，想过的话进 `reasoning` 块，要调工具进 `tool-call` 块，而块与块之间是有序的。一次回答因此可以同时是“一段 reasoning + 一段文本 + 一个工具调用”，且顺序确定。

哪里不等价：DSH 的 `ContentBlock` 是 TypeScript 的 **merge-extensible union**——插件可以通过 `declare module` 给自己的块类型加一个成员。Go 没有声明合并，所以本包用**接口加兜底类型**表达：已知块是具体结构体，未知块是保留原文的 `UnknownBlock`。差异在[内容块参考](content-blocks.md#未知内容怎样处理)逐条说明。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面的完整程序构造一条完整的 assistant 消息（含 reasoning、文本与工具调用），打印它的结构，再做一次无损往返。

```go runnable=llm-primer
package main

import (
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/llm"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	message, err := llm.NewAssistantMessage("deepseek", "v4", []llm.ContentBlock{
		llm.ReasoningBlock{Text: "the user asked for a file"},
		llm.TextBlock{Text: "reading it now"},
		llm.ToolCallBlock{ID: "call-1", Name: "read", Arguments: `{"path":"a.txt"}`},
	})
	if err != nil {
		return err
	}
	// A fixed id keeps this program's output identical on every run.
	message.ID = "msg-1"

	fmt.Printf("role=%s source=%s route=%s/%s blocks=%d\n",
		message.Role, message.Source.Kind, message.Source.Provider, message.Source.Model, len(message.Content))
	for index, block := range message.Content {
		fmt.Printf("  block[%d] type=%s\n", index, block.BlockType())
	}
	fmt.Printf("visible text=%q reasoning kept separate=%t\n",
		llm.ContentText(message.Content), message.Content[0].BlockType() == llm.BlockTypeReasoning)

	encoded, err := llm.MarshalMessage(message)
	if err != nil {
		return err
	}
	fmt.Println("encoded:", string(encoded))

	decoded, err := llm.UnmarshalMessage(encoded)
	if err != nil {
		return err
	}
	again, err := llm.MarshalMessage(decoded)
	if err != nil {
		return err
	}
	fmt.Printf("round trip byte-identical=%t\n", string(again) == string(encoded))
	return nil
}
```

预期输出：

```text output=llm-primer
role=assistant source=model route=deepseek/v4 blocks=3
  block[0] type=reasoning
  block[1] type=text
  block[2] type=tool-call
visible text="reading it now" reasoning kept separate=true
encoded: {"id":"msg-1","role":"assistant","content":[{"type":"reasoning","text":"the user asked for a file"},{"type":"text","text":"reading it now"},{"type":"tool-call","id":"call-1","name":"read","arguments":"{\"path\":\"a.txt\"}"}],"source":{"kind":"model","model":"v4","provider":"deepseek"}}
round trip byte-identical=true
```

## 先预测，再观察

上面那行 `encoded:` 值得仔细看，因为它的字段顺序**不完全**是你按代码顺序期望的样子。

先写出你的预测：`content` 数组里 `reasoning`、`text`、`tool-call` 三块会按什么顺序出现？`source` 里又会先出现哪个字段？

实际结果：块的顺序与内容切片一致（`reasoning`、`text`、`tool-call`），这符合预期。但**每个对象内部**的字段顺序由实现决定，不是按你写代码时的顺序：

- 每个块以 `type` 开头，其后按该 Go 结构体的**字段声明顺序**：`tool-call` 是 `id`、`name`、`arguments`。
- `source` 也是 `kind` 开头，但其余字段是 **`model` 在 `provider` 之前**——按字母序，不是按 `MessageSource` 的声明顺序。

原因是两条不同的编码路径：已知块用 `json.Marshal` 直接编结构体（字段声明序），把 `type` 拼接到最前；`source` 因为有 `Extra` 兜底字段、必须支持未知字段，走的是 `map[string]json.RawMessage` 加排序的路径。**关键在于两条路径都是确定的**——同一个值编码两次得到相同字节。

现在改一个条件验证“确定”这件事：在 `llm/codec_test.go` 里把 `TestMarshalIsDeterministic` 的循环次数从 20 改成 200，再运行

```sh
go test -run TestMarshalIsDeterministic ./llm
```

它仍然通过。把 `MessageSource.Extra` 换成更多字段，也仍然通过：排序保证了与插入顺序无关。如果哪天有人在编码路径里引入未排序的 map 迭代，这个测试会立刻失败，而失败信息会直接打印两组不同的字节。

恢复方式：把循环次数改回 20。

再预测一次：**未知**块走的是哪条路径？把这一句加进上面的程序：

```go fragment
blocks, err := llm.UnmarshalContent([]byte(`[{"type":"audio","seconds":3,"transcript":"hi"}]`))
```

答案：**两条都不走**。未知块保留原文，`MarshalContent` 发现原文自带的 `type` 与声明的类型一致时直接输出原字节，因此字段顺序**就是你读到的顺序**，逐字节不变。这是刻意的：一个更新的写入方产生的记录，不能被一个旧的读取器重排或丢失字段。已知种类的未知**字段**则不同——它们按已知结构体自己的字段重编码，不保留。完整规则见[内容块参考](content-blocks.md#未知内容怎样处理)。

最后预测一次：把 `llm.TextBlock{Text: "reading it now"}` 改成 `llm.TextBlock{}`（空文本），`ContentText` 会输出什么？

答案仍然是 `""`，但这次是因为那一块**存在**、只是文本为空。这跟 `llm.TextContent("")` 不一样——后者返回 `nil`，表示“没有内容”。这个区别很重要：后面 Session 的投影规定，**内容为空的消息存在但不产生模型可见消息**。空 system 消息就是这样记录“本次没有 system prompt”的。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | `Message`（角色 + 内容块 + source）与 `ContentBlock`（带 `type` 判别的联合）；`MessageSourceMap` 区分 user／model／tool／system-prompt 生产者；`createXxxMessage` 构造并冻结 |
| TypeScript 入口 | `packages/llm/llm/src/message.ts`（`MessageBase`、`MessageRoleMap`、`MessageSourceMap`、`createMessage`）、`packages/llm/llm/src/types.ts`（`ContentBlockMap`、`TextBlock`…`ToolRemovalBlock`） |
| Go 入口 | [llm/content.go](../../llm/content.go)（`ContentBlock` 接口与七个块）、[llm/message.go](../../llm/message.go)（`Message`、`MessageSource`、`NewXxxMessage`）、[llm/codec.go](../../llm/codec.go)（`MarshalMessage`／`UnmarshalMessage`） |
| 保持的行为 | 角色集合封闭；assistant 必须写明 provider 与 model；tool 消息必须两处记录同一个 call id；`arguments` 保持原始字符串；空内容的消息存在但 `Empty()` 为真；编码确定且逐字节往返 |
| Go 表达 | 消息用结构体加 `Role` 判别字段（存储层需要单一可识别形状）；内容块用接口加 `UnknownBlock` 兜底（词汇需要增长）；`MessageID` 是 `string` 而不是带格式校验的类型 |
| 不等价边界 | TS 的 merge-extensible union 与声明合并无法照搬；TS 的 `deepFreeze`＋`structuredClone` 换成 Go 的值语义与显式深拷贝；TS 的 brand 是编译期身份转换，Go 的 `MessageID` 也不校验格式 |

### 为什么消息是结构体、内容块是接口

两者的约束不同，所以表达不同。

**消息**必须能被一个不认识它的读取器**保留**，同时解码器必须能**拒绝**缺少角色必需字段的消息。结构体加判别字段正好：`encoding/json` 直接检查字段，而 `Role` 决定哪些字段有意义。用接口为每个角色建一个类型也可以，但那样“未知角色”就没有落脚点。

**内容块**面对词汇增长。参考实现的做法是：未知块在**存储层存活**，由真正需要语义的边界（提供方适配器）拒绝。接口加兜底类型能在 Go 里表达同一件事——`UnknownBlock` 保留原文，重新编码逐字节还原。注意这是“未知**种类**保留”，已知种类上的未知字段会按该类型自己的字段重编码，见[内容块参考](content-blocks.md#未知内容怎样处理)。

## 一次操作的时序与所有权

一次构造与往返的所有者很清楚：

1. `NewAssistantMessage` 调用 `NewMessage`，后者用 `NewMessageID` 取一个随机 UUID，填好角色与 source，然后调用 `Validate`。
2. `Validate` 检查角色与 source 种类是否自洽、必填字段是否齐全、内容块是否各自完整。任何一项失败返回包装了 `ErrInvalidValue` 的错误，**不返回半成品**。
3. 调用方拿到的是**值**（`Message`），不是指针。Go 的切片字段仍与调用方共享底层数组，所以 `MarshalMessage` 前会重新 `Validate`；而 `session` 在把值写进日志时会做自己的拷贝与冻结（见 C07 的事件日志 `docs/session/go-primer.md`）。
4. `MarshalMessage` 先校验，再分别调用内容与 source 的编码器，最后拼成对象。
5. `UnmarshalMessage` 走反向路径：先解出原始字段，再让内容与 source 各自解码并校验，最后整体校验。

谁持有状态：`Message` 是值，没有隐藏生命周期；`UnknownBlock.Raw` 持有原始 JSON 的**独立拷贝**，所以调用方的缓冲区改变不影响它。没有 goroutine、没有锁、没有 `context`——本包是纯函数。

## 失败与定位

三个关键失败：

**零、先弄清楚本包**会不会**拒绝什么。** 固定参考在运行期**不**校验 payload：那些规则写在它的类型系统里，运行期只在 Session 的入日志边界生效。所以本包只复刻参考真正会拒绝的四类（缺 id、未知 role、role 与 source 不符、tool 消息两处 call id 不一致），其余一律接受。下面第一、二条正是这四类；第三条是形状错误。

**一、`llm: invalid value: assistant message requires a model source`。**
症状：用 `NewMessage(llm.RoleAssistant, ..., llm.MessageSource{Kind: llm.SourceUser})` 构造时失败。原因：角色与生产者归属必须一致——一条 assistant 消息必然来自某个模型路由，而路由必须可追溯。诊断入口：[llm/message.go](../../llm/message.go) 的 `Message.Validate`。恢复：给 assistant 用带有 `Provider` 与 `Model` 的 `SourceModel`；或者如果你确实在注入内容，用 `RoleUser` 加一个 producer 归属。

**二、`llm: invalid value: tool message source callId "call-1" does not match toolCallId "call-2"`。**
症状：工具结果无法与工具调用配对。原因：call id 在**两处**记录（`Message.ToolCallID` 与 `Source.CallID`），构造时一致性由 `Validate` 检查；手工改了一个字段就会失败。诊断入口：同一个 `Validate`。恢复：用 `NewToolResultMessage(callID, ...)` 构造，它会同时填两处。

**三、`llm: invalid value: block "text": json: cannot unmarshal number into Go struct field ... of type string`。**
症状：读取一份字段类型不对的日志时解码失败。原因：已知块的解码是**严格**的，一个被改名的字段不会被默默当成零值。诊断入口：`UnmarshalContent` 的错误链，它带块序号与判别字段。恢复：修正数据源；如果数据来自更新的写入方，那它应该用一个**未知判别字段**（那才会被 `UnknownBlock` 保留），而不是给已知块塞新字段。

区分清楚这四类：**预期等待**（本包没有）、**无效输入**（`ErrInvalidValue`）、**执行失败**（本包不执行任何东西）、**释放失败**（本包没有资源）。本包只产生第二类。

## 回到源码与下一步

建议的阅读顺序：

1. [llm/content.go](../../llm/content.go) 的 `ContentBlock` 接口与七个块——先建立“内容是有序的、带判别的块”这一认识。
2. [llm/message.go](../../llm/message.go) 的 `Message.Validate`——看角色与 source 如何互相约束。
3. [llm/codec.go](../../llm/codec.go) 的 `UnmarshalContent` → `decodeBlock` → `UnknownBlock`——看词汇增长如何不被损失。
4. 对照 `packages/llm/llm/src/message.ts` 的 `MessageBase` 与 `MessageRoleMap`。

对应测试：`TestUnknownBlockSurvivesRoundTrip`（未知种类逐字节保留）、`TestContentRoundTripIsLossless`（已知块往返）、`TestMessageValidateRequiresRoleConsistency`（角色与 source 自洽）、`TestMarshalIsDeterministic`（编码确定）、`TestToolMessageRequiresMatchingCallID`（call id 两处一致）。

读完你应该能回答：**这个机制解决什么问题**——把“模型说了什么”拆成有归属、有顺序、可无损存储的值；**谁拥有资源**——没有资源，消息是值；**为什么这样表达**——存储层需要单一可识别形状用结构体，词汇增长需要保留未知用接口。

下一篇是[内容块详细规则](content-blocks.md)，然后是 C07 事件溯源（`docs/session/go-primer.md`，由 M3 后续切片创建）：这些值怎样进入一份只能追加的日志。
