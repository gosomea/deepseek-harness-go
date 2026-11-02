# 文档网站（骨架）

本文件是文档网站的结构骨架，用于把全域知识组织成可导航入口。实际静态站点发布（双语、托管）属发布缺口，见 [发行缺口](../release/gaps.md)。

## 语言范围

- 主语言：简体中文（内容）。
- 英文：仅代码、标识符与协议枚举值保留原文；不承诺完整英文翻译。

## 导航

| 层次 | 入口 |
| --- | --- |
| 快速开始 | [README](../../README.md) |
| 学习地图 | [learning/index.md](../learning/index.md) |
| 架构 | [architecture.md](../architecture.md) |
| 全域账本 | [conformance/ledger.md](../conformance/ledger.md) |
| 兼容缺口 | [compatibility/gaps.md](../compatibility/gaps.md) |
| 发行缺口 | [release/gaps.md](../release/gaps.md) |
| 协议 | [protocols/overview.md](../protocols/overview.md) |
| 客户端 | [client/overview.md](../client/overview.md) |

## 边界

- 本骨架不包含静态站点生成器与部署配置；那属 [M12.8](file:///dev/null) 发行缺口。
- 导航与真实模块一致由 `make check` 的 Markdown 链接检查保证。
