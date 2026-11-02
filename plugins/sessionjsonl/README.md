---
description: "把 Session 日志存成一目录一文件、一行一记录的 JSON Lines，并兑现单写者、flush 与可修复尾部恢复。"
kind: "package-library"
---

# plugins/sessionjsonl

## 概述

`sessionjsonl` 是 [persistence](../../persistence/README.md) 的第一个后端：**一行一条完整记录**，每条记录自带校验和；第一行是 header，声明格式身份、版本与所属会话。

三条规则决定了它的行为：

1. **格式身份是自己的。** 它不冒用参考实现的 generation 名称或目录布局。借名字而不借行为，会同时失去两边的可信度；而「能读 JSONL」也不等于「兼容 DSH 会话文件」。
2. **写入只发生在一个数据目录里。** 会话 id 会被**编码**成单个路径元素，`../` 之类无法逃逸；本包从不碰用户已有的 DSH home。
3. **损坏分两类处理。** 末尾不完整的记录是**撕裂尾部**，恢复可以截掉；中间损坏是**不可恢复**，一律拒绝，因为丢掉中部会静默删除无法找回的记录。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- **帧格式**：`EncodeHeader`/`decodeHeader` 与 `EncodeRecord`/`decodeRecord`，含版本拒绝与校验和验证。
- **存储**：`Provider` 实现 `persistence.Service`；`Create`/`Open`/`Stat`/`List`/`Close`。
- **单写者**：进程内用 `persistence.BaseService` 登记，进程间用独占创建的 lease 文件；两处都拒绝第二个写者。
- **append 与 flush**：`Append` 只把记录放进缓冲区并对本句柄可见；`Flush` 才 `Sync` 到磁盘。失败路径绝不报告已持久。
- **恢复**：`Inspect` 报告一个 artifact 的状况（完整记录数、撕裂尾部字节数、不可修复原因），`Recover` 只修复撕裂尾部。

本包不负责：Agent 轮次语义、压缩、附件引用、以及跨机器协调。

## 使用

```sh
go test -race -count=1 ./plugins/sessionjsonl/...
```

恢复一个被中断写入的会话：

```go fragment
analysis, err := provider.Inspect(ctx, "s-1")
if err != nil {
	return err
}
if analysis.Reason != "" {
	// 中间损坏：拒绝，而不是悄悄缩短历史。
	return fmt.Errorf("corrupt: %s", analysis.Reason)
}
if analysis.Repairable {
	// 只有撕裂尾部会走到这里；修复只截掉不完整的字节，
	// 已提交前缀逐字节不变。
	if _, err := provider.Recover(ctx, "s-1"); err != nil {
		return err
	}
}
```

## 理解实现

**记录是薄包装。** 每条记录只加三样东西：序号、事件自身 JSON 的**原样**嵌入、以及该 JSON 的 SHA-256。校验和放在记录层，所以「事件能否解码」与「字节是否被改动」是两个独立的判断，报错也能分开。

**两个写者拒绝点，顺序有讲究。** 先取进程内声明，再取磁盘 lease：只有 lease 会漏掉同进程的两个句柄，只有声明会漏掉第二个进程。任一步失败都要回滚前一步，否则一次失败的打开会把会话永久锁死——这一条在测试里断言。

**恢复用截断而不是重写。** 丢掉撕裂尾部就是截断到最后一个完整记录；被保留的字节没换位置，所以前缀逐字节相同，不需要临时文件加改名。修复用 `os.Truncate`，并因此需要文件的写权限（而不是目录的写权限）。

**错误分类是有意义的。** 外部格式、未来版本、撕裂尾部、中间损坏、序列间隙各有自己的错误值：调用方对它们的处置完全不同，合成一个错误会逼调用方去解析字符串。

## 进一步阅读

- [persistence](../../persistence/README.md)：本后端实现的契约。
- [会话事件日志(../../session/README.md)：被存储的事件与 surface 规则。
- [格式与损坏](../../docs/persistence/format.md)：记录边界、版本与恢复步骤。
- [恢复与 checkpoint](../../docs/persistence/recovery.md)：重开之后哪些工作会继续。

## 验证

第一条命令用竞态检测跑本包的全部行为测试：帧格式、单写者、flush 与损坏恢复。第二条命令确认本包出现在覆盖率门禁清单里且不低于 90% 语句覆盖率。

```sh
go test -race -count=1 ./plugins/sessionjsonl/...
go run ./scripts/doccheck -coverage coverage.out
```

## 限制

- **不兼容 DSH Session 格式**：格式身份与目录布局都是本项目自己的；原始日志互读属后续里程碑。
- **无压缩**：spill、compaction 与附件属后续里程碑。
- **无跨机器协调**：单写者是同一数据目录内的规则。
- **不做跨进程崩溃注入**：崩溃恢复用「截断副本 + 重开」模拟，不声称覆盖所有平台的中断电。
- **`Sync` 的强度取决于平台**：本包调用 `os.File.Sync`，但不对文件系统是否真正落盘做额外承诺。
