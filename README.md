---
description: "通过可运行的 Go 插件示例学习 DSH 的依赖、生命周期与组合方式。"
kind: "project-overview"
---

# deepseek-harness-go

## 概述

你可以用 Go 写出插件、声明服务依赖，并观察依赖消失和恢复时插件如何停止与重新启动。本仓库面向已经掌握 Go 函数、接口、错误处理和基本 goroutine 用法，想理解 DeepSeek Harness（DSH）架构的读者。当前可运行部分是 Cordis 核心 v0.1，使用 Go 1.22 和标准库；实现范围与差异由[源码对应表](docs/cordis/source-map.md)说明。

## 目录

- [快速开始](#快速开始)
- [学习路径](#学习路径)
- [开发与验证](#开发与验证)
- [授权](#授权)

## 快速开始

安装 Go 1.22 或更新版本和 Git，然后在终端执行：

```sh
git clone https://github.com/gosomea/deepseek-harness-go.git
cd deepseek-harness-go
go run ./examples/cordis
```

示例不需要模型密钥或网络服务，成功时输出如下。计数器出现后 greeter 启动；计数器移除后 greeter 停止并等待，新计数器出现后它重新启动。

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

如果 Go 提示找不到 module，请回到包含 `go.mod` 的仓库根目录。注册依赖尚未出现时得到 `pending` 是这个示例的正常结果；定位其他状态和错误可查[开发说明](docs/development.md#调试生命周期)。

## 学习路径

这条路径从概念进入可运行程序，再转向行为参考与源码。第一次学习无需先阅读验收报告。

1. [从 Go 理解 Cordis 与 DSH](docs/cordis/go-primer.md)：把接口、注入、取消和清理与 DSH 概念对应起来。
2. [写第一个插件](docs/cordis/tutorial.md)：运行最小插件，再加入服务依赖和恢复。
3. [生命周期](docs/cordis/lifecycle.md)、[服务](docs/cordis/services.md)与[事件](docs/cordis/events.md)：按正在修改的行为查阅时序、错误和并发规则。
4. [源码对应表](docs/cordis/source-map.md)：从一个问题进入 Go 实现与固定版本的 TypeScript 源码。

[学习地图](docs/learning/index.md)把当前 Cordis 主题与后续完整 DSH 的概念串起来；[文档索引](docs/README.md)提供按问题查阅的入口。[项目架构](docs/architecture.md)描述当前运行时，[系统复刻方案](docs/10-plans/dsh-go-replication/plans.md)规划后续实现及每阶段配套的源码对照、学习实验与门禁。

## 开发与验证

准备修改代码时，先读[贡献指南](CONTRIBUTING.md)和[开发规则](AGENTS.md)。macOS/Linux 安装 Make 后运行 `make check`；完整检查包含竞态检测，需要启用 CGO 并具备本机 C 编译器。Windows 可按[测试说明](docs/testing.md)执行同样的 Go 命令，CI 使用 Bash。

每个包交付自己的 README、公开注释、示例和行为测试。[文档规范](docs/documentation.md)定义 Go 版的模板与示例标记，[测试与门禁](docs/testing.md)定义可执行验收要求。Go module 为 `github.com/gosomea/deepseek-harness-go`，领域包、示例与文档的位置见[架构](docs/architecture.md#仓库布局)。

## 授权

本项目基于 Cordis 的行为使用 Go 组织实现，授权见 [MIT License](LICENSE)，参考来源与版权见[第三方声明](THIRD_PARTY_NOTICES.md)。
