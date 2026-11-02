---
description: "审批渠道：请求绑定到具体调用，四种结果中只有 allowed-once 授权，headless 政策没有 allow。"
kind: "package-library"
---

# approval

## 概述

`approval` 定义副作用发生前的询问：**一个绑定到具体调用的请求，与一个一次性答复。**

词汇表的设计目标是让**安全答案是"什么都不做"**。除显式授权外的每一种结果——无人应答、被拒绝、等待被取消——都**不授权**；headless 政策必须说明自己产生哪一种，而不是落到"允许"。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- **定义结果**：`allowed-once`、`rejected`、`cancelled`、`unavailable`，其中只有第一种授权。
- **定义请求**：交换身份、**绑定的调用身份**、工具名、动作描述、可选过期时间。
- **定义政策**：`deny`（不问，直接拒绝）与 `ask-once`（问，但无人应答即拒绝）。
- **实现网关**：一个交换只记录一个答复；重复呈现不产生第二个答复；迟到答复不能改写已经执行的决定。

## 使用

下面这个片段请求一次审批：只有 `allowed-once` 会继续，其余三种结果一律不授权。

```go fragment
gateway, err := approval.NewGateway(responder, approval.PolicyAskOnce)
if err != nil {
	return err
}
decision, err := gateway.Request(ctx, approval.Request{
	ID: "edit_file:call-1", CallID: "call-1", Tool: "edit_file", Action: "write notes.txt",
})
if err != nil {
	return err // 只有请求本身非法才返回错误
}
if !decision.Granted() {
	// 无人应答、拒绝与取消都到这里：一律不授权。
	return fmt.Errorf("not authorized: %s", decision.Reason)
}
```

## 理解实现

**为什么三种失败结果是分开的。** 「操作员说不行」与「没人在看」需要不同的后续处置；合成一个值会让报告失去可操作性。

**为什么请求必须绑定调用身份。** 一次授权只对它针对的那次调用有效。绑定到"无"的授权是**一揽子许可**——这正是请求在缺少调用身份时被拒绝的原因。

**为什么重复呈现不产生第二个答复。** 否则调用方只要反复询问就能拿到一个"是"。同理，迟到答复不能把已经据以行动的拒绝改写成授权。

**为什么没有 allow 政策。** headless 运行若静默允许副作用，其他每一道保护都会变成可选项。

## 进一步阅读

- [审批与沙箱升级](../../docs/execution/approval.md)：三种不授权结果与升级如何配合。
- [本机工具](../../plugins/local/README.md)：工具怎样在写文件前请求审批。

## 验证

```sh
go test -race -count=1 ./capability/approval/...
```

测试覆盖请求身份校验、四种结果的授权判定、headless 政策、重复与迟到答复、过期请求与并发呈现。

## 限制

- **不实现界面**：谁来回答由调用方提供（终端、编辑器、测试替身）。
- **不持久化决定**：网关是进程内的；跨进程审批属后续里程碑。
- **不定义策略来源**：配置怎样给出政策由应用层决定。
