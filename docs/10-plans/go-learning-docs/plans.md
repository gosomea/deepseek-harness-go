# Go 视角学习文档与门禁方案

## 目标与范围

面向掌握 Go 基础、首次接触 DSH 的读者，修复已审查的教程对象名和覆盖率前置条件，建立可运行的阅读路径。目标为学习路径 [[goal:learning]]、文档规范 [[goal:structure]]、可执行示例 [[goal:examples]] 和真实验收 [[goal:validation]]。本方案继承 Cordis v0.1 的运行时与门禁要求。

## 选择

采用中文学习文档与英文 Go 注释；为库、示例命令、工具命令定义不同 README 模板，项目首页和索引按自身用途组织。保留职责、使用、限制、验证要求，增加元数据、概述、目录与非空检查。通用库不引入不适用的 npm/profile 或模型章节；双语与网站不在本次范围。

可运行 Go 代码块使用 `go runnable=<id>`，配套 `text output=<id>`；检查器在仓库 module 中逐个执行并比较 stdout。接口片段使用 `go fragment`；生成 API 保持由源码生成。文档引用已有输出时使用 `text output-file=<repo-path>` 并检查新鲜度。判定行为与测试由代码维护，教学顺序和概念准确性另作语义审查。

## 执行顺序

1. [[node:documentation]]：修改首页、包 README、入门与概念文档、来源链接、文档规则和检查器；执行工具拒绝样例、文档检查、可运行示例。
2. [[node:acceptance]]：依赖 documentation 的必要检查；执行完整 `make check` 和 diff 检查，再逐页审阅目标、失败与资源所有权说明。

本任务串行执行，语义审阅由同一代理单独完成，`independent: false`。不修改原始 DSH checkout 或既有冻结验收导出。规则变化增加有效/无效样例，不降低覆盖率或竞态检查。

## 验证与发布

原 Cordis 验收对应旧产物，本次验证以 [status.md](status.md) 和新导出为准；旧报告保留历史。检查包含教程编译失败、运行失败、输出不符、空章节、元数据错误及原有拒绝路径。用户已授权公开 GitHub 仓库；本地通过后提交推送，另行核对对应提交的 Linux/macOS/Windows CI，不将本地证据视为远端通过。
