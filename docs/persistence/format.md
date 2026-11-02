---
description: "记录边界、格式版本与两类损坏：为什么撕裂尾部可以截掉，而中间损坏必须拒绝。"
kind: "concept-note"
---

# 格式与损坏：一行一条记录，两类损坏

## 读完要解决的问题

把日志写成文件之后，第一个现实问题不是「怎么写得快」，而是**「读到一个坏字节时怎么办」**。进程可能在任意时刻被打断：写到一半断电、磁盘满、被 kill -9。下次打开这个文件时，它有几种可能？

如果只有「能读」和「不能读」两种答案，调用方就只能丢掉整份历史——哪怕坏掉的只是最后几个字节。所以真实需要的是一套**分层判断**：

1. 这个头是**我们的**文件吗？
2. 版本是我们认识的**这一个**吗？
3. 坏的字节在**末尾**还是在**中间**？
4. 修复之后，已提交的那部分还是原来的字节吗？

读完后你能：解释为什么「撕裂尾部」和「中间损坏」必须分开处理；说明为什么版本用拒绝而不是迁移；用 `Inspect` 与 `Recover` 在一个损坏副本上走一遍。本文讨论的是**已实现**的代码。

需要的 Go 知识：`bufio.Reader.ReadBytes` 与 `io.EOF`/`io.ErrUnexpectedEOF` 的区别、`os.Truncate`、`json.RawMessage`、SHA-256。

## 从熟悉的 Go 写法出发

普通的 Go 做法是把结构体序列化后写一行，读的时候按行扫描再 `json.Unmarshal`：

```go fragment
type record struct {
	Seq uint64          `json:"seq"`
	Pay json.RawMessage `json:"payload"`
}
```

这在「文件总是完整的」前提下够用。它挡不住三种情况：

- **半行**：最后一行写到一半就断了，`Unmarshal` 报语法错误，而错误里并不包含「这只是尾部」这个信息。
- **被改动的字节**：语法仍然合法，但内容已经不同；`Unmarshal` 会愉快地接受它。
- **未来版本**：新版本加了字段或改了语义，旧程序按老规则解读会得到**看起来正常但错误**的结果。

DSH 侧的对应概念是「记录边界 + 校验和 + 显式版本」，Go 侧落在 `plugins/sessionjsonl`：

- **记录（record）**：一行一条完整记录 = 序号 + 事件 JSON 原样嵌入 + 该 JSON 的 SHA-256。
- **校验和（checksum）**：判断「字节是否被改动」，与「事件能否解码」是两件独立的事。
- **撕裂尾部（torn tail）**：末尾没有换行符的不完整记录。
- **可修复（repairable）**：只有撕裂尾部才是可修复的。

## 最小实验

准备：仓库根、无需密钥。先用一个「干净」会话确认正常路径，再制造两类损坏。

```sh
go test -count=1 -run 'TornTail|MiddleCorruption|SequenceGap|FutureVersion' -v ./plugins/sessionjsonl/
```

预期四个测试都 PASS。它们分别断言：

- `TestTornTailIsRepairable`：末尾追加半条记录 → `Inspect` 报告 `Complete=3`、`Repairable=true`、`TornTailBytes>0`；`Recover` 之后 `Repairable=false`、`Complete=3`，且**文件字节与修复前完全相同**（因为截断只是删掉尾部）。
- `TestMiddleCorruptionIsRefused`：改动中间那条记录的校验和 → `Inspect` 报 `Repairable=false` 且 `Reason` 非空；`Recover` 返回 `persistence.ErrCorruptReport`；**文件保持原样**。
- `TestSequenceGapIsRefused`：把第二条记录声明成 `seq:7` → 同样被拒绝，`Reason` 里含 `seq`。
- `TestFutureVersionIsRefused`：header 版本改成 99 → 返回 `errUnsupportedVersion`；换成别人的格式名 → 返回 `errForeignFormat`。

想亲手看一个损坏副本，用最小程序即可：把某个会话的 artifact 复制一份，在末尾追加 `{"seq":3,"payl`，然后调用 `Inspect`。仓库内的测试就是这么做的，所以不需要额外的 fixture 文件。

## 先预测，再观察

**预测一**：如果一个文件在**中间**被删掉一段、导致后面的记录序号不连续，`Inspect` 会把它当成「尾部可修复」，还是拒绝？

观察：

```sh
go test -count=1 -run 'SequenceGap|MiddleCorruption' -v ./plugins/sessionjsonl/
```

结果：拒绝，`Repairable=false`。原因是判断点在**位置**而不是**数量**：只有「最后一条之后的不完整字节」才算撕裂尾部。序号是连续的整数，日志里的位置就是序号，所以间隙与重复是缺陷，绝不重编号——这与 [session](../../session/README.md) 的日志规则是同一条。

**预测二**：修复撕裂尾部之后，被保留的那些字节会不会被重新序列化一遍（因此可能与原文件不同）？

结果：逐字节相同，`TestTornTailIsRepairable` 显式比较了修复前后的字节。实现用 `os.Truncate` 截断到最后一个完整记录，不做临时文件加改名——因为被保留的字节没有换位置。这也解释了修复需要的是**文件**写权限，而不是目录写权限（该差异由 `TestRecoverReportsWriteFailure` 断言）。

**预测三**：为什么不干脆把「未知的未来版本」也修复掉（比如按当前版本尽力解析）？

结果：`TestFutureVersionIsRefused` 断言拒绝。理由是**误读比重读更贵**：一个新版本可能改了某字段的含义，按旧规则解析会得到一份看起来正常、实则错误的历史；而拒绝打开是安全且可解释的。恢复方式就是升级程序，或换回能识别该版本的实现。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 版本化会话格式：`SESSION_FORMAT_VERSION`、header 行、记录边界、截断修复偏移计算；未知版本与外部格式分别拒绝 |
| TypeScript 入口 | 固定提交 `00102833` 的 `packages/session/session-persistence-jsonl/src/format.ts`（路径净化、header (反)序列化、`truncation-repair offset computation`）与 `packages/session/session-format/src/filename.ts`；错误类型见同目录 `error.ts` |
| Go 入口 | [plugins/sessionjsonl/format.go](../../plugins/sessionjsonl/format.go)（`FormatID`、`FormatVersion`、`EncodeHeader`、`EncodeRecord`、`SessionPath`、`DecodeID`）、[plugins/sessionjsonl/recover.go](../../plugins/sessionjsonl/recover.go)（`Inspect`、`Recover`）；测试见 [format_test.go](../../plugins/sessionjsonl/format_test.go) 与 [recover_test.go](../../plugins/sessionjsonl/recover_test.go) |
| 保持的行为 | 头声明格式身份与版本；未知版本拒绝、外部格式拒绝且两者错误不同；末尾不完整记录可修复且不改已提交前缀；中间损坏与序号间隙拒绝 |
| Go 表达 | `json.RawMessage` 原样嵌入事件 JSON；SHA-256 存于记录层；`bufio.Reader.ReadBytes` 区分 `io.EOF` 与 `io.ErrUnexpectedEOF`；修复用 `os.Truncate`；每类损坏一个 sentinel error |
| 不等价边界 | **格式身份是本项目自己的**（`dsh-go-session`），不冒用 DSH 的 generation 名称（如 `session.v4.jsonl`），也不读 DSH 的目录布局；参考的 zstd 压缩与 catalog 迁移在本阶段**未实现**；会话 id 走可逆编码（参考的 `filename.ts` 也是编码而非信任），但具体转义字符集不同 |

参考实现里有一条注释明确写了「路径净化」的理由：`SessionId` 是**未经验证**的字符串，用作路径前必须编码，否则会路径穿越或碰撞。Go 侧照此实现，且**编码可逆**——`DecodeID` 能把文件名还原成会话 id，否则列举出来的会话就没法被命名。这一点在 `TestEncodedIDIsRecoverableFromTheListing` 中断言。

## 一次操作的时序与所有权

判别与修复的参与者：调用方、`Provider`、artifact 文件。

```text
调用方          Provider                artifact
  |                |                       |
  |-- Inspect ---->|-- 打开 + 读 header --->|
  |                |<-- header（格式/版本）--|
  |                |-- 逐行读记录 ---------->|
  |                |   完整 → Complete++
  |                |   末尾无换行 → TornTailBytes, Repairable=true
  |                |   校验和/序号异常 → Reason（不可修复）
  |<-- Analysis ---|                       |
  |                |                       |
  |-- Recover ---->| 有写者持有？→ 拒绝     |
  |                | Reason 非空？→ ErrCorruptReport（不改文件）
  |                | 仅有撕裂尾部 → os.Truncate(size-torn) -->| 删掉尾部字节
  |                |-- 重新 Inspect -------->|
  |<-- Analysis ---|                       |
```

所有权要点：

- **谁持有状态**：`Provider` 不缓存 artifact 内容，`Inspect` 每次都从磁盘读，因此报告的是**当前**事实。
- **何时发布结果**：`Inspect` 只报告、不修改；`Recover` 是唯一会改文件的调用。
- **取消传播**：`Recover`/`Inspect` 在动手前检查 `context`；`Recover` 还拒绝在有写者持有时运行，否则会与正在进行的追加竞争。
- **谁释放什么**：两者都用 defer 关闭自己打开的文件；`Recover` 不持有 lease，因此不需要释放。

## 失败与定位

| 现象 | 原因 | 定位方式 |
| --- | --- | --- |
| 正常文件报 `errInvalidHeader` | 文件为空、被截断，或根本不是我们的格式 | `Inspect` 的 `Reason`；`TestEmptyArtifactIsRefusedAtOpen` 覆盖空文件 |
| 中间损坏被当成可修复 | 判断点用了「数量」而非「位置」 | 看 `inspectArtifact`：只有 `io.ErrUnexpectedEOF` 才置 `Repairable` |
| 修复后历史变短却无告警 | 修复了不可修复的损坏 | `Recover` 必须先看 `Reason`，非空即返回 `ErrCorruptReport`；`TestMiddleCorruptionIsRefused` 断言文件未被改写 |
| 未来版本被当成当前版本解析 | header 校验被跳过 | `decodeHeader` 对 `version != FormatVersion` 返回 `errUnsupportedVersion` |
| 会话 id 出现在目录之外 | 路径拼接前没编码 | `SessionPath` 用 `encodeID`；`TestSessionPathEncodesTheID` 断言 `../` 无法逃逸 |

## 回到源码与下一步

- 格式定义与路径：[plugins/sessionjsonl/format.go](../../plugins/sessionjsonl/format.go)
- 判别与修复：[plugins/sessionjsonl/recover.go](../../plugins/sessionjsonl/recover.go)
- 日志自身的序号规则：[session/README.md](../../session/README.md)
- 下一步读：[恢复与 checkpoint](recovery.md)
