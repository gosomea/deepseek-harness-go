---
description: "运行计数器与 greeter，观察依赖等待、卸载、恢复和事件监听器释放。"
kind: "command-example"
---

# Cordis 示例

## 概述

运行本例可以看到消费者等待服务、服务出现后启动、服务移除后停止，以及服务恢复后重新启动。计数器在内存中运行，不需要密钥或外部服务。示例同时演示事件监听器随所属插件清理，完整输出由快照测试固定。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

本例用 counter 提供 `Next()`，greeter 消费它并监听 ready 事件。插件定义与注册返回的 Fiber 分开保存，从而可以永久卸载某个实例，再用原定义注册新的实例。

## 使用

在项目根目录运行 `go run ./examples/cordis`，预期输出如下：

```text output-file=examples/cordis/expected.txt
before provider: pending
greeter started
hello #1
greeter stopped
after provider removal: pending
greeter started
hello again #1
greeter stopped
```

| 输出 | 观察到的行为 |
| --- | --- |
| before provider: pending | 消费者注册成功，等待必需服务 |
| greeter started / hello #1 | counter 可用，Apply 注册监听器并处理事件 |
| greeter stopped / after provider removal: pending | `p.Dispose()` 删除提供者实例，消费者释放监听器 |
| hello again #1 | 新提供者让消费者重新激活，新计数器从 1 开始 |
| 最后一行 greeter stopped | 宿主 Close 释放根树 |

“not delivered”不会出现，因为那次事件发生在 greeter 的监听器已移除时。完整代码见 [main.go](main.go)；想逐步写出插件时阅读[教程](../../docs/cordis/tutorial.md)，状态含义见[生命周期](../../docs/cordis/lifecycle.md)。

## 验证

运行 `go test ./examples/cordis`。测试调用同一个 `run` 并逐字比较 [expected.txt](expected.txt)；文档中嵌入的输出也与该文件比较。`make check` 执行这些检查；输出变化时同步审阅教学说明。

## 限制

这是串行 Emit 的内存示例，计数器没有并发保护。改用 Parallel 或并发发起事件时，需要为状态增加同步，并处理已经进入的监听器与卸载可能重叠的情况，见[并发约定](../../docs/cordis/lifecycle.md#并发与等待)。
