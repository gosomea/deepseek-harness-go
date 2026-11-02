---
description: "审批与沙箱升级：三种不授权的结果、绑定调用身份的一次性授权，以及只能更宽的升级阶梯。"
kind: "concept-note"
---

# 审批与沙箱升级：谁批准更宽的限制

## 读完要解决的问题

一个模型要在临时工作区里改一个文件。改文件是**副作用**，所以要先问一个人类。这一问立刻带出四个问题：

1. **无人应答时怎么办？** 拒绝、还是"反正没人反对"继续？
2. **等待被取消时怎么办？** 之前已经拿到的"可以"还算数吗？
3. **一次"可以"能管几次？** 只管它针对的那次调用，还是一整个会话？
4. **要更宽的限制怎么办？** 谁能升、能升到哪、什么时候校验？

读完后你能：预测这四种情形的结果；解释为什么"什么都不做"是安全答案；在 `capability/approval` 与 `capability/sandbox` 里找到每条规则的实现与对应测试。

需要的 Go 知识：接口、`context` 取消、`sync.Mutex`、`errors.Is`。

## 从熟悉的 Go 写法出发

普通的 Go 做法是"问一下，拿到布尔值就继续"：

```go fragment
allowed, err := askUser(ctx, "write notes.txt?")
if err != nil {
	return err
}
if allowed {
	return os.WriteFile(path, content, 0o644)
}
```

这段代码有两个隐患：

- **错误的语义被混在一起**：超时、取消、无人应答都走 `err` 分支，而"用户说不"走 `allowed=false`。调用方很难在报告里区分它们。
- **授权没有绑定**：一旦 `allowed` 为真，它对这个进程里任何一次写入都成立。

DSH 的概念对应是「一次性结果 + 绑定调用身份」：

- **结果（outcome）**：`allowed-once`、`rejected`、`cancelled`、`unavailable`，只有第一种授权。
- **绑定（binding）**：请求必须带调用身份，一次授权只对它针对的那次调用有效。
- **headless 政策（policy）**：`deny` 或 `ask-once`，**没有 allow**。

## 最小实验

准备：仓库根、无需密钥、无需网络。

```sh
go test -race -count=1 ./capability/approval/... ./capability/sandbox/...
```

预期：两个包都 `ok`。

最小的成功场景是"显式授权才放行"：

```sh
go test -count=1 -run 'GatewayGrantsOnlyAnExplicitAnswer|GatewayWithoutAResponderDenies' -v ./capability/approval/
```

预期两个测试 PASS：有应答者且回答 `allowed-once` 时授权；没有应答者时 `unavailable`，**不授权**。

再增加一个因素：三种不授权结果。

```sh
go test -count=1 -run 'Cancelled|LateAnswer|Expired' -v ./capability/approval/
```

预期 PASS：取消的等待即使有愿意批准的应答者也不授权；迟到的批准不能改写已经据以行动的拒绝；过期请求直接记为取消且**不打扰应答者**。

## 先预测，再观察

**预测一**：一个愿意批准的应答者存在，但调用方在等待期间取消了。结果是授权、拒绝，还是取消？

观察：`TestGatewayCancelledWaitDenies`。结果：**取消**，不授权。理由是那次授权将批准一个"已经没人在等"的动作。

**预测二**：第一次询问被拒绝，调用方已经据此放弃；之后人类回复"其实可以"。这个迟到的答复会改写记录吗？

结果：不会。`Gateway.record` 只写一次，`TestLateAnswerCannotUpgradeADenial` 断言存储的决定仍是拒绝。理由：如果迟到答复能改写，那么"先按拒绝行动"就成了一个可被追溯推翻的状态。

**预测三**：同一个交换被呈现两次，会拿到两个答复吗？

结果：不会，且**应答者只被问一次**。否则调用方只要反复询问就能拿到一个"是"。

**预测四**：沙箱从 `workspace-write` 请求升到 `workspace-write`（同模式）会怎样？

结果：`sandbox.ValidateEscalation` 返回 `ErrNotWider`——**拒绝而不是忽略**。请求一次并不需要的升级通常说明调用方误读了有效模式，静默接受会把这次误读藏起来。

**预测五**：`read-only` 会话运行一条命令，工具应该自动升到 `workspace-write` 吗？

结果：不。`plugins/local` 的 `runCommand` 直接返回 `ErrPolicyDenied` 并指出需要 `workspace-write`：**不能为了跑通而隐式降级隔离**。只读文件系统索性不提供编辑工具，模型因此不会被要求在两难里选。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 用户审批渠道与权限预设：请求/一次性结果、无应答不放行、沙箱升级在执行时校验且只能更宽 |
| TypeScript 入口 | 固定提交 `00102833` 的 `packages/interaction/user-approval/src/types.ts`（`ApprovalOutcome`）、`packages/interaction/permission-presets`、`packages/sandbox/sandbox/src/escalation.ts`（`WIDER_MODES`、`ESCALATION_TARGETS`、`approveEscalation`） |
| Go 入口 | [capability/approval](../../capability/approval/README.md)（`Outcome`、`Request`、`Gateway`、`Policy`、`Resolve`）、[capability/sandbox](../../capability/sandbox/README.md)（`Mode`、`ValidateEscalation`、`Policy`）；工具侧见 [plugins/local](../../plugins/local/README.md) 的 `authorize` |
| 保持的行为 | 只有 `allowed-once` 授权；`unavailable`/`rejected`/`cancelled` 都不授权；请求绑定调用身份；升级只能严格更宽；所需限制不可强制时拒绝启动 |
| Go 表达 | `Outcome` 带 `Granted()` 作为唯一判定点；`Gateway` 用互斥锁保证一个交换一个答复；`Policy.Validate` 把"宽于可用"变成构造错误 |
| 不等价边界 | 参考的审批是**可插拔渠道 + 事件流**（`ApprovalRequestEvent`），Go 侧只有同步请求/结果；权限**预设**未实现；`sandbox_permissions` 的工具参数配对校验未复刻；真实沙箱未实现，`Sandboxed` 只是报告字段 |

参考实现里有一条值得记住的注释：升级阶梯的校验发生在**执行时**，而不是 tool schema 里——因为 schema 是 registry-global，而有效模式属于单次调用。Go 侧照此实现。

## 失败与定位

| 现象 | 原因 | 定位方式 |
| --- | --- | --- |
| 无人应答时动作仍执行 | 只检查了错误而没检查结果 | `decision.Granted()` 是唯一判定点；`TestGatewayWithoutAResponderDenies` |
| 一次授权被用于另一次调用 | 请求未绑定调用身份 | `Request.Validate` 要求 `CallID`；`TestSideEffectRequiresACallIdentity` |
| 反复询问最终拿到授权 | 同一交换被记录了两次 | `Gateway.Request` 复用已记录的答复；`TestGatewayRefusesASecondPresentation` |
| 迟到答复改写结果 | 记录被覆盖 | `record` 只写一次；`TestLateAnswerCannotUpgradeADenial` |
| 只读会话仍能跑命令 | 隐式降级隔离 | `runCommand` 检查有效模式；`TestShellRefusesUnderAReadOnlyPolicy` |
| 平台无隔离却启动了 | 能力未校验 | `Policy.Validate` / `Capabilities.Supports` |

## 回到源码与下一步

- [能力与执行世界](capabilities.md)：读这条规则的全貌。
- [平台矩阵](platform-matrix.md)：哪些平台证据已验、哪些未验。
- [capability/approval](../../capability/approval/README.md)、[capability/sandbox](../../capability/sandbox/README.md)：接口与错误。
