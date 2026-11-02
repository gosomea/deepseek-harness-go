---
description: "CLI 与会话：一次「读文件→受控修改→退出→恢复」走的是哪条路径，以及退出时清理什么。"
kind: "concept-note"
---

# CLI 与会话：一次受控修改与恢复

## 读完要解决的问题

把能力缝好之后，还差一步：**一次真实的端到端使用是什么样子？** 具体要回答：

1. 一次"读文件 → 受控修改 → 退出 → 恢复"里，谁在什么时候检查策略？
2. 退出时"等待拥有的资源"是什么意思？
3. 恢复之后，上一次没结算的工作怎样呈现？
4. 非交互模式下审批政策是什么？

读完后你能：画出这条路径上的参与者与顺序；解释为什么审批检查在**副作用之前**；在 `plugins/local`、`persistence` 与后续 CLI 之间找到每步的落点。

需要的 Go 知识：`context` 取消、`defer` 与清理顺序、`errors.Is`。

## 从熟悉的 Go 写法出发

普通的 CLI 直接干活：

```go fragment
content, err := os.ReadFile(path)
if err != nil {
	return err
}
if err := os.WriteFile(path, changed(content), 0o644); err != nil {
	return err
}
fmt.Println("done")
```

在"人类就坐在终端前、命令自己说了算"的前提下这没问题。它的边界是：

- **没有策略**：写就是写，没有任何人有机会说不。
- **没有身份**：一次同意等于这个进程里所有写入都被同意。
- **没有恢复**：退出之后，做到哪一步只能靠人回忆。
- **没有清理**：起了的子进程、占着的写句柄，退出时无人过问。

DSH 把这条路径拆成三层：

- **能力（capability）**：fs/subprocess 等 seam，路径先经工作区判读。
- **策略（policy）**：审批与沙箱在**副作用之前**表态。
- **会话（session）**：日志是权威记录，恢复时报告"还欠什么工作"而**不重放副作用**。

## 最小实验

准备：仓库根、无需密钥、无需网络（用 fake 模型）。

先用包级测试确认这条路径的每一段：

```sh
go test -race -count=1 ./plugins/local/... ./persistence/... ./agentloop/...
```

预期全部 `ok`。要看的关键断言：

```sh
go test -count=1 -run 'TestEditIsDeniedWithoutApproval|TestShellIsDeniedWithoutApproval' -v ./plugins/local/
```

预期 PASS：**拒绝审批时文件没有被创建、命令没有运行**——这是"副作用之前先过策略"的可观察形式。

再看恢复语义：

```sh
go test -count=1 -run 'TestResumeReportsUnsettledWork|TestResumeDoesNotReExecuteUnsettledTools' -v ./persistence/ ./agentloop/
```

预期 PASS：重开后未结算的调用被**列出**，而工具体**没有**被重新执行。

## 先预测，再观察

**预测一**：一次被拒绝的编辑，会在文件系统上留下任何痕迹吗？

观察：`TestEditIsDeniedWithoutApproval`。结果：**什么都不留**——测试在拒绝后断言文件不存在。理由：如果策略检查发生在写入之后，或者写入是"先落地再撤销"，那么一个更慢或崩溃的进程就会留下痕迹。

**预测二**：命令的非零退出，应该报告为工具错误（模型能看到）还是进程失败？

结果：`plugins/local` 返回 `IsError=true` 的**结果**；而进程**没启动**（程序不存在）才是错误。区别在于模型需要看到"命令跑了但失败了"。

**预测三**：一条命令被取消时，只杀直接子进程够吗？

结果：不够。`TestCancellationKillsTheProcessTree` 用"孙进程写的标记文件在取消后停止变化"证明必须终止**整个进程组**。

**预测四**：恢复之后，工具会被自动重跑吗？

结果：不会。崩溃可能发生在工具体**之前、之中或之后**，"结果没结算"不等于"副作用没发生"。因此恢复只**列出**未结算调用，由调用方决定。

**预测五**：退出时"等待拥有的资源"具体等什么？

结果：等**活动轮次结束**与**写句柄释放**。`Dispose` 等待的是空闲通道而不是自己的完成通道（M5 修掉的真实死锁），`Provider.Close` 在有写者时**拒绝**关闭——丢掉声明会让另一个进程趁虚而入。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | CLI 应用：参数与终端 I/O、退出码、信号、会话恢复与 profile；非交互审批政策确定；退出等待 owned 资源 |
| TypeScript 入口 | 固定提交 `00102833` 的 `apps/cli`；审批渠道语义见 `packages/interaction/user-approval/src/types.ts`；会话恢复语义见 `packages/session/session-persistence/src/handle.ts` |
| Go 入口 | [plugins/local](../../plugins/local/README.md)（`authorize` 与工具）、[persistence](../../persistence/README.md)（`Resume`/`Restore`）、[agentloop](../../agentloop/README.md)（轮次与取消）；`cmd/dsh-go` 承载入口 |
| 保持的行为 | 副作用先过策略；拒绝不留痕迹；取消终止进程树；退出等待释放；恢复只描述不重放；非交互政策确定 |
| Go 表达 | 小接口 + 显式装配；`ErrPolicyDenied` 让拒绝成为可分类事实；`context` 贯穿取消；`Resume` 返回描述而非动作 |
| 不等价边界 | 本阶段 CLI 的**无密钥 E2E 用 fake 模型**；真实端点 smoke **单列且缺密钥记未验证，不记为通过**；账户平台登录、完整原生能力、持久终端与全部 shell Provider 属后续里程碑；Windows 的 shell 与路径规则单独验证，不以 Bash 测试代替 |
| 真实端点实测 | 2026-11-02 用内网 OpenAI 兼容代理跑通 `llmtrpc.Smoke`：五个模型（`deepseek-v4.1-flash-external`、`deepseek-v4-flash`、`deepseek-v4-pro`、`glm-5.3-external`、`kimi-k3.1-pro-external`）各连续两次 `smoke=passed`。凭据仅来自 `DSH_LLM_*` 环境变量，不入库；该代理偶发限流（观测到瞬时 403 与空流，重试即恢复），单次 `failed` 应先重试再判定 |
| 差分实测 | 同日以 `CORDIS_REFERENCE_LIB` + `DSH_PACKAGES` 指向固定参考，跑通 Cordis 与 Session 两族实时差分；Session 侧 6 个场景（`message-projection`、`surface-replacement`、`rejection-cases`、`replay-consistency`、`audit-events-do-not-enter-history`、`empty-message-placeholder`）全部一致 |

## 一次操作的时序与所有权

```text
用户/模型       工具层            审批渠道          文件系统/进程        会话日志
   |              |                  |                   |                 |
   |-- 调用 ----->|                  |                   |                 |
   |              |-- 请求(绑定调用) ->|                   |                 |
   |              |<- 结果 -----------|                   |                 |
   |              |  仅 allowed-once 才继续               |                 |
   |              |-- 执行 ----------------------------->|                 |
   |              |<-- 结果 ------------------------------|                 |
   |<-- 结果 ------|                  |                   |                 |
   |              |-- 记录事件 ------------------------------------------->|
```

所有权要点：

- **谁持有状态**：审批网关持有交换表（一个交换一个答复）；会话日志是权威记录。
- **何时发布结果**：策略结论先于任何副作用；工具结果随后被记入日志。
- **取消传播**：`context` 贯穿；命令取消终止整个进程组；轮次取消保留 inbox。
- **谁释放什么**：工具不持有资源；`Provider.Close` 释放写句柄并在有写者时拒绝；Agent 的 `Dispose` 等待活动轮次结束。

## 失败与定位

| 现象 | 原因 | 定位方式 |
| --- | --- | --- |
| 拒绝审批后文件仍被改 | 策略检查在写入之后 | `editFile` 先 `authorize` 再 `Write`；`TestEditIsDeniedWithoutApproval` |
| 非交互模式静默放行 | 没有确定的 headless 政策 | `approval.Policy` 只有 `deny`/`ask-once`；`TestDenyPolicyNeverGrants` |
| 退出后有子进程残留 | 只杀直接子进程 | 进程组；`TestCancellationKillsTheProcessTree` |
| 退出时挂死 | 等待了自身完成通道 | `Dispose` 等待空闲通道；M5 的 `TestDisposeWaitsAndHonoursDeadline` |
| 恢复后重复执行工具 | 把"未结算"当成"未发生" | `Resume` 只描述；`TestResumeDoesNotReExecuteUnsettledTools` |
| 关闭后另一个进程抢到写权 | 声明被提前丢弃 | `Provider.Close` 在有写者时拒绝 |

## 回到源码与下一步

- [能力与执行世界](../execution/capabilities.md)：这条路径的世界定义。
- [审批与沙箱升级](../execution/approval.md)：策略表态的具体规则。
- [恢复与 checkpoint](../persistence/recovery.md)：退出之后哪些工作会继续。
- [plugins/local](../../plugins/local/README.md)：本机工具的装配与限制。
