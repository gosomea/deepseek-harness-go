---
description: "把技能发现与按需加载分开：目录只列描述，启用才把内容放进请求。"
kind: "package-library"
---

# plugins/skills

## 概述

这个包把**「有什么可用」与「模型实际拿到什么」分成两个事实**。一个技能目录只是清单；被启用的技能才是进入请求的内容。把清单当成请求，是 Agent 拿到自己从未启用的指令、或为从不使用的技能付 token 代价的常见原因。

因此发现与加载是两个步骤、两种结果，并且已加载的技能可以卸载——**卸载后不得留下任何残留内容**。

读者需要 Go 的结构体与 map 基础。当前实现范围是 M8.3 的发现、按需启用与卸载清理；技能的 token 预算属 M8.5。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- `Skill` 描述一个可发现技能：名称、描述、正文与依赖列表。描述与正文分开存，因为前者进清单（便宜）、后者进请求（昂贵）。
- `Entry` 是清单行，**不带 Body 字段**，所以调用方拿到的类型里根本没有正文，无法误把目录内容放进请求。
- `Catalogue.Discover` 按名称排序返回清单；`Register` 拒绝重复名与非法技能。
- `Loader.Load` 启用技能并**传递地启用其依赖**；`Unload` 卸载技能并**传递地卸载依赖它的技能**。
- `Loader.Loaded` / `Render` 只返回已启用的内容；`DependencyClosure` 让调用方在加载前先看到代价。

## 使用

先登记技能，再按名称启用。下面的片段假定错误会被检查；`Load` 会对未知名称、缺失依赖与依赖环失败。

```go fragment
catalogue := skills.NewCatalogue()
err := catalogue.Register(skills.Skill{
	Name: "base", Description: "the base skill", Body: "base body",
})
err = catalogue.Register(skills.Skill{
	Name: "leaf", Description: "depends on base", Body: "leaf body",
	DependsOn: []string{"base"},
})

loader := skills.NewLoader(catalogue)
entries := catalogue.Discover()      // 清单：只有名称、描述与依赖
err = loader.Load("leaf")            // 传递启用 base
text := loader.Render()              // 只有已启用技能的正文
```

## 理解实现

- **依赖被启用而不是假定存在**：否则请求会引用一个前提从未加载的技能。
- **重复 `Load` 是幂等的**：不会把同一个技能的内容放两遍。
- **`Unload` 连带卸载依赖者**：留下一个依赖已消失的技能，会产生与自身声明的依赖相矛盾的请求。
- **`Unload` 未启用的技能是错误而非静默成功**：否则调用方会以为移除了它从未添加的内容。
- **清单按名称排序**：两次以不同顺序登记的构建，列出同样的顺序。
- **`Entry` 没有正文**：类型层面就杜绝了「把目录当请求」。

## 进一步阅读

- [技能清单与实际加载的 token 差异](../../docs/context/skills.md)：清单与请求为何必须分开记账。

## 验证

```sh
go test -race -count=1 -timeout=240s ./plugins/skills/...
```

覆盖：清单排序与不含正文、传递启用与幂等、未知名称/缺失依赖/依赖环拒绝、卸载连带依赖者与不误伤无关技能、卸载未启用技能与未知技能拒绝、渲染为已启用集合的纯函数、依赖闭包报告，以及非法与重复登记的拒绝。

## 限制

- **不含 token 预算与截断**。技能体积如何进入总预算属 M8.5。
- **不写 Session 日志**。本包只管理启用状态，不负责把这些状态记录成日志事实。
- **不解析技能文件**。从磁盘或目录读技能属后续切片；本包只管理已登记的技能值。
