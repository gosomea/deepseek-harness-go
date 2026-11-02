---
description: "平台支持矩阵：哪些能力有实测证据、哪些只是代码存在、哪些明确未验证。"
kind: "concept-note"
---

# 平台矩阵：哪些能力验证过，哪些没有

## 读完要解决的问题

跨平台的代码最容易犯的错误不是写错，而是**把"代码存在"当成"行为验证过"**。一个 `_windows.go` 文件能编译，不代表它在 Windows 上终止过进程树。

因此需要的不是一句"支持多平台"，而是一张**逐项说明证据状态**的表：

1. 哪些能力在**本机实测**过？
2. 哪些只有代码、**未在目标平台运行**？
3. 不支持时，程序是拒绝还是降级？

读完后你能：说出每个能力当前的证据等级；解释为什么不支持项必须**阻塞配置**而不是静默降级；在 `capability` 与 `plugins/local` 里找到对应的报告与检查。

## 从熟悉的 Go 写法出发

跨平台代码常用构建标签分文件：

```go fragment
//go:build windows
package local

func killProcessTree(process *os.Process) error {
	return exec.Command("taskkill", "/T", "/F", "/PID", itoa(process.Pid)).Run()
}
```

这解决了**编译**问题，但没解决**证据**问题：这段代码在本机从未执行过。如果文档只说"支持 Windows 进程树终止"，读者会把未验证读成已验证。

DSH 的做法是让能力**可报告**且文档**逐项标注证据**：

- **能力报告（capabilities）**：`subprocess.Capabilities` 说明当前平台能否承诺进程树终止与沙箱。
- **拒绝（refusal）**：需要的能力缺失时 `Supports` 返回 `ErrUnsupported`，调用方**拒绝启动**。
- **证据等级**：本页把"实测""仅代码""未验证"分开写。

## 最小实验

准备：任意平台、仓库根、无需网络。

```sh
go test -race -count=1 ./capability/subprocess/... ./plugins/local/...
```

预期：`ok`。本页的证据结论就来自这些测试**实际跑过什么**。

查看当前平台报告的能力：

```sh
go test -count=1 -run 'TestCapabilitiesAndRefusal|TestSignalledProcessReportsMinusOne' -v ./plugins/local/
```

预期 PASS：平台被报告；进程树终止报告为可用；`Sandboxed` 报告为 false（本 Provider 不实现沙箱）；要求沙箱的调用被拒绝；被信号杀掉的进程报告 `-1`。

## 先预测，再观察

**预测一**：本 Provider 的 `Sandboxed` 是 true 还是 false？

观察：`provider.Capabilities().Sandboxed`。结果：**false**，且 `TestCapabilitiesAndRefusal` 断言要求沙箱的调用返回 `ErrUnsupported`。理由：报告 false 是诚实的；报 true 会让调用方以为自己拿到了从未存在的隔离。

**预测二**：被测进程被信号杀死时，`ExitCode` 是多少——和 `exit 1` 一样吗？

结果：`-1`。`TestSignalledProcessReportsMinusOne` 断言这一点：被信号终止是**另一类事实**，不该伪装成某个退出码。

**预测三**：取消一条 `sh -c "(while true; ...) & sleep 30"` 时，后台孙进程会活下来吗？

结果：不会。`TestCancellationKillsTheProcessTree` 以「标记文件在取消后停止变化」验证——因为取消发送给**整个进程组**，而不是直接子进程。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 平台能力报告与不支持项阻塞：沙箱模式、进程控制、shell/路径规则按平台分别验证 |
| TypeScript 入口 | 固定提交 `00102833` 的 `packages/sandbox/sandbox/src/{index,roots,diagnostics}.ts`、`packages/subprocess/subprocess/src/control.ts`；参考的 Windows 与 shell Provider 在各自包内 |
| Go 入口 | [capability/subprocess](../../capability/subprocess/README.md)（`Capabilities`、`Supports`）、[plugins/local/process_unix.go](../../plugins/local/process_unix.go)、[plugins/local/process_windows.go](../../plugins/local/process_windows.go)、[plugins/local](../../plugins/local/README.md) |
| 保持的行为 | 能力报告而非假定；不支持项拒绝启动；信号终止与退出码区分；取消终止整棵进程树 |
| Go 表达 | 构建标签分平台文件；`runtime.GOOS` 填入 `Platform`；`syscall.SysProcAttr` 设独立进程组；组信号终止 |
| 不等价边界 | **本页的证据只在 macOS 本机取得**；Windows 路径只编译未运行；参考的 `diagnostics.ts` 能力诊断未实现；无真实沙箱 |

### 证据矩阵

| 能力 | Linux | macOS | Windows | 说明 |
| --- | --- | --- | --- | --- |
| 工作区路径规范化与包含判断 | 未在本机验证 | **实测** | 未验证 | 依赖 `filepath`，平台大小写规则未单独验证 |
| 符号链接逃逸检测 | 未在本机验证 | **实测** | 未验证 | 需要符号链接权限 |
| 文件读写的冲突检测与创建语义 | 未在本机验证 | **实测** | 未验证 | — |
| 读取上限 | 未在本机验证 | **实测** | 未验证 | 需要支持稀疏文件才能构造用例 |
| 进程启动与输出捕获 | 未在本机验证 | **实测** | 未验证 | — |
| 输出限额与截断报告 | 未在本机验证 | **实测** | 未验证 | — |
| 超时终止 | 未在本机验证 | **实测** | 未验证 | — |
| 取消（含进程树终止） | 未在本机验证 | **实测** | **仅代码** | Windows 用 `taskkill`，未运行过 |
| 信号终止报告为 -1 | 未在本机验证 | **实测** | 未验证 | Unix 专有语义 |
| 沙箱隔离 | **未实现** | **未实现** | **未实现** | `Sandboxed` 报告为 false，要求者被拒绝 |
| 不以 shell 执行 | 未在本机验证 | **实测** | 未验证 | 参数不分裂由测试断言 |

**读法**：「实测」= 本机测试断言过；「仅代码」= 有实现但从未执行；「未验证」= 未测；「未实现」= 不存在。**未验证不等于不支持**，但也不能读成已支持。

## 失败与定位

| 现象 | 原因 | 定位方式 |
| --- | --- | --- |
| 要求隔离却仍在跑 | 能力未检查 | `Capabilities.Supports`；`TestCapabilitiesAndRefusal` |
| 把未验证读成已支持 | 文档没写证据等级 | 本页矩阵；代码里 `Sandboxed` 默认 false |
| 取消后仍有子进程 | 只杀了直接子进程 | 独立进程组 + 组信号；`TestCancellationKillsTheProcessTree` |
| 信号终止被当成普通退出码 | 未检查 `WaitStatus.Signaled()` | `exitCodeOf`；`TestSignalledProcessReportsMinusOne` |

## 回到源码与下一步

- [能力与执行世界](capabilities.md)：规则全貌。
- [审批与沙箱升级](approval.md)：为什么要求隔离而不能降级。
- [plugins/local](../../plugins/local/README.md)：本机实现与它的限制。
