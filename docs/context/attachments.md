# 持久附件与本次请求可访问路径

## 读完要解决的问题

一次请求里要带上一个文件：用户上传的截图、日志片段、配置快照。最直觉的做法是带上**路径**——但路径不是身份。同一个路径下的文件可以被替换、移动或删除，而引用它的请求会**静默地描述成另一个东西**：模型看到的是新内容，日志里记的却是旧路径。

反过来还有一个坑：附件已经存在 store 里了，是不是请求就能读？如果"存储里有"等于"这次请求能读"，那么"这次请求被允许看到什么"就没有答案。

所以要把两件事分开：**存储身份**（内容按身份存一次，不可变）与**请求可达映射**（每次请求一份，只含它能读的身份）。

需要的 Go 知识：接口、结构体、map。不需要 TypeScript 或 Cordis 基础。当前实现范围是 M8.4 的附件 seam 与请求映射；**落盘、淘汰与真实模态协商属 M12，本文不声称支持**。完成实验后你能做到：**预测四种失败各自报什么，并解释为什么不能返回空结果。**

## 从熟悉的 Go 写法出发

普通做法是 `map[string][]byte`，key 用路径，读取时 `os.ReadFile(path)`。这能工作，但有三个边界：

1. **路径会变**。文件被替换后，同一个 key 指向不同内容，而没有任何地方察觉。
2. **没有"这次请求能读什么"**。map 里有什么就能读什么，权限范围无从表达。
3. **失败被抹平**。"文件不存在""内容变了""这台机器上没有""这个模态不支持"最终都变成一个 `nil`。

于是引入两个概念：

- **身份（ID）与描述符（Descriptor）**：内容按身份存一次，描述符记录模态、大小与**摘要**；读取时重验摘要。
- **映射（Mapping）**：一次请求可读的身份集合，且绑定**执行世界**（World）。

类比之后要说清哪里不等价：这与"缓存"不同。缓存用内容换速度，可以随时淘汰重建；这里换的是**可追溯性**——同一个身份必须永远是同一份内容，否则引用它的历史记录就失效了。

## 最小实验

准备条件：仓库根目录，Go 1.22+。不需要网络与密钥。

```go runnable=attachment-mapping
package main

import (
	"fmt"

	capatt "github.com/gosomea/deepseek-harness-go/capability/attachment"
	"github.com/gosomea/deepseek-harness-go/plugins/attachment"
)

func main() {
	store := capatt.NewMemoryStore(capatt.ModalityText, capatt.ModalityImage)
	for _, a := range []struct {
		id, body string
		m        capatt.Modality
	}{
		{"notes", "notes body", capatt.ModalityText},
		{"shot", "png bytes", capatt.ModalityImage},
	} {
		if _, err := store.Put(a.id, a.m, []byte(a.body)); err != nil {
			panic(err)
		}
	}

	// Only text is exposed to this request, and only in the local world.
	m := attachment.NewMapping("local", store, capatt.ModalityText)
	if err := m.Expose("notes"); err != nil {
		panic(err)
	}
	content, desc, err := m.Resolve("notes", "local")
	fmt.Printf("resolve notes: %q modality=%s err=%v\n", content, desc.Modality, err)

	// Stored but not exposed: reachable by storage is not reachable by request.
	_, _, err = m.Resolve("shot", "local")
	fmt.Println("resolve shot:", err)

	// Same identity, different execution world.
	_, _, err = m.Resolve("notes", "remote")
	fmt.Println("resolve in remote:", err)

	// Every unreachable id reports why, rather than looking absent.
	for _, d := range m.Diagnose([]string{"notes", "shot", "ghost"}, "local") {
		fmt.Printf("diagnostic %-6s %v\n", d.ID, d.Err != nil)
	}
}
```

```text output=attachment-mapping
resolve notes: "notes body" modality=text err=<nil>
resolve shot: attachment: unknown attachment: "shot" is not exposed to this request
resolve in remote: attachment: mapping belongs to another execution world: "remote" != "local"
diagnostic ghost  true
diagnostic shot   true
```

观察重点：**三种失败各不相同**——未暴露（`unknown attachment ... is not exposed`）、世界不匹配（`belongs to another execution world`）、不存在（`ghost` 同样报诊断）。没有一个是空结果，因为"读不到"和"没有这个附件"需要不同的修法。

## 先预测，再观察

**先写下你的预期**：把 `NewMapping` 的暴露模态改成 `capatt.ModalityText, capatt.ModalityImage`，再 `m.Expose("shot")`，会成功吗？如果 store 只声明了 `ModalityText`，`Expose("shot")` 又会怎样？

观察方法：改两行后重跑。

结果：第一种情况 `Expose("shot")` **成功**，因为请求确实开放了 image 模态；第二种情况**失败**并报 `attachment: store cannot carry this modality`。原因：`Expose` 在暴露前先 `Describe` 并查 `Supported`——**身份不能仅凭名字就成立**。恢复：还原改动即可。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 附件是不可变存储 + 稳定身份；请求只引用本次可访问的映射；损坏／换世界／不支持模态都有诊断 |
| TypeScript 入口 | 固定提交 `00102833` 的 [packages/attachment](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/attachment) |
| Go 入口 | [attachment.go](../../capability/attachment/attachment.go) 的 `Store`/`MemoryStore`/`Digest`；[store.go](../../plugins/attachment/store.go) 的 `Mapping`/`Diagnostic`；测试见 [attachment_test.go](../../capability/attachment/attachment_test.go) 与 [store_test.go](../../plugins/attachment/store_test.go) |
| 保持的行为 | 身份不可变；暴露前确认 store 承载能力；世界必须匹配；读取重验摘要；失败给具体诊断 |
| Go 表达 | `Store` 用小接口而非具体类型；`MemoryStore` 进出都拷贝；`Mapping` 只持身份不持内容，复制映射不复制内容 |
| 不等价边界 | 本阶段**不落盘、不淘汰、不含路径解析、不实现模态协商**；真实存储 Provider 与平台目录属 M12 |

## 一次操作的时序与所有权

1. `store.Put(id, modality, content)` 校验身份与模态，拷贝内容，记录摘要。
2. `NewMapping(world, store, modalities...)` 建立映射——**只有身份集合**。
3. `mapping.Expose(id)`：先 `Describe` 查身份（未知即 `ErrUnknownAttachment`），再查请求是否开放该模态（`ErrUnsupportedModality`），再查 store 是否有能力（`ErrStoreIncapable`），最后记录身份。
4. `mapping.Resolve(id, world)`：先比世界（`ErrWorldMismatch`），再查是否已暴露，最后 `Get` 并重验摘要。
5. 存储层在摘要不符时返回 `ErrCorrupt`；映射如实上报，不返回内容。
6. `Diagnose` 排序后返回每条失败的身份与理由。

所有权要点：**store 拥有内容，mapping 只拥有"这次请求能读哪些身份"**。因为映射不缓存内容，复制它不会连带复制字节；也因为如此，`Resolve` 每次都回 store 取内容并重验，**不存在"映射里的旧副本"**。

## 失败与定位

**失败一：身份未暴露。** 症状是 `attachment: unknown attachment: "shot" is not exposed to this request`。原因：附件在 store 里，但这次请求没有 `Expose` 它。诊断入口：`Resolve` 的暴露检查。恢复：按本次请求的授权显式暴露——**存储可达不等于请求可达**。

**失败二：执行世界不匹配。** 症状是 `attachment: mapping belongs to another execution world: "remote" != "local"`。原因：映射在本机构建、却拿到远端读取。诊断入口：`Resolve` 的世界比较。恢复：在所属世界构建映射——同一身份在不同世界是不同对象。

**失败三：模态不开放或不支持。** 症状是 `attachment: modality is not exposed: "shot" carries "image"` 或 `attachment: store cannot carry this modality: "image"`。原因：请求没开放该模态，或 store 声明不承载它。诊断入口：`Expose` 的两道检查。恢复：显式开放模态或换存储——身份不靠名字成立。

**失败四：内容损坏。** 症状是 `attachment: content digest does not match`。原因：内容在 store 底下被替换（例如文件备份 Provider 的文件变了）。诊断入口：存储层 `Get` 的摘要比对。恢复：重新 `Put` 一份新身份——**绝不把摘要不符的内容当作记录内容返回**。

## 回到源码与下一步

建议阅读顺序：

1. [attachment.go](../../capability/attachment/attachment.go) 的 `Digest` 与 `MemoryStore.Put` —— 先看身份与摘要如何确立。
2. 同文件的 `MemoryStore.Get` —— 再看读取时如何重验摘要。
3. [store.go](../../plugins/attachment/store.go) 的 `Mapping.Expose` —— 确认暴露前的两道检查。
4. 同文件的 `Mapping.Resolve` 与 `Diagnose` —— 最后看四种失败如何分流。

对应测试名：`TestCorruptContentIsDetected`、`TestResolveRefusesAnUnexposedButStoredAttachment`、`TestWorldMismatchIsRefused`、`TestUnsupportedModalityIsRefusedAtExpose`、`TestCorruptContentSurfacesAsADiagnostic`。

读完应能回答：**机制解决什么问题**（路径不是身份；请求可达范围可回答）；**谁拥有资源**（store 拥有内容，mapping 只拥有身份集合）；**Go 与 DSH 的差异在哪**（不落盘、不淘汰、无路径解析、无模态协商）。

可运行实验见 [examples/context](../../examples/context/README.md)。下一篇：长输出如何缩短但仍可定位原文（`docs/context/budgets.md`，属 M8.5）。
