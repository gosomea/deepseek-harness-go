---
description: "定义消息角色、内容块与工具调用／结果这一套对话词汇，并用无损 JSON codec 让未知内容原样留存。"
kind: "package-library"
---

# llm

## 概述

`llm` 回答一个具体问题：一次模型请求和一次模型回答里，究竟有哪些**值**？它给出消息角色、内容块（文本、reasoning、工具调用、图片、文件）、生产者归属（source）与 token 计量（usage），并提供一套无损 JSON codec。

本包不知道 Session、传输或提供方：消息是值，不是日志记录，也不是 HTTP 请求。`session` 负责存储并从中重建历史，提供方适配器负责转换成 wire 格式。当前切片只有词汇与编解码；流、assembler、Provider 与 tRPC 适配器属 M4。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

本包负责三件事：定义消息与内容块的 Go 类型；校验一个值是否可以进入词汇（角色与 source 是否自洽、工具消息是否两处都写明 call id）；在 JSON 之间无损往返。

它不负责：调用模型、解析工具参数、决定谁能看见哪条消息、把消息写进日志。工具调用的 `arguments` 保持模型产出的原始 JSON 字符串，**本包从不解析它**——解析属于知道提供方方言的适配器，而一个畸形的参数字符串必须保持可观察，不能被悄悄替换成空对象。

## 使用

在仓库根目录保持终端位置，把下方完整程序保存为 `bin/llm-demo.go`（先创建 `bin` 目录），执行 `go run ./bin/llm-demo.go`。

```go runnable=llm-vocabulary
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

// withID pins a message's identity so this program prints the same bytes on
// every run. A real caller lets NewMessage mint a random id instead.
func withID(message llm.Message, id llm.MessageID) llm.Message {
	message.ID = id
	return message
}

func run() error {
	user, err := llm.NewUserMessage("read the file")
	if err != nil {
		return err
	}
	user = withID(user, "msg-user")

	// TextContent drops empty text, so an empty prompt yields no block.
	system, err := llm.NewSystemMessage("")
	if err != nil {
		return err
	}
	system = withID(system, "msg-system")

	assistant, err := llm.NewAssistantMessage("deepseek", "v4", []llm.ContentBlock{
		llm.ReasoningBlock{Text: "the user wants a file"},
		llm.TextBlock{Text: "let me look"},
		llm.ToolCallBlock{ID: "call-1", Name: "read", Arguments: `{"path":"a.txt"}`},
	})
	if err != nil {
		return err
	}
	assistant = withID(assistant, "msg-assistant")

	result, err := llm.NewToolResultMessage("call-1", llm.TextContent("hello"), false)
	if err != nil {
		return err
	}
	result = withID(result, "msg-tool")

	for _, message := range []llm.Message{system, user, assistant, result} {
		fmt.Printf("%-9s id=%-14s blocks=%d empty=%t text=%q\n",
			message.Role, message.ID, len(message.Content), message.Empty(), llm.ContentText(message.Content))
	}
	fmt.Printf("route=%s/%s call=%s\n",
		assistant.Source.Provider, assistant.Source.Model, assistant.Content[2].(llm.ToolCallBlock).ID)
	fmt.Printf("tool answers call=%s isError=%t\n", result.ToolCallID, result.IsError)

	encoded, err := llm.MarshalMessage(assistant)
	if err != nil {
		return err
	}
	decoded, err := llm.UnmarshalMessage(encoded)
	if err != nil {
		return err
	}
	again, err := llm.MarshalMessage(decoded)
	if err != nil {
		return err
	}
	fmt.Printf("round trip stable=%t\n", string(encoded) == string(again))
	return nil
}
```

预期输出：

```text output=llm-vocabulary
system    id=msg-system     blocks=0 empty=true text=""
user      id=msg-user       blocks=1 empty=false text="read the file"
assistant id=msg-assistant  blocks=3 empty=false text="let me look"
tool      id=msg-tool       blocks=1 empty=false text="hello"
route=deepseek/v4 call=call-1
tool answers call=call-1 isError=false
round trip stable=true
```

注意空的 system 消息：它**存在**且被保留，但 `blocks=0 empty=true`。后面 Session 的投影正是靠这一点把它判成“不产生模型可见消息”。这条规则不是优化，是行为。

`NewUserMessage` 与 `NewAssistantMessage` 返回校验失败的错误而不是半成品：system 消息必须来自 system-prompt 生产者，assistant 消息必须写明路由，工具消息必须两处都记录同一个 call id。[内容块参考](../docs/llm/content-blocks.md)解释这些规则为什么不能放宽。

## 理解实现

<details>
<summary>为什么内容块是接口而消息是结构体</summary>

两者处理的是不同的问题。

**消息**必须是**在存储层能被认识的一个形状**：一个不认识某个角色的读取器仍要能保留它，而解码器必须拒绝缺少该角色必需字段的消息。用结构体加 `Role` 判别字段就能同时做到，字段缺省也能被 `encoding/json` 直接检查。

**内容块**面对的是**词汇增长**。参考实现允许未知块在持久层存活、由真正需要语义的边界（Provider）拒绝。Go 的接口值不直接序列化，所以本包用 `ContentBlock` 接口加 `UnknownBlock` 兜底：未知判别字段被解码成保留原文的 `UnknownBlock`，重新编码时**逐字节**还原。这解释了一个容易误解的点——未知的**种类**会被完整保留，而已知种类上的未知**字段**会按该结构体自己的字段重编码（不会被保留）。

`MarshalContent` 因此不经过 map 迭代决定输出：已知块由结构体字段顺序决定，未知块直接沿用原文，两次编码同一值得到相同字节。日志要比较和哈希这些字节，随机顺序会让重放不可复现。

</details>

## 进一步阅读

从[消息模型](../docs/llm/go-primer.md)开始：它从 Go 结构体和接口出发，解释一条 assistant message 为什么不能等同于一次 HTTP chunk。之后读[内容块参考](../docs/llm/content-blocks.md)了解每个块的字段与失败规则。

要回到固定参考，看 `packages/llm/llm/src/message.ts`（角色与 source）、`packages/llm/llm/src/types.ts`（内容块与 stream chunk）与 `packages/llm/llm/src/assembler.ts`（分片组装，属 M4）。固定提交为 `00102833dfaee1da9f48a3a8eae9d34005a75218`。

## 验证

从项目根目录运行 `go test -race -count=1 ./llm` 验证本包；阶段交付执行 `make check`。`llm` 已登记在[覆盖门禁](../scripts/doccheck/main.go)的受控清单中，语句覆盖率下限为 90%；上面的完整程序由 `make doc-examples` 执行。行为测试集中在对未知块的无损往返、角色与 source 的自洽规则、以及编码的确定性上。

## 限制

- **没有流**。`StreamChunk`、assembler、增量拼装与 `max-tokens` 时丢弃工具调用都属 M4。本包只有最终值。
- **没有请求**。请求头、路由能力、采样参数与重试边界属 M4。
- **没有模型调用**。本包不联网、没有密钥，也不包含任何提供方。
- **没有解析工具参数**。`arguments` 始终是原始字符串；把它变成结构体是适配器的职责。
- **未知种类保留，但已知种类的未知字段不保留**：后者按该块自己的字段重编码。这是与“保留一切原文”不同的取舍，见[内容块参考](../docs/llm/content-blocks.md#未知内容怎样处理)。
- **图片与文件的字节不在本包**。`ImageAttachmentRef` 与 `FileAttachmentRef` 只是内容寻址的引用（形如 `sha256:<hex>`），存储与读取附件的服务属后续阶段。
- **没有 `tool-choice`／`top_p`／惩罚项**，也没有预填充。参考实现同样只声明 `temperature`／`maxTokens`／`stop`，本切片连这些也没有。

## 与固定参考的实测差异

下面每一条都由对固定提交的实现直接调用得出，不是从 TypeScript 类型推断。参考的 `packages/llm/llm/src` 在运行期**不做** payload 校验——那些规则写在它的类型系统里，运行期只在 `packages/core/session` 的入日志边界生效（`snapshotSessionEvent`）。因此本包的 `Validate` 复刻的是**参考真正会拒绝的东西**，而不是它类型上不允许的东西。

| 行为 | 本包 | 固定参考实测 | 处置 |
| --- | --- | --- | --- |
| 空 call id 的 `tool-call` 块 | 接受 | 接受（`snapshotSessionEvent` 不检查） | 一致；流组装在 M4 才保证可用名字与 id |
| 空 toolName 的 `tool-addition`／`tool-removal` | 接受 | 接受 | 一致；`headerSeq` 与角色规则在 Session 边界检查 |
| 无法识别的 `mediaType` | 接受 | 接受 | 一致；由提供方（M4）拒绝不支持的编码 |
| 缺少 `attachmentId` 或 `name` | 接受 | 接受 | 一致；本包不做身份检查 |
| 负数的 usage 计数 | 接受 | 接受 | 一致；缺省仍是 nil 指针，与“报告为零”可区分 |
| user／assistant 消息携带 `toolCallId`／`isError` | 接受 | 接受 | 一致；未使用的可选字段被保留 |
| assistant 消息使用非 model source | 拒绝 | 拒绝（`message must have model source`） | 一致 |
| system 消息使用非 system-prompt source | 拒绝 | 拒绝（`message must have system-prompt source`） | 一致 |
| tool 消息两处 call id 不一致 | 拒绝 | 拒绝（`message has mismatched tool call ids`） | 一致 |
| 消息缺少 id | 拒绝 | 拒绝（`lacks an identified message`） | 一致 |
| 未知 role | 拒绝 | 拒绝（`message must have role "user"`） | 一致 |
| **未知 source kind** | **接受并保留** | **接受** | 一致，但两者含义不同：本包把未知字段存进 `Extra` 并保留，参考的类型是 merge-extensible，未知 kind 在类型上就允许 |
| **`offloaded: false` 显式写入** | **保留为 `false`** | **保留为 `false`** | 初版用 `omitempty` 会丢掉它，实测发现后改为 `*bool` |

一条**未对齐**的差异，必须如实记录：`Usage` 的计数字段在本包里是 `int`，所以一个显式的 `null`（参考可写）无法往返；参考的 `undefined` 与 `null` 都表示“未披露”，本包只用 nil 指针表示，因此**写入方写 `null` 会被本包读成“未披露”并以缺省重写**。这不影响任何当前生产者（它们从不写 `null`），但它是真实的格式差异，属 M6 格式互读时要处理的问题，本阶段不声称兼容。
