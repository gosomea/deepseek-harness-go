---
description: "在启动之前检查或 dump 一份 loader 配置文档，并把失败分成可区分的退出类别。"
kind: "command-tool"
---

# dsh-go

## 概述

你可以用这个命令检查一份配置文档能不能装配：它按文档声明的名称解析插件、挂载、报告每个条目的结果，然后释放整棵树。它也可以 dump 组合后的声明树，便于与配置文档逐行比对。检查会真实挂载并卸载，所以它同时验证了「能不能起来」和「能不能收回」。

命令不执行任何任务、不读写 Session、不留后台资源。本阶段只支持显式编译期注册：不提供 npm 动态安装、模块 HMR 或原 profile 兼容。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

本命令是宿主的配置入口，复用与示例相同的装配步骤：解析文档 -> 建立插件目录 -> 挂载 -> 观测 -> 关闭。它不定义插件语义、不实现模型调用或工具执行，也不管理 Session。

## 使用

在仓库根目录保持终端位置，先准备一份文档：

```sh fragment
cat > /tmp/cordis.json <<'JSON'
{"entries":[{"id":"store","name":"store"},{"id":"cache","name":"cache"}]}
JSON
```

检查它：

```sh fragment
go run ./cmd/dsh-go -config /tmp/cordis.json -plugin store,cache
```

预期输出（顺序固定，可重复）：

```text
active cache
active store
summary active=2 pending=0 unmounted=0
```

参数：

| 参数 | 含义 |
| --- | --- |
| `-config` | 必填，配置文档路径。 |
| `-plugin` | 宿主声明存在的插件名，逗号分隔。文档只能引用这里列出的名称。 |
| `-mode` | `check`（默认）报告每个条目的结果；`dump` 打印声明树。 |

退出码：`0` 表示检查通过；`2` 表示命令行或输入不可用（缺 `-config`、未知 `-mode`、文件读不到、插件名重复）；`1` 表示文档被拒绝或条目未就绪，原因写到 stderr。

## 验证

从项目根目录运行 `go test -race -count=1 ./cmd/dsh-go` 验证本命令；阶段交付执行 `make check`。

## 限制

本命令只支持显式编译期注册：不提供 npm 动态安装、模块 HMR 或原 profile 兼容。它不启动 Agent、不调用模型、不执行工具，也不写入任何持久化状态。`-plugin` 只验证「文档引用的名称已声明」，不验证这些插件在完整应用中的真实配置——那要等 M4 之后的模型与工具切片。
