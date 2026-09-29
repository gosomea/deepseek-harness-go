# Cordis Go v0.1 执行方案

## 现状与目标

参考 DSH 的 `vendor/cordis` 核心，建立独立 Go 学习仓库，先实现生命周期与依赖基础。目标分别是仓库架构 [[goal:repository]]、可用 Cordis [[goal:runtime]]、完整文档 [[goal:documentation]] 和可执行验收 [[goal:gates]]。用户补充要求每个部分提供充足文档，并经过门禁；此要求已经纳入本版执行契约。

仓库已具备初始代码，本计划通过阶段验收检查这些产物并固定证据。不会将验收前的试运行自动记成通过。执行状态由 [status.md](status.md) 生成；机器契约见 [plan.json](plan.json)。

## 选择与取舍

| 方案 | 适用性 | 选择 |
| --- | --- | --- |
| 按 TS 文件和动态语言机制逐一移植 | 需要模拟 Proxy、装饰器和模块身份，学习成本大 | 仅保留源文件对应关系供阅读 |
| Go 接口与显式容器，保留依赖生命周期 | 便于类型化消费、标准库测试和后续 Harness 装配 | 采用 |
| 引入现成 DI 容器后包裹插件 | 需要额外建立服务变化、清理与实例状态的衔接 | 当前不引入 |

一个 Go module，Cordis 作为根层公共包。服务用名称与 Scope 作为注册键，Key[T] 提供消费点类型检查。生命周期串行协调，注册表互斥保护，用户回调锁外执行。分发器保留 Cordis 五模式和显式 Filter；源码差异由 [source-map](../../cordis/source-map.md) 维护。

## 阶段依赖与交付物

1. [[node:foundation]]：核对 Go 环境与 module，验收架构、路线图与来源/范围说明。
2. [[node:runtime]]：依赖 foundation；验收核心源文件、生命周期/事件测试和可运行示例。
3. [[node:acceptance]]：依赖上述两个必要门禁；执行 `make check`，验收文档、拒绝样例、覆盖率、API、示例与编译，再进行单独语义审阅。

本任务串行执行，不使用独立子代理；语义审阅记录 `independent: false`。每项必需检查非零、超时、缺少产物或审阅未通过，都会阻止相应节点完成。已经固定的上游产物后续变化会使证据失效。

## 约束与范围

只修改本项目。原 DSH checkout 作为只读参考。运行时零第三方 Go module，Go 1.22 可构建。公开接口、所有权与限制随代码交付；覆盖率当前检查 Cordis，未来新增运行时包必须扩展选择器。

没有 Loader、Session、Tool、Model、Agent Loop、CLI 或网站。当前没有真实 API 或跨平台本地验收；远端 CI 状态按提交记录。用户已授权将项目作为公开的独立仓库发布到 `gosomea/deepseek-harness-go`；后续路线见 [roadmap](../../roadmap.md)。

## 复现与证据

在本项目根目录运行 `make check` 和 `go run ./examples/cordis`。阶段 CLI 入口为本机 `/Users/yuqixian/forever-skills/skills/plan-and-phase/scripts/plan.py`；机器计划固定必要检查的 argv、超时、输入与产物。

验证导出保存在 [validation](validation/) 的新运行目录；`report.json` 包含检查结果、产物指纹和语义审阅，`report.md` 是入口。本地检查结果与 GitHub CI 分开记录。更改门禁必须保留有效/无效样例，不能降低门禁来接受既有失败。

## GitHub 发布准备（第二版）

用户授权建立公开仓库。Go module、import 和覆盖率统计选择器统一迁移为 `github.com/gosomea/deepseek-harness-go`，README 提供 clone 入口，原验收导出保持不变。本版本重新执行 foundation → runtime → acceptance；门禁下限与检查内容不变。本地全部通过后提交并推送，远端 CI 结果另行核对。

## Windows 检出修复（第三版）

首次 GitHub CI 的 Windows 格式检查失败；Linux 全部通过，macOS 各检查通过但任务被矩阵提前取消。增加 `.gitattributes` 固定文本 LF；矩阵设置 `fail-fast: false`，格式失败时列出文件。用 `core.autocrlf=true` 的本机临时 clone 复现并验证检出行为，再按原门禁重新验收；不降低任何检查要求。

## Windows 命令解析修复（第四版）

第二次 CI 的 Linux/macOS 全部通过；Windows 的格式与 vet 通过，测试命令在启动前报 `no required module provides package .out`。Windows 默认 PowerShell 拆分覆盖率 flag 的值；将工作流 run shell 统一为 Bash，保持竞态、覆盖率和其他门禁不变。对应提交的三平台 CI 作为最终远端证据。
