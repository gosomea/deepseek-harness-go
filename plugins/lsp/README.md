---
description: "LSP：LSP（Language Server Protocol）stdio 客户端。"
kind: "package-library"
---

# plugins/lsp

## 概述

LSP（Language Server Protocol）stdio 客户端。本包提供一个小而 keyless 的 seam，规则（关联、生命周期、错误可见性）在进程内即可证明，真实外部服务单列为未验证缺口。

读者需要 Go 的 `context` 与 `io` 基础。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

见下方实现与测试覆盖的行为。核心不变量：请求／响应按 id 关联，失败可见，资源释放明确。

## 使用

下面的片段展示最小调用形态；具体方法名取决于对端。

```go fragment
client := lsp.New(os.Stdout, os.Stdin, nil)
resp, err := client.Call(lsp.Request{Method: "initialize"})
_ = resp
_ = err
```

## 理解实现

- **id 关联**：响应 id 与请求不符即拒绝。
- **失败可见**：错误返回给调用方，不静默吞掉。
- **释放明确**：`Close` 幂等，transport 的 teardown 由 close 钩子承担。

## 进一步阅读

- [协议与远程能力](../../docs/protocols/overview.md)

## 验证

下面的命令运行本包的测试；测试用内存 reader/writer 或内存模型，不连真实服务器。

```sh
go test -race -count=1 -timeout=240s ./plugins/lsp/...
```

## 限制

- **keyless**。真实外部服务器／主机 smoke 缺凭据记为未验证，不是通过。
