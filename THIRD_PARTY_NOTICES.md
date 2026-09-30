# 第三方声明

## Cordis

本项目 Cordis 核心参考 DeepSeek Harness 的 `vendor/cordis`（`@deepseek-ai/cordis` 4.0.4）的行为与接口，使用 Go 重新组织实现。参考版本固定为公开仓库的 Git 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`。参考目录与各文件对应见 [源码对应表](docs/cordis/source-map.md)。

Cordis 版权：Copyright (c) 2021-present Shigma。原始代码使用 MIT License，完整授权文本保留在本项目 [LICENSE](LICENSE)。源码来源：[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) 与 [Cordis](https://github.com/cordiverse/cordis)。

## Go 标准库

运行时、示例和门禁工具均只依赖 Go 标准库，没有新增第三方 Go module。执行计划使用本机已有的 `plan-and-phase` 技能及其 Python 依赖；这些工具不进入运行时依赖。
