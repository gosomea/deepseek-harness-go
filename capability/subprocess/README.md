---
description: "进程 seam：运行命令、限定输出、让取消终止整棵进程树；平台能力不足时拒绝而非降级。"
kind: "package-library"
---

# subprocess

## 概述

`subprocess` 的问题是：**一条命令是副作用，谁来决定它能不能发生？**

答案是：本包只定义 seam，策略由调用方在此之前完成。同时它把三件容易含糊的事写成契约——输出上限、进程树取消、平台能力**报告而非假定**。

参数**不经过 shell**：一个含空格的值就是一个参数，而不是两个词。这样一次参数注入不会变成第二条命令。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- **定义请求**：程序、参数、工作目录、环境、标准输入、超时、输出上限。
- **定义结果**：退出码、标准输出与错误输出、是否被截断、是否超时、是否取消、耗时。
- **区分成功**：`Succeeded` 要求零退出**且**未被停止、未被截断——被切断的输出不是成功。
- **报告能力**：`Capabilities` 说明平台能否承诺进程树终止与沙箱；缺能力时 `Supports` 返回拒绝。

## 使用

下面这个片段运行一条命令，并区分「成功」与非成功：非零退出、超时、取消与截断都不算成功。

```go fragment
result, err := runner.Run(ctx, subprocess.Request{
	Program: "git", Arguments: []string{"status", "--short"},
	Timeout: 10 * time.Second, MaxOutputBytes: 64 << 10,
})
if err != nil {
	return err
}
if !result.Succeeded() {
	// 非零退出、超时、取消与截断都到这里。
	return fmt.Errorf("exit=%d timedOut=%v truncated=%v", result.ExitCode, result.TimedOut, result.Truncated)
}
```

## 理解实现

**为什么 nil 环境表示空环境。** 继承父环境等于把本进程持有的每个凭据交给命令。契约明确写为「nil 表示空」，Provider 负责把 nil 变成空而非继承——Go 的 `exec.Cmd` 恰好相反（nil 表示继承），这个差异是 `plugins/local` 里一个真实缺陷的来源。

**为什么取消要终止进程树。** 只杀直接子进程，脚本的后代仍会活在调用之外。seam 因此把「进程树终止」列为可报告的能力，而不是隐含假设。

**为什么能力要报告。** 需要隔离的调用方必须能**拒绝启动**，而不是在一次副作用已经发生之后才发现平台给不了。

## 进一步阅读

- [本机进程 Provider](../../plugins/local/README.md)：进程组与输出上限的实现。
- [能力与执行世界](../../docs/execution/capabilities.md)：为什么"缺能力要拒绝"。
- [平台矩阵](../../docs/execution/platform-matrix.md)：各平台的实际证据状态。

## 验证

```sh
go test -race -count=1 ./capability/subprocess/...
```

测试覆盖请求校验、成功判定（非零/超时/取消/截断各自不算成功）与能力拒绝。

## 限制

- **只定义接口**：本包不启动进程。
- **不提供真实沙箱**：`Sandboxed` 是报告字段，本包不实现隔离。
- **平台差异由 Provider 承担**：Windows 的树终止路径在本机未验证。
