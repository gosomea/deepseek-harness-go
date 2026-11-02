---
description: "不产生副作用：让四次工具调用分别被准入、被 schema 拒绝、找不到工具与被策略拒绝。"
kind: "command-example"
---

# examples/tools

## 概述

这个程序不读写文件、不起进程：它注册一个 `read_file` 定义，然后执行四次调用——一次合法、一次缺少必填参数、一次工具名不存在、一次参数类型不对——并打印每个阶段的观察事件与规范化结果。最后它把同一份工具定义交给 `systemprompt` 装配，证明**模型看到的 schema 就是管线强制的 schema**。读它能让[执行管线参考](../../docs/tools/execution-pipeline.md)的每个阶段有可对照的实体。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

演示 M4 的四条核心行为：

1. **拒绝不进工具体**：三次失败调用的 `attempts=0`，且观察事件里没有 `stage=execute`。
2. **结果唯一且可诊断**：每次调用恰好一条结果，失败也带模型可见的原因。
3. **guard 单调**：先拒绝 `read_file` 再追加一个放行 guard，调用仍被拒绝。
4. **schema 一致**：提示词里的 `"required":["path"]` 与管线校验用的是同一份定义。

它不负责真实文件系统、子进程、沙箱与审批——那些属 M7。

## 使用

```sh
go run ./examples/tools
```

输出逐字保存在 [expected.txt](expected.txt)，并由 `TestDemoMatchesDocumentedOutput` 比对。

> 注意：用 `go build ./examples/tools/` 生成默认二进制名 `tools` 时会与仓库根的同名目录冲突，请写 `go build -o /tmp/tools ./examples/tools/` 或直接用 `go run`。

## 验证

```sh
go test -count=1 ./examples/tools/...
go run ./examples/tools
```

它由 `TestDemoMatchesDocumentedOutput` 逐字比对 `expected.txt`，并由 `TestDemoShowsRefusalsNeverReachTheBody` 断言三次拒绝的 `attempts=0` 且只有一次 `stage=execute`。

## 限制

- **工具体是测试替身**，不接触本机文件；真实执行、审批与沙箱属 M7。
- **schema 是受约束的子集**，未知 `type` 会报错而不是被忽略。
- **没有审批交互**：本阶段的准入只有 guard 链。
