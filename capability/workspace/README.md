---
description: "执行世界：一个目录上的稳定身份，加上让「在这个工作区内」成为可判定问题的路径规则。"
kind: "package-library"
---

# workspace

## 概述

`workspace` 回答一个问题：**一条路径到底在不在这个世界里？**

答案是**逐元素比较 + 可选的符号链接解析**。相对路径接到根上、绝对路径按原样、结果清理后比较路径元素而非字符串前缀——否则 `/work` 与 `/workshop` 这种「前缀相同」的兄弟目录会被误判为包含。

身份是生成值，**不是路径**：路径会被规范化、可能经由链接抵达、大小写规则因平台而异；把锚点放在路径字符串上意味着锚点会随拼写漂移。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- **构造**：把根做成绝对且清理过的路径；开启链接解析时，根也一并规范化。
- **判读路径**：`Resolve` 给已存在的读路径，`ResolveForCreate` 给尚不存在的写路径（解析最深的已存在祖先，再拼回缺失的尾部）。
- **判包含**：`Contains` 让已经规范化的调用方问同一个问题而不必绕道 `Resolve`。

## 使用

下面这个片段建一个世界，并判读一条越界路径与一条待创建的路径。

```go fragment
world, err := workspace.New(workspace.Options{ID: "w-1", Root: dir, ResolveSymlinks: true})
if err != nil {
	return err
}
if _, err := world.Resolve("../escape.txt"); err != nil {
	// 世界之外：拒绝，而不是尽力执行。
	return err
}
path, err := world.ResolveForCreate("docs/new.txt")
if err != nil {
	return err
}
```

## 理解实现

**为什么根也要规范化。** 开启链接解析时，候选路径会被解析；如果根没有一并解析，两者就在不同拼写下比较。macOS 上临时目录经由 `/var` → `/private/var` 抵达，这个差异会让**世界内**的路径被判为「在外部」。这一条由 `TestWorldCanonicalizesItsRootWhenResolving` 断言。

**创建路径为什么要单独处理。** 叶子还不存在，无法整体解析。`ResolveForCreate` 向上找到最深的已存在祖先、解析它、再拼回缺失尾部——这样经由「符号链接父目录 + 不存在的叶子」的逃逸仍然被挡住。

**为什么默认不解析链接。** 解析有代价，而且会改变「根」的拼写。默认关闭是一种取舍：`New` 的文档写明了它的含义，需要链接逃逸检测的调用方显式开启。

## 进一步阅读

- [能力与执行世界](../../docs/execution/capabilities.md)：世界与 cwd 为什么必须一致。
- [本机文件系统](../../plugins/local/README.md)：使用世界规则限制读写。

## 验证

```sh
go test -race -count=1 ./capability/workspace/...
```

测试覆盖兄弟目录前缀、遍历、符号链接逃逸、创建路径祖先解析与根规范化。

## 限制

- **不创建目录**：只判读路径；建目录是 Provider 的事。
- **默认不解析链接**：见上文取舍。
- **大小写按平台**：不额外做大小写归一，平台怎么判就怎么判。
