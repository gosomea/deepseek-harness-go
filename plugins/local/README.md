---
description: "本机能力 Provider：受工作区限制的文件系统与进程运行，以及先过策略再动手的读、写与命令工具。"
kind: "package-library"
---

# plugins/local

## 概述

`plugins/local` 是 `capability` 五个 seam 在**本机**上的实现：受工作区限制的文件系统、有界输出与进程树取消的进程运行，以及在此之上的工具。

它是 **Provider，不是策略**。它执行的每个副作用都发生在调用方的策略检查之后；平台给不出配置所要求的承诺时它**拒绝**而不是降级。这条分工正是同一套工具能换一个世界运行、而工具本身毫不知情的原因。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- **`OSFS`**：读写、统计、列举与删除；写入支持条件检测与创建语义；读取有上限。
- **`OSProcess`**：不经 shell 地运行命令；输出有界并报告截断；取消终止整棵进程组。
- **工具**：`read_file`（只读，不询问）、`edit_file` 与 `run_command`（**先审批，再动手**）。
- **环境纪律**：`Env` 为 nil 表示**空环境**而非继承父环境。

## 使用

```sh
go test -race -count=1 ./plugins/local/...
```

装配一套工具：

下面这个片段装配一套本机工具：文件系统、进程提供者、审批渠道与限制策略。

```go fragment
fileSystem, err := local.NewFS(local.FSConfig{World: world, Mode: fs.ModeWorkspaceWrite})
if err != nil {
	return err
}
process, err := local.NewProcess(local.ProcessConfig{World: world})
if err != nil {
	return err
}
gateway, err := approval.NewGateway(responder, approval.PolicyAskOnce)
if err != nil {
	return err
}
set, err := local.NewTools(local.ToolsConfig{
	FS: fileSystem, Process: process, Approvals: gateway,
	Policy: sandbox.Policy{Effective: sandbox.ModeWorkspaceWrite, Available: sandbox.ModeDangerFullAccess},
})
if err != nil {
	return err
}
```

## 理解实现

**顺序就是重点。** `edit_file` 与 `run_command` 先请求审批，**只有** `allowed-once` 才继续；拒绝、取消与无人应答都返回 `ErrPolicyDenied` 且什么都不做。「拒绝审批不写文件」由测试断言，而不是靠注释约定。

**只读策略不做隐式降级。** 命令工具需要至少 `workspace-write`；策略是 `read-only` 时调用被拒绝，而不是"为了跑通"去掉限制。只读文件系统索性不提供编辑工具，模型因此不会被要求在两难里选。

**nil 环境是空环境。** Go 的 `exec.Cmd` 把 nil 解释为**继承父环境**，直接传入会把本进程持有的凭据交给命令。实现显式把 nil 变成空切片；这个真实缺陷由 `TestNilEnvironmentIsEmptyNotInherited` 抓到。

**取消终止进程组。** 用独立进程组 + 组信号，脚本的后代不会活在调用之外；测试以「孙进程写标记文件、取消后标记停止变化」验证。

**路径显示相对而不是绝对。** 工具输出相对工作区的路径，既不泄漏主机布局，又能让模型再次精确指称同一个文件。

## 进一步阅读

- [capability](../../capability/README.md)：本 Provider 实现的五个 seam。
- [能力与执行世界](../../docs/execution/capabilities.md)：策略先于执行的整体规则。
- [平台矩阵](../../docs/execution/platform-matrix.md)：各平台的证据状态。
- [CLI 与会话](../../docs/cli/session-cli.md)：工具在完整轮次里怎样被调用。

## 验证

第一条命令用竞态检测跑本机 Provider 与工具的行为测试；第二条确认本包出现在覆盖率门禁清单里且不低于 90% 语句覆盖率。

```sh
go test -race -count=1 ./plugins/local/...
go run ./scripts/doccheck -coverage coverage.out
```

## 限制

- **不提供真实沙箱**：`Sandboxed` 报告为 false，需要隔离的调用方会被拒绝启动。
- **Windows 树终止路径未验证**：代码存在，但只在本机（macOS）验证过 Unix 路径。
- **不以 shell 执行**：需要 shell 语义的调用方要显式传 `sh -c`，这是刻意的。
- **无远程能力**：远程 Provider 属后续里程碑。
