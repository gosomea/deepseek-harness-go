# 内容块参考：字段、失败规则与未知内容政策

## 读完要解决的问题

你已经知道一条消息由有序的内容块组成（见[消息模型](go-primer.md)）。接下来的问题是**每个块到底有哪些字段、哪些是必填、以及遇到不认识的内容怎么办**。

这不是查表问题。第三问决定了整个复刻的正确性：如果未知内容被悄悄转成文本，那么一个更新的写入方产生的记录会在旧读取器里变成一段没有任何意义的话，而复刻的目标是“相同日志得到相同历史”。这一篇给出三层不同政策——**组装报错、存储保留、提供方拒绝**——并解释为什么一个全局政策在每个方向都是错的。

前置知识：Go 的结构体、接口、`encoding/json` 与 `errors.Is`。关联行为见 [M3 执行契约](../10-plans/dsh-go-replication-m03/plans.md)。

## 从熟悉的 Go 写法出发

普通 Go 做法是给内容定义一个大结构体，用 `omitempty` 让不适用的字段消失：

```go fragment
type Content struct {
	Text      string `json:"text,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	ToolName  string `json:"toolName,omitempty"`
	ToolArgs  string `json:"arguments,omitempty"`
}
```

它简单、能序列化。边界在于两块内容无法同时出现：一条消息里既有 reasoning 又有文本，这个结构体就表达不了；工具调用可以有多个，也表达不了。于是你会加上 `[]ToolCall`，再发现“顺序”也丢了——模型先说话再调工具，和先调工具再说话，是有区别的。

DSH 用**判别字段加块列表**解决：每个块自带 `type`，消息持有块的有序列表。这个选择还有一个副作用，正是本篇的主题：当 `type` 是数据而不是类型参数时，**一个不认识的 `type` 就成了一个必须给出政策的情况**，而不是编译错误。

哪里不等价：TS 的 `ContentBlockMap` 通过 `declare module` 允许插件追加成员，编译期就把新种类纳入联合。Go 没有声明合并，本包把“未知种类”做成一个**运行时**的兜底类型 `UnknownBlock`。政策因此必须写清楚，也就必须被测试固定。

## 最小实验

准备条件：Go 1.22+，仓库根目录。下面的完整程序演示三层政策中的两层：已知块逐字节往返，未知块原样保留。

```go runnable=content-blocks
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
	// Two records a newer writer could have produced: one block kind this build
	// knows, one it does not.
	records := []string{
		`[{"type":"text","text":"hi"}]`,
		`[{"type":"audio-note","seconds":3,"transcript":"hi"}]`,
	}

	for _, record := range records {
		blocks, err := llm.UnmarshalContent([]byte(record))
		if err != nil {
			return err
		}
		encoded, err := llm.MarshalContent(blocks)
		if err != nil {
			return err
		}
		fmt.Printf("read   %s\n", record)
		fmt.Printf("kind   %s\n", blocks[0].BlockType())
		fmt.Printf("go     %T\n", blocks[0])
		fmt.Printf("visible text=%q\n", llm.ContentText(blocks))
		fmt.Printf("byte-identical=%t\n\n", string(encoded) == record)
	}

	// A structurally incomplete but representable block is stored, matching the
	// reference: the check for a usable call id belongs where the call is paired.
	blocks, err := llm.UnmarshalContent([]byte(`[{"type":"tool-call","name":"read","arguments":"{}"}]`))
	if err != nil {
		return err
	}
	fmt.Printf("missing call id accepted=%t kind=%s\n", true, blocks[0].BlockType())
	return nil
}
```

预期输出：

```text output=content-blocks
read   [{"type":"text","text":"hi"}]
kind   text
go     llm.TextBlock
visible text="hi"
byte-identical=true

read   [{"type":"audio-note","seconds":3,"transcript":"hi"}]
kind   audio-note
go     llm.UnknownBlock
visible text=""
byte-identical=true

missing call id accepted=true kind=tool-call
```

四行值得注意：

- 未知块的 `go` 类型是 `llm.UnknownBlock`，但它的 `kind` 仍是原始的 `audio-note`——判别字段没有丢。
- `visible text=""`：未知块**没有**被压成文本。`ContentText` 只拼接 `TextBlock`，这是刻意的。
- 缺 call id 的已知块被**接受**并原样存储：固定参考在运行期也不检查它，检查发生在真正把调用与结果配对的边界。

## 先预测，再观察

先做一次预测：把下面这份“已知种类、但带一个本包不认识的字段”的记录喂给 `UnmarshalContent`，往返后 `colour` 还在吗？

```text
[{"type":"text","text":"hi","colour":"red"}]
```

预测之后，运行 `go test -run TestKnownBlocksWithoutExtraFieldsRejectUnknownFields -v ./llm`。

答案是**不在**。这看起来与上一条（未知块完整保留）矛盾，其实不是：

- **未知的种类**（`audio-note`）→ 整个对象原文保留，包括所有字段与它们的顺序。
- **已知种类上的未知字段**（`text` 上的 `colour`）→ 按 `TextBlock` 自己的字段重编码，`colour` 丢掉。

为什么故意不同？因为一个已知种类的**形状是本包承诺的事实**：`TextBlock` 只有一个 `text` 字段，一个多出来的字段不属于这个契约。把它当成数据保留，会让“`TextBlock` 是什么”变成依赖输入的东西。反过来，一个未知**种类**本包根本不知道它的形状，唯一安全的行为就是原样保存。

这带来一条使用规则：**给已知块加字段必须同时加一个块类型**，否则旧读取器会丢掉它而不报错。这正是参考实现要求“新核心块必须连同适配器、UI 与压缩支持一起落地”的原因。

再预测一次：未知块能不能带一个 JSON 数组而不是对象？

答案：不能，`UnknownBlock.Validate` 要求原文是对象。原因：`type` 判别字段本身要求对象形状，一个非对象无法携带判别字段。用 `go test -run TestUnknownBlockNeedsPayload ./llm` 验证。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | `ContentBlockMap` 键控的 merge-extensible 联合；`ContentBlockType = keyof ContentBlockMap`；每个块的具体字段与可选的 literal-true 标记 |
| TypeScript 入口 | `packages/llm/llm/src/types.ts`（`TextBlock`、`ReasoningBlock`、`ImageBlock`、`FileBlock`、`ToolCallBlock`、`ToolAdditionBlock`、`ToolRemovalBlock`） |
| Go 入口 | [llm/content.go](../../llm/content.go) 的 `ContentBlock` 接口与七个块；[llm/codec.go](../../llm/codec.go) 的 `MarshalContent`／`UnmarshalContent`／`decodeBlock` |
| 保持的行为 | 块的顺序有意义；每个块校验自己的必填字段；未知种类逐字节保留且不进入可见文本；已知种类的解码是严格的 |
| Go 表达 | 接口加 `UnknownBlock` 兜底；`ContentBlockType` 是 `string` 而不是整数枚举（未知值必须可读）；`TextContent("")` 返回 `nil` 表示无内容 |
| 不等价边界 | TS 的声明合并无法照搬，改由运行时兜底；TS 的 `@persistenceReserved` 这类编译期保留字段在 Go 里只能靠注释与校验表达 |

### 未知内容怎样处理

三层政策，逐层不同：

| 层 | 遇到未知内容 | 理由 |
| --- | --- | --- |
| 本包的解码／编码（存储边界） | 未知**种类**原样保留；已知种类上的未知**字段**按该类型重编 | 一个更新的写入方产生的记录不能被旧读取器重排或丢字段；而已知种类的形状是本包的契约 |
| 本包的块校验 | 只拒绝无法表示的值（nil 块）；**不做**身份或范围检查 | 固定参考在运行期也不检查这些：空 call id、空 toolName、未知 mediaType、负计数它都接受。本包加严会比参考更容易拒绝一份它存下的记录 |
| 未来的提供方适配器（M4） | 用自己的能力声明拒绝不支持的块 | 只有真正需要语义的边界才该拒绝；把拒绝提前到存储层会让旧读取器无法保存新数据 |

“组装报错”指第三层之外的组装过程：一个已打开但种类不明的块无法拼接，因此报错；这与存储层保留未知种类是两件事。本包只实现前两层。

### 字段表

| 判别字段 | 必填字段 | 可选字段 | 说明 |
| --- | --- | --- | --- |
| `text` | `text` | — | 可见文本，可为空字符串 |
| `reasoning` | `text` | — | 思考内容，与可见文本分开 |
| `image` | `attachment` | `offloaded`（只能为 `true`） | 内容寻址的图片引用；角色中立，但当前生产适配器只声明文本输出，实际只有 user 内容会带图 |
| `file` | `attachment` | — | 内容寻址的文件引用；**任何提供方都收不到字节**，请求装配会把每次出现投影成句柄文本 |
| `tool-call` | `id`、`name`、`arguments` | — | `arguments` 是模型产出的**原始 JSON 字符串**，从不解析 |
| `tool-addition` | `toolName` | — | 只能出现在 developer 消息里；激活被引用请求头中的一个工具 |
| `tool-removal` | `toolName` | — | 按会话内名字记录工具移除 |
| *其他* | 由写入方决定 | — | 解码成 `UnknownBlock`，原文保留；本包不判断它的形状 |

`image` 的 `attachment` 是 `ImageAttachmentRef`：`attachmentId`（约定的内容地址，形如 `sha256:<hex>`）、`mediaType`、`bytes`、`width`、`height`，可选 `name` 与 `originalDimensions`。

**必填不等于本包会校验**。表中“必填”描述的是**生产者应当写什么**（参考的 TypeScript 类型如此要求）；本包的 `Validate` 是结构性的，实测参考在这些字段缺失或取值异常时仍然接受。`mediaType` 因此是开放的 `ImageMediaType` 字符串而不是封闭枚举——枚举会让本包拒绝一份参考存下的记录。`attachmentId` 的约定是内容地址，不是路径也不是 URL；这条约定由生产者遵守，本包不检查它。

`file` 的 `attachment` 是 `FileAttachmentRef`：`attachmentId`、`name`、`bytes`。

## 一次操作的时序与所有权

`UnmarshalContent` 的路径决定了政策落在哪里：

1. 先拒绝尾随内容，再把输入解成 `[]json.RawMessage`。
2. 对每个元素解出 `{"type": ...}`。没有 `type` 直接拒绝。
3. 若 `type` 是已知种类，解到该结构体（严格，未知字段被忽略、类型不符报错），再调用它自己的 `Validate`。
4. 若 `type` 未知，把**原始字节拷贝**成 `UnknownBlock.Raw` 并校验它是一个对象。

第 4 步的拷贝是有意的：`Raw` 不引用调用方的缓冲区，所以调用方之后修改自己的数据不会改变已解码的值。

`MarshalContent` 的路径分两条：已知块由 `json.Marshal` 编结构体（字段声明序），再把 `type` 拼到最前；未知块若原文的 `type` 与声明一致，**直接输出原始字节**，否则在保留其余字段的前提下补上 `type`。两条路径都确定：同一个值编码两次得到相同字节。这个性质由 `TestMarshalIsDeterministic` 固定，因为 `session` 会把字节当作可比较、可哈希的权威事实。

谁拥有资源：没有资源。所有类型都是值；`UnknownBlock.Raw` 是一个切片，指向它自己的底层数组。

## 失败与定位

**一、`content block 0: llm: invalid value: tool-call block requires a call id`。**
症状：解码一份工具调用没有 id 的记录时失败。原因：没有 call id 的调用无法被结果配对，存下来只会产生一个没法回答的问题。诊断入口：错误链里的 `ToolCallBlock.Validate`（外层前缀是块序号，用 `errors.Is(err, llm.ErrInvalidValue)` 匹配真正的判据）。恢复：修正写入方；如果这是历史数据的缺陷，那属于迁移问题，不属于本包。

**二、`llm: invalid value: content must be an array`。**
症状：把一个对象当成内容传进 `UnmarshalContent`。原因：内容在编码形式里是数组，即使只有一个块。诊断入口：错误信息直接指出形状。恢复：用 `[ ... ]` 包起来。

**三、`llm: invalid value: unknown block "x" requires its original JSON`。**
症状：手工构造了一个没有 `Raw` 的 `UnknownBlock`。原因：这个类型的存在意义就是保留原文；没有原文就没有任何可恢复的内容。诊断入口：`UnknownBlock.Validate`。恢复：不要手工构造未知块——它应当只由解码器产生。

区分清楚：这三类都是**无效输入**。本包没有**预期等待**（不涉及并发）、没有**执行失败**（不调用任何外部东西）、没有**释放失败**（没有资源）。如果错误不是 `ErrInvalidValue`，那它来自 `encoding/json` 本身，说明输入根本不是合法 JSON。

## 回到源码与下一步

建议的阅读顺序：

1. [llm/content.go](../../llm/content.go) 的接口与七个块的 `Validate`——先看每个块认为自己必须有什么。
2. [llm/codec.go](../../llm/codec.go) 的 `decodeBlock` 的 `default` 分支——政策就落在这里。
3. `spliceType`、`splitFields`、`removeTypeField`——字节级保真如何实现。
4. 对照 `packages/llm/llm/src/types.ts` 的 `ContentBlockMap`。

对应测试：`TestUnknownBlockSurvivesRoundTrip`（未知种类保真）、`TestContentRoundTripIsLossless`（已知块保真）、`TestKnownBlocksWithoutExtraFieldsRejectUnknownFields`（已知种类的未知字段不保留）、`TestDecodeBlockRejectsMalformedInput`（形状错误的拒绝）、`TestBlockValidateRejectsIncompleteValues`（必填字段）。

读完你应该能回答：**这个机制解决什么问题**——让内容有顺序、有归属，并让词汇增长不损失已记录的事实；**谁拥有资源**——没有资源，`UnknownBlock.Raw` 拥有自己的字节；**为什么这样表达**——接口承载可增长的词汇，判别字段承载可判读的存储。

下一步是 C07 事件溯源（`docs/session/go-primer.md`，由 M3 后续切片创建）：这些消息值怎样进入一份只能追加、序号连续的日志。

## 与固定参考的实测差异

本包的块校验复刻的是**参考运行期真正会拒绝的东西**，不是它 TypeScript 类型上不允许的东西。实测（直接调用固定提交的 `packages/core/session` 入日志边界）：

| 行为 | 本包 | 固定参考 | 处置 |
| --- | --- | --- | --- |
| 空 call id 的 `tool-call` | 接受 | 接受 | 一致；流组装在 M4 保证可用 id |
| 空 toolName 的 `tool-addition`／`tool-removal` | 接受 | 接受 | 一致；`headerSeq` 规则在 Session 边界检查 |
| 未知 `mediaType` | 接受 | 接受 | 一致；`ImageMediaType` 保持开放字符串 |
| 缺 `attachmentId`／`name` | 接受 | 接受 | 一致 |
| 负 usage 计数 | 接受 | 接受 | 一致 |
| 未知块种类 | 保留原文 | 保留 | 一致 |
| 已知块上的未知字段 | 按该类型重编（丢弃） | 接受并保留 | **未对齐**：参考保留任意自有字段，Go 结构体不会。影响被限制在“给已知块加字段”这一个用法，见[未知内容怎样处理](#未知内容怎样处理) |

最后一条是本包与参考之间**真实的格式差异**，不是可以声称等价的地方。它之所以可接受，是因为参考自己要求“新核心块必须连同适配器、UI 与压缩支持一起落地”——即任何新字段都应带来一个新块类型，而新块类型本包会完整保留。
