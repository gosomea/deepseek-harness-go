---
description: "检查 README 结构、公开注释、链接、API、示例输出与 Cordis 覆盖率。"
kind: "command-tool"
---

# 文档门禁工具

## 概述

你可以在修改文档后检查必要章节、链接和公开注释，并发现 API 或示例输出是否已经过时。标记为可运行的 Go 程序可以单独执行，编译失败、运行失败或输出不符都会返回非零。工具使用标准库，教学顺序与行为准确性仍需对照实现审阅。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

工具读取 Go 包和 Markdown，按[文档规范](../../docs/documentation.md)检查 README 类型、必要内容和代码块分类，生成或比较 Cordis API，并检查覆盖率下限。[测试说明](../../docs/testing.md)维护阶段交付要求。

## 使用

以下命令均在项目根目录执行。先检查结构与链接，成功时打印 `documentation gate passed`：

```sh
go run ./scripts/doccheck
```

运行文档内的完整 Go 程序并比较预期输出，每个程序有 30 秒上限：

```sh
go run ./scripts/doccheck -examples
```

修改公开 Go 注释后重新生成 API，并再次检查文档：

```sh
go run ./scripts/doccheck -write-api
```

覆盖率检查需要已有报告。先运行测试生成 `coverage.out`，再读取它：

```sh
go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...
go run ./scripts/doccheck -coverage coverage.out
```

覆盖率成功时先打印 Cordis 的百分比，再打印通过消息。如果报告不存在，先执行生成命令；不能用空文件代替报告。完整交付可以使用 `make check`，它按顺序执行这些检查。

| 参数 | 含义 |
| --- | --- |
| `-root <目录>` | 结构、API 与示例检查的仓库根目录；可运行示例在这个 module 中执行 |
| `-examples` | 执行完整示例；不生成覆盖率或 API |
| `-write-api` | 更新生成 API，然后检查文档 |
| `-coverage <文件>` | 仅检查覆盖率；文件路径相对当前工作目录，与 `-root` 无关 |

每次调用选择一个操作参数。失败信息给出文件、代码块或缺失章节；修复该处后重新运行对应命令。

## 验证

运行 `go test ./scripts/doccheck`。拒绝样例覆盖错误元数据、空章节、代码块分类、编译和运行失败、输出不符，以及原有注释、链接、API 与覆盖率规则。`make check` 执行这些测试；改变规则时保留通过与拒绝样例。

## 限制

工具不判断概念解释是否友好或准确，也不检查远端 URL 可用性、引用式链接、重复标题编号或网站投影。Go fragment 只标明片段，不执行；生成 API 的声明代码由新鲜度检查维护。验证历史目录不重新检查。当前 API 与覆盖率选择器只覆盖 Cordis，新增公共运行时包必须扩展它们。
