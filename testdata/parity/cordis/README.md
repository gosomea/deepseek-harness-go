---
description: "用共享场景与归一化 trace 对照 Go 运行时与固定 TypeScript 参考，并记录已声明的差异。"
kind: "package-library"
---

# Cordis 差分场景

## 概述

这里保存 Go 运行时与固定 TypeScript 参考共享的行为场景，以及从参考侧录制、经核对的两侧 trace。“Go 测试通过”只说明实现没有偏离自己；要证明**行为等价**，两侧必须吃同一份输入。每个场景是一份 JSON，由两个回放器分别执行：Go 侧的 [`internal/testkit.Replay`](../../../internal/testkit/parity.go) 与本目录的 [runner-ts.mjs](runner-ts.mjs)，产出同一种逐行 trace 后比较。

目录本身不是 Go 包，内容由 [internal/testkit](../../../internal/testkit/README.md) 消费。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- 保存两侧共享的场景定义，保证输入一致。
- 保存从固定参考提交录制、经确定性核对的两侧 trace。
- 登记**已声明**的行为差异及其理由；不负责改写 trace 让两侧看起来相同。

## 使用

回放一个场景需要固定提交对应的构建产物：

```sh
node testdata/parity/cordis/runner-ts.mjs \
  <DSH>/vendor/cordis/lib/index.js \
  testdata/parity/cordis/scenarios/config-update.json
```

重新录制全部参考 trace：

```sh
for f in testdata/parity/cordis/scenarios/*.json; do
  n=$(basename "$f" .json)
  node testdata/parity/cordis/runner-ts.mjs \
    <DSH>/vendor/cordis/lib/index.js "$f" \
    > "testdata/parity/cordis/expected/$n.ts.txt"
done
```

录制会覆盖已评审的 trace，因此必须与差异清单一起评审；核对信息写在 [provenance.json](provenance.json)。

## 理解实现

### 固定来源

| 项目 | 值 |
| --- | --- |
| 参考仓库 | `deepseek-ai/deepseek-harness` |
| 固定提交 | `00102833dfaee1da9f48a3a8eae9d34005a75218` |
| 参考包 | `vendor/cordis` `@deepseek-ai/cordis` 4.0.4 |
| 运行形式 | 该提交的构建产物 `vendor/cordis/lib/index.js` |

参考树始终只读。录制只读取固定提交对应的构建产物，本仓库不写入参考 checkout。

### trace 归一化规则

以下内容**不写入** trace，因为它们在两侧实现间不同，却不描述行为：

- fiber ID 与节点内部注册顺序
- 墙钟时间、超时与调度次序
- 错误文本、堆栈与包装层级（用固定标签代替，例如 `close error=cleanup-failed`）

以下内容**必须**保留，不能被归一化抹掉：

- 生命周期状态转换（`pending`、`loading`、`active`、`failed`、`unloading`、`disposed`）
- Apply 与 cleanup 的发生次数和相对顺序
- 事件监听器的调用顺序与 bail 结果
- 成败结论（例如配置更新被拒绝时打 `update <node>=rejected`）

### 场景格式

```text
{
  "id": "scenario-id",
  "title": "这个场景钉住的一条可观察行为",
  "steps": [ { "op": "register", "as": "p", "name": "p", "provides": ["a"] }, ... ],
  "divergences": [ { "go": ["..."], "reference": ["..."], "reason": "为什么不同且可以接受" } ]
}
```

支持的 op：`register`、`settle`、`observe`、`setReady`、`notify`、`dispose`、`close`、`emit`、`serial`、`bail`、`waterfall`、`update`。

### 差异怎样声明

两侧行为确实不同时，用 `divergences` 声明，**不允许**改写 trace 让它们看起来相同。比较器只在当前位置两侧恰好匹配声明块时应用差异，并且要求每条声明都实际发生：

- 声明了却没出现 → 测试失败，防止差异清单腐化成白名单
- 缺少 `reason` → 测试失败，没有理由就不是评审过的证据
- 未声明的差异 → 测试失败

当前已声明的差异见 [DIVERGENCES.md](DIVERGENCES.md)。

## 进一步阅读

- [internal/testkit README](../../../internal/testkit/README.md)：回放器与比较器的使用说明。
- [Cordis 源码对应表](../../../docs/cordis/source-map.md)：每个行为在两侧的入口。
- [测试与门禁](../../../docs/testing.md)：这些检查如何进入仓库验收。

## 验证

```sh
go test -race -count=1 ./internal/testkit
```

要对照**实时**参考而不是已记录的 trace：

```sh
CORDIS_REFERENCE_LIB=<DSH>/vendor/cordis/lib/index.js go test -count=1 -run TestLiveReferenceTrace ./internal/testkit
```

## 限制

- 场景表达力受回放器 op 限制，尚未覆盖并发取消、竞态与事件过滤组合。
- 逐行比较不是语义等价证明；两侧都未声明的行为差异不会被发现。
- 实时对照需要本机存在固定提交的构建产物；缺失时该检查跳过，表示未验证而非通过。
