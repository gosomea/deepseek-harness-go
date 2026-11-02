# 贡献、渲染与可重建的请求

## 读完要解决的问题

模型每次请求里都有一些「看起来一直都在」的内容：基础指令、当前时间、指向某个文件的引用。它们平时没人追问，一旦模型答错，第一个问题就是：**这些内容是谁放进去的、当时到底是什么？**

如果答案只能靠「大概哪个插件先跑了」来猜，那就无法复现。本包要解决的正是这件事：把「模型看到什么」变成**先记录、后渲染**的事实，让同一份日志加同一版代码渲染出同一份请求。

需要的 Go 知识：结构体与方法、切片排序、接口。不需要 TypeScript 或 Cordis 基础。当前实现范围是 M8.1 的贡献记录与渲染；技能、附件、预算与压缩属后续切片，本文只在边界处点名。

完成实验后你能做到：**对同一个贡献集合渲染两次并解释为什么两次必然相同，再制造一次「未记录就想渲染」的尝试并说明它为什么必然失败。**

## 从熟悉的 Go 写法出发

普通的 Go 做法是**边拼边发**：拿到依赖就 `builder.WriteString(...)`，拼完直接交给模型。构造注入能保证初始依赖就位，但它管不了两件事：

1. **顺序**。如果贡献来自 `map` 或按注册顺序遍历，两次拼出来的文本可能不同——而提示词回归测试恰恰要靠逐字比较。
2. **归属**。文本拼完之后，没人能回答「这一行是谁加的」。

于是引入两个概念：

- **贡献（Contribution）**：一条被记录的、有稳定 ID 的「模型应该看到的东西」，先写进 Session 日志。
- **渲染（Render）**：读取已记录的贡献，按来源顺序拼成文本，并逐行给出它引用的贡献 ID。

类比之后要说清哪里不等价：这与「把模板字符串参数化」不同。模板法仍然允许运行期随手塞入变量，而这里**渲染不能引入日志里没有的内容**——渲染器手上只有集合，集合只由 `Recorder` 在日志接受之后填充。

## 最小实验

准备条件：仓库根目录，Go 1.22+。这一步不需要网络与密钥。

```go runnable=context-rebuild
package main

import (
	"fmt"

	"github.com/gosomea/deepseek-harness-go/plugins/context"
)

// memoryLog is the smallest thing that satisfies context.MemoryLog: a place
// facts are recorded. The real session log plays this role in a harness.
type memoryLog struct{ recorded []string }

func (l *memoryLog) AppendContribution(c context.Contribution) (string, error) {
	l.recorded = append(l.recorded, c.ID)
	return c.ID, nil
}

func main() {
	log := &memoryLog{}
	recorder := context.NewRecorder(log)

	set, err := recorder.Record(
		context.Contribution{
			ID: "base", Kind: context.KindInstruction, Source: context.SourceBase,
			Text: "You are a harness.",
		},
		context.Contribution{
			ID: "spec", Kind: context.KindReference, Source: context.SourceEnvironment,
			Reference: &context.Reference{Path: "docs/context/contributions.md"},
		},
		context.Contribution{
			ID: "clock", Kind: context.KindTime, Source: context.SourceProfile,
			Text: "now: fixed for this experiment",
		},
	)
	if err != nil {
		panic(err)
	}

	request, err := context.Render(set, "")
	if err != nil {
		panic(err)
	}
	fmt.Println("recorded:", log.recorded)
	fmt.Println("cited:", request.CitedIDs)
	fmt.Println("text:")
	fmt.Println(request.Text)
}
```

```text output=context-rebuild
recorded: [base spec clock]
cited: [base clock spec]
text:
You are a harness.

now: fixed for this experiment

reference: docs/context/contributions.md
```

观察重点：**记录顺序与引用顺序不同**。`recorded` 是写入顺序（`base, spec, clock`），`cited` 是渲染顺序（`base, clock, spec`）——因为 `SourceProfile` 排在 `SourceEnvironment` 之前。顺序由来源决定，不由谁先注册决定。

## 先预测，再观察

**先写下你的预期**：把 `spec` 的 `Source` 从 `SourceEnvironment` 改成 `SourceSession`，`cited` 与 `text` 会怎么变？`recorded` 会怎么变？

观察方法：改一行后重跑上面的程序。

结果：`recorded` **不变**（仍是 `[base spec clock]`，因为写入顺序没动）；`cited` 变成 `[base clock spec]` —— 与改之前相同；`text` 也不变。原因：`SourceProfile` 与 `SourceSession` 在 `renderOrder` 里分别排在 `SourceEnvironment` 之后与最后，而 `clock` 的插入位置在两者之前，所以它在两种来源下都在 `spec` 之前。

换个改法更有意思：把 `clock` 的 `Source` 改成 `SourceBase`。此时 `cited` 变成 `[base clock spec]` 不变，但若把 `clock` 的插入位置挪到 `base` 之前，`cited` 会变成 `[clock base spec]`——**插入顺序只在同一来源内决定先后**。

恢复：把改动还原即可，实验不写入任何仓库文件。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 模型可见的上下文要先成为日志事实，再由请求渲染；渲染不得引入日志没有的内容 |
| TypeScript 入口 | 固定提交 `00102833` 的 [packages/context](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/context)；相关子系统说明见 [docs/subsystems](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/subsystems) |
| Go 入口 | [贡献定义](../../plugins/context/contribution.go) 的 `Contribution`/`Set`；[渲染](../../plugins/context/render.go) 的 `Render`/`Recorder`；测试见 [contribution_test.go](../../plugins/context/contribution_test.go) |
| 保持的行为 | 同一 `(Set, scope)` 渲染出同样字节；每行输出都可追溯到被引用的 ID；重复 ID 被拒绝；空 `Scopes` 表示全局可见 |
| Go 表达 | 用 `MemoryLog` 小接口而非具体 `*session.Log`，让依赖停在「有个地方记录事实」；`renderOrder` 用切片而非 map；`Recorder` 在日志接受后才填充集合 |
| 不等价边界 | 本包**不读引用内容**、**不写 Session 日志**、**不含预算与压缩**、**不处理技能与附件**。DSH 的附件与 spill 语义属 M8.4/M8.5，此处不声称支持 |

## 一次操作的时序与所有权

1. 调用方构造 `Contribution` 并交给 `Recorder.Record`。
2. `Record` 先 `Validate`（空 ID、种类与字段搭配、未知来源在此被拒）。
3. `Record` 调 `MemoryLog.AppendContribution`，**日志先接受**。
4. 只有日志接受了，才把该贡献 `Add` 进 `Set`。
5. `Render` 读 `Set.Ordered(scope)`，按来源顺序拼接，同时登记 `CitedIDs`。
6. 渲染结束，`Set` 与 `Request` 都是普通值，没有后台 goroutine、没有需要释放的句柄。

所有权要点：**日志拥有事实**，`Set` 只是本次渲染的视图。第 4 步是「先记录后渲染」的落地点——日志拒绝时集合不建立，调用方不可能拿到一个包含未记录内容的 `Set`。

## 失败与定位

**失败一：重复 ID 被拒。** 症状是 `context: duplicate contribution id: "same"`。原因：同一 ID 交了两条不同的贡献。诊断入口：`Set.Add` 的重复检查。恢复：给两条不同内容各自一个稳定 ID——静默覆盖会让「模型看到哪条」取决于调用顺序。

**失败二：引用带了内联正文。** 症状是 `context: invalid contribution: reference "r" must not carry inline text`。原因：`KindReference` 同时填了 `Text`。诊断入口：`Contribution.Validate`。恢复：把正文留在被引用的文件里，只传路径。

**失败三：渲染遇到未知种类。** 症状是 `context: contribution was not recorded: no render rule for kind "..."`。这条在正常使用下不该出现（`Ordered` 只返回已校验的贡献），它存在是为了让**将来新增的种类若忘了写渲染规则，会响亮失败**，而不是从请求里静默消失。诊断入口：`Render` 的 `default` 分支。

一个容易混淆的边界：**日志拒绝与校验拒绝是两种失败**。校验失败时日志根本没被调用（`Record` 在写之前就返回错误）；日志拒绝时校验已通过。前者是「这条贡献不合法」，后者是「记录面现在不接受」。区分它们才能判断该改数据还是查存储。

## 回到源码与下一步

建议阅读顺序：

1. [contribution.go](../../plugins/context/contribution.go) 的 `Contribution.Validate` —— 先看一条贡献成立的条件。
2. 同文件的 `Set.Add` 与 `Set.Ordered` —— 再看集合如何拒绝重复、如何按来源排序。
3. [render.go](../../plugins/context/render.go) 的 `Recorder.Record` —— 确认「先记录后渲染」的落点。
4. 同文件的 `Render` 与 `Rebuild` —— 最后看渲染与一致性检查。

对应测试名：`TestRenderIsAPureFunctionOfTheSet`、`TestRecorderDropsWhatTheLogRefused`、`TestRenderFailsLoudlyOnAnUnknownKind`、`TestDuplicateIDIsRefusedNotOverwritten`。

读完应能回答：**这个机制解决什么问题**（让「模型看到什么」可复现、可归属）；**谁拥有资源**（Session 日志拥有事实，`Set` 只是视图，无后台资源需释放）；**Go 与 DSH 的差异在哪**（本包只到贡献与渲染，附件、spill、压缩、技能都未实现）。

可运行实验见 [examples/context](../../examples/context/README.md)。下一篇：技能清单与实际加载的 token 差异（`docs/context/skills.md`，属 M8.3）。
