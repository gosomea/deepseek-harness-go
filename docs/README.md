---
description: "按 Go 学习路径或行为问题查找 Cordis 教程、参考与开发验收说明。"
kind: "documentation-index"
---

# 文档索引

## 概述

这里帮助你选择下一篇文档：第一次学习从 Go 概念对照和可运行教程开始，修改某个行为时直接进入参考页。教程负责操作与结果，参考负责时序、错误和所有权。项目路线与验收证据有单独入口。

## 目录

- [学习路径](#学习路径)
- [查阅参考](#查阅参考)
- [开发与验收](#开发与验收)

## 学习路径

读者需要掌握 Go 函数、接口与错误处理；goroutine 和取消的基础有助于后续理解并发约定。

1. [从 Go 理解 Cordis 与 DSH](cordis/go-primer.md)：认识 Plugin、Fiber、Context 与运行时注入。
2. [写第一个插件](cordis/tutorial.md)：从完整小程序进入服务等待和恢复。
3. [运行组合示例](../examples/cordis/README.md)：加入事件，观察监听器释放。
4. [源码对应表](cordis/source-map.md)：按问题阅读 Go 与固定版本的 DSH 源码。

[完整学习地图](learning/index.md)标明当前可学的四个主题和后续 C05–C16 的前置关系。新增主题按[学习笔记模板](learning/note-template.md)同时交付概念对照、完整实验和失败定位；未来主题在实现前保留规划状态。

## 查阅参考

按当前的问题选择页面，参考页可以独立查阅：

| 问题 | 文档 |
| --- | --- |
| 插件何时启动、失败后如何恢复、谁释放资源？ | [生命周期](cordis/lifecycle.md) |
| 类型如何检查、如何隔离服务、依赖何时重启？ | [服务与作用域](cordis/services.md) |
| 事件如何继续或短路、哪些操作会并发？ | [事件](cordis/events.md) |
| 某个公开方法的参数与返回值是什么？ | [生成 API](cordis/api.md) |
| 当前运行时由哪些部分组成？ | [架构](architecture.md) |
| 后续 Harness 模块按什么顺序实现？ | [路线图](roadmap.md) |
| Go 实现与固定 TypeScript 参考怎样对照，差异记在哪？ | [差分场景说明](../testdata/parity/cordis/README.md)、[已声明差异](../testdata/parity/cordis/DIVERGENCES.md)、[对照工具](../internal/testkit/README.md) |
| DSH 的每个部分如何分配到 Go，怎样算复刻完成？ | [系统方案](10-plans/dsh-go-replication/plans.md)、[能力覆盖](10-plans/dsh-go-replication/capabilities.md)、[共同验收](10-plans/dsh-go-replication/acceptance.md) |
| 某个阶段具体先做什么、改哪些文件、怎样学习和验收？ | [阶段实施索引](10-plans/dsh-go-replication/stages/index.md) |
| 配置文档怎么写、命令行怎么检查、出错怎么看？ | [装配教程](loader/tutorial.md)、[dsh-go 命令](../cmd/dsh-go/README.md) |
| 配置条目和插件实例为什么分开、工厂目录怎么建？ | [loader 包](../loader/README.md)、[Go 概念对照](loader/go-primer.md) |
| 条目怎么组成父子树、关闭时谁释放？ | [装配树](loader/entry-tree.md) |
| 运行中改配置、禁用、跨组移动各自什么语义？ | [更新与启停](loader/update-semantics.md) |
| 多个配置来源谁覆盖谁、哪些条目必须成功？ | [app 包](../app/README.md)、[Bundle 与就绪](loader/profile.md) |

## 开发与验收

修改前阅读[开发说明](development.md)、[文档规范](documentation.md)和[贡献指南](../CONTRIBUTING.md)。提交检查由[测试与门禁](testing.md)定义；新增模块同时增加本索引与包 README。

[系统复刻方案](10-plans/dsh-go-replication/plans.md)及其 [M0 状态](10-plans/dsh-go-replication/status.md)是下一阶段入口。[Cordis v0.1 方案](10-plans/cordis-v01/plans.md)与[Go 学习文档方案](10-plans/go-learning-docs/plans.md)保留各自版本的历史；验收报告用于查验证据，不是学习前置材料。
