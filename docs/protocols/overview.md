# 协议与远程能力

## 读完要解决的问题

为什么内部 Go interface 不能直接充当稳定 wire 协议？读完后你能：区分内部接口与 wire DTO；用版本信封与请求 id 关联请求／响应；解释 keyless 契约与真实外部服务 smoke 的区别。需要 Go 的 JSON、`context` 与 `io` 基础。当前实现是 `protocol`、`plugins/hostapi`、`plugins/sdk`、`plugins/mcp`、`plugins/lsp`、`plugins/ssh`、`plugins/hooks`、`plugins/web`。

## 从熟悉的 Go 写法出发

普通 Go 里，一个接口值可以跨包传递，编译器会在调用点检查签名。但**跨进程**时没有编译器：一个 wire 消息改了字段名，编译一切正常，客户端在运行时才坏。DSH 因此把「内部 Go 接口」与「稳定 wire DTO」分开：DTO 命名自己的字段、冻结版本、把未知当显式条件。

## 最小实验

在仓库根目录：

```sh
go run ./examples/sdk
```

输出逐字保存在 [examples/sdk/expected.txt](../../examples/sdk/expected.txt)：

```text output-file=examples/sdk/expected.txt
echo result: hi
unknown method code: unsupported
protocol version: 1
```

观察重点：客户端把调用变成带版本的 `Envelope`，网关按 id 返回响应，客户端按 id 校验；一次未知方法得到 `unsupported` code。

## 先预测，再观察

先预测：响应 id 与请求不符时客户端会怎样？写出预期再对照实现——`sdk` 在 `resp.ID != request id` 时返回错误，而不是把别人的答案当自己的。这正是不按到达顺序匹配的原因。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 稳定 wire 协议、请求关联、版本协商、能力声明 |
| TypeScript 入口 | 固定提交 `packages/api`、`packages/host`、`packages/sdk`、`packages/acp` |
| Go 入口 | [protocol](../../protocol/README.md)、[plugins/hostapi](../../plugins/hostapi/README.md)、[plugins/sdk](../../plugins/sdk/README.md) |
| 保持的行为 | 版本不符拒绝；未知方法明确 code；请求 id 关联；认证只做一次 |
| Go 表达 | DTO 结构体 + 信封分发 + SendFunc 客户端 |
| 不等价边界 | keyless；真实外部服务与 Python SDK 属后续 |

## 一次操作的时序与所有权

客户端构造 `Envelope` → 发送函数（测试里是网关本身）→ 网关校验→解析方法→认证→处理器→`Response` → 客户端按 id 校验并解码。取消经 context 表达。

## 失败与定位

- **`ErrVersionMismatch`**：版本不符。
- **`CodeUnsupported`**：未知方法。
- **`ErrUnauthorized`**：缺／错凭据。
- **响应 id 不符**：关联错误，客户端报错。

## 回到源码与下一步

先读 [protocol](../../protocol/README.md)，再看 [兼容矩阵](compat-matrix.md) 了解每个协议的证据与未验证项。
