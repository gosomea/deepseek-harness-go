---
description: "限制范围：单向升级阶梯、执行时校验，以及「所需限制无法强制时拒绝启动」。"
kind: "package-library"
---

# sandbox

## 概述

`sandbox` 定义一次调用在什么限制下运行，以及它**能升到多宽**。

阶梯是**单向**的：`read-only` → `workspace-write` → `danger-full-access`。"比要求更受限"允许，"比要求更宽"不允许，且不向更窄方向升级。校验发生在**执行时**而不是 tool schema 里——schema 是全局的，而有效模式属于单次调用。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- **定义模式**：三种限制级别与其排序。
- **定义升级**：`ValidateEscalation` 只接受严格更宽的目标；同模式请求被拒绝而非忽略。
- **定义策略**：`Policy` 同时记录「当前生效」与「平台能强制」，`Validate` 在生效宽于可用时拒绝。
- **列出目标**：`EscalationTargets` 给出封闭词汇表（`read-only` 是地板，不可升入）。

## 使用

下面这个片段先校验策略能否被强制，再判断一次升级是否合法。

```go fragment
policy := sandbox.Policy{Effective: sandbox.ModeWorkspaceWrite, Available: sandbox.ModeDangerFullAccess}
if err := policy.Validate(); err != nil {
	// 生效模式比平台能强制的更宽：拒绝启动，而不是不带限制地运行。
	return err
}
if err := sandbox.ValidateEscalation(policy.Effective, sandbox.ModeDangerFullAccess); err != nil {
	return err
}
```

## 理解实现

**为什么同模式请求也被拒绝。** 调用方请求一次它并不需要的升级，通常说明它误读了有效模式。静默接受会把这次误读藏起来。

**为什么目标列表是完整的而不是按默认值裁剪。** 如果只广告"比本次组合默认更宽"的模式，一个有效模式低于默认的会话就会既受限制、又拿不到任何杠杆。

**为什么"可用"与"生效"要分开。** 它们回答不同问题：现在适用什么，以及**至多**能适用什么。程序可以被配置得比平台允许的更窄。

## 进一步阅读

- [审批与沙箱升级](../../docs/execution/approval.md)：升级请求怎样与审批配合。
- [能力与执行世界](../../docs/execution/capabilities.md)：拒绝而非降级的整体规则。

## 验证

```sh
go test -race -count=1 ./capability/sandbox/...
```

测试覆盖阶梯的每个方向（含同模式与反向）、目标列表完整性与不可强制策略的拒绝。

## 限制

- **不实现隔离**：本包只表达意图与校验，实际限制由 Provider 承担。
- **无平台探测**：可用模式由调用方告知。
- **不定义审批**：是否询问由 `capability/approval` 负责。
