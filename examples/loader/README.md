---
description: "用一份 JSON 文档装配插件树，并对照声明树与运行树观察每个条目的状态。"
kind: "command-example"
---

# examples/loader

## 概述

这个示例把一份内联的 JSON 文档装配成一棵插件树，然后分别打印声明树与运行树，最后按条目列出状态。它演示的是 `dsh-go` 检查命令使用的同一套步骤，但把每一步暴露出来，便于读者对照 [教程](../../docs/loader/tutorial.md) 阅读。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

示例负责把「解析 -> 挂载 -> 观测 -> 关闭」串成一个可运行程序，并固定它的输出。它不实现插件语义，也不定义新的配置规则；那些属于 `loader` 与 `app`。

## 使用

从仓库根目录运行：

```sh fragment
go run ./examples/loader
```

输出先给出两种树的对照，再给出逐条目状态与汇总。文档声明了三个条目：`store` 发布服务，`cache` 依赖它，`off` 被禁用。

`examples/loader/expected.txt` 保存同一份输出，由测试逐字比较。

## 验证

从项目根目录运行 `go test -race -count=1 ./examples/loader`；输出快照见 `expected.txt`。阶段交付执行 `make check`。

## 限制

示例使用内联字符串而不是配置文件，并且只注册两个插件；它不演示模型调用、工具执行、持久化或远程能力，这些属于后续阶段。示例只支持显式编译期注册：不提供 npm 动态安装、模块 HMR 或原 profile 兼容。
