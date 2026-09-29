# deepseek-harness-go

用 Go 逐步重建 DeepSeek Harness 的插件架构。当前实现 **Cordis 核心 v0.1**，用于学习、验证插件组合和为后续 Harness 模块建立基础。项目使用 Go 1.22 和标准库，Go module 为 `github.com/gosomea/deepseek-harness-go`；尚无模型调用、会话持久化或 Agent CLI。

## 快速开始

```sh
git clone https://github.com/gosomea/deepseek-harness-go.git
cd deepseek-harness-go
go version
make check
go run ./examples/cordis
```

在本项目根目录执行。Windows 的 CI 使用 [工作流中的 Go 命令](.github/workflows/check.yml)，不依赖 Make。示例的完整预期输出在 [expected.txt](examples/cordis/expected.txt)，每次门禁都会比对。

## 阅读顺序

1. [项目架构](docs/architecture.md)：为什么先写 Cordis，后续模块放在哪里。
2. [第一个插件](docs/cordis/tutorial.md)：运行示例，观察服务加入、消失和恢复。
3. [生命周期](docs/cordis/lifecycle.md)、[服务与作用域](docs/cordis/services.md)、[事件](docs/cordis/events.md)：三个核心行为说明。
4. [源码对应表](docs/cordis/source-map.md)：从 Go 实现回到本机 DSH 的 TypeScript 参考。
5. [API](docs/cordis/api.md)：由公开 Go 注释生成的接口参考。
6. [测试与门禁](docs/testing.md)：如何判断本阶段可以交付。
7. [实施计划](docs/10-plans/cordis-v01/plans.md)：目标、依赖和验收证据。

## 目录

```text
cordis/                插件、生命周期、服务、作用域和事件
examples/cordis/        完整可运行示例与输出快照
docs/                  架构、教程、行为说明、开发与测试
docs/10-plans/          执行计划与阶段验收证据
scripts/doccheck/      文档、API、覆盖率门禁及失败样例
.github/workflows/     独立仓库 CI 配置
```

后续模块的布局和阶段条件由 [架构文档](docs/architecture.md) 与 [路线图](docs/roadmap.md) 定义。只在开始实现模块时创建源码目录。

## 开发约定

每个 Go 包同时交付包说明、公开接口注释、行为测试与使用示例。修改代码时同步维护对应的文档；生成的 API 通过 `make api` 更新。[AGENTS.md](AGENTS.md) 约束后续 AI 修改，[贡献指南](CONTRIBUTING.md) 说明人工修改流程。

本项目是基于 Cordis 行为的 Go 实现；与 TypeScript 的差异详见 [源码对应与范围](docs/cordis/source-map.md)。授权与参考源码见 [第三方声明](THIRD_PARTY_NOTICES.md) 和 [MIT License](LICENSE)。
