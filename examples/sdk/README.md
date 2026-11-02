---
description: "用 Go SDK 客户端经进程内网关调用方法，并观察未知方法的 unsupported code。"
kind: "command-example"
---

# examples/sdk

## 概述

这个程序用 `sdk` 客户端把一次方法调用变成带版本的 `protocol.Envelope`，经进程内 `hostapi` 网关分发并按 id 校验响应；再对未知方法观察 `unsupported` code。它让内部 Go 接口与稳定 wire 协议的差别有可对照的实体。

## 目录

- [职责](#职责)
- [使用](#使用)
- [验证](#验证)
- [限制](#限制)

## 职责

- 演示请求／响应按 id 关联。
- 演示未知方法得到 `unsupported` code 而非静默。
- 演示协议版本是冻结整数。

## 使用

```sh
go run ./examples/sdk
```

输出逐字保存在 [expected.txt](expected.txt)，并由 `TestDemoMatchesDocumentedOutput` 比对。

## 验证

```sh
go test -count=1 ./examples/sdk/...
go run ./examples/sdk
```

两者都应退出 0；输出应与 `expected.txt` 完全一致。

## 限制

- **进程内**。无套接字传输；网络传输是同一调用约定上的另一个发送函数。
- **keyless**。不连真实服务器。
