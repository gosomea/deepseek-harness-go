---
description: "用共享场景与归一化 trace 对照 Go session surface 与固定 TypeScript 参考，并登记两侧的行为差异。"
kind: "package-library"
---

# Session surface 差分场景

## 概述

这里保存 Go `session` 包与固定 TypeScript 参考共享的 surface 行为场景，以及从参考侧录制、经确定性核对的两侧 trace。“Go 测试通过”只说明实现没有偏离自己；要证明**行为等价**（同一份日志得到同一份模型历史），两侧必须吃同一份输入。每个场景是一份 JSON，由两个回放器分别执行：Go 侧的 [`internal/testkit.ReplaySessionScenario`](../../../internal/testkit/session_scenario.go) 与本目录的 [runner-ts.mjs](runner-ts.mjs)。

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
- 保存从固定参考提交录制、经确定性核对的参考侧 trace。
- 登记**已声明**的行为差异及其理由；不负责改写 trace 让两侧看起来相同。

## 使用

回放一个场景需要固定提交对应的构建产物：

```sh
node testdata/parity/session/runner-ts.mjs \
  <DSH>/packages \
  testdata/parity/session/scenarios/surface-replacement.json
```

重新录制全部参考 trace：

```sh
for f in testdata/parity/session/scenarios/*.json; do
  n=$(basename "$f" .json)
  node testdata/parity/session/runner-ts.mjs <DSH>/packages "$f" \
    > "testdata/parity/session/expected/$n.ts.txt"
done
```

录制会覆盖已评审的 trace，因此必须与差异清单一起评审，并更新 [provenance.json](provenance.json) 里的哈希。参考 checkout 始终只读。

## 理解实现

### 固定来源

| 项目 | 值 |
| --- | --- |
| 参考仓库 | `deepseek-ai/deepseek-harness` |
| 固定提交 | `00102833dfaee1da9f48a3a8eae9d34005a75218` |
| 参考包 | `packages/core/session` |
| 运行形式 | 该提交的构建产物 `packages/core/session/lib/index.js` |

参考的 `lib/` 是 `.gitignore` 的构建产物。它是**未提交**但存在的；缺失时实时对照检查会被跳过，并记为未验证而不是通过。源码侧的行为依据用 `git show` 从固定提交读取。

### trace 归一化规则

以下内容**不写入** trace，因为它们在两侧实现间不同，却不描述行为：

- 消息 id（参考自己生成，本包保留 payload 里带的那个）
- 当前时钟（只记录场景里显式写下的 `time`）

以下内容**必须**保留：

- fold 是否被拒绝
- surface 节点序列与顺序
- 替换报告：替换者 seq、声明范围、实际被遮蔽节点序列
- 两个 generation 计数
- 派生历史的条数、顺序、role、可见文本与内容块种类

### 场景格式

```text
{
  "id": "scenario-id",
  "title": "这个场景钉住的一条可观察行为",
  "events": [ { "type": "user/message", "seq": 0, "time": 1, "data": {...}, "surfaceOp": "append" } ],
  "expectFoldError": false
}
```

`surfaceOp` 是 `"append"` 或 `{"op":"replace","startSeq":N,"endSeq":M}`。`data` 保持原始 JSON，所以场景不绑定 Go 类型。含 `image/offload` 的场景在两侧都会装上等价的占位解释器，因为该类型在没有解释器时会被拒绝。

### 差异怎样声明

两侧行为确实不同时，用 `divergences` 声明，**不允许**改写 trace 让它们看起来相同。比较器只在当前位置两侧恰好匹配声明块时应用差异，并且要求每条声明都实际发生。当前没有任何已声明差异，见 [DIVERGENCES.md](DIVERGENCES.md)。

## 进一步阅读

- [internal/testkit README](../../../internal/testkit/README.md)：回放器与比较器的使用说明。
- [session 包](../../../session/README.md)：日志、surface 与投影的实现。
- [投影笔记](../../../docs/session/projection.md)：读者视角的机制讲解。
- [测试与门禁](../../../docs/testing.md)：这些检查如何进入仓库验收。

## 验证

```sh
go test -race -count=1 ./internal/testkit
```

要对照**实时**参考而不是已记录的 trace：

```sh
DSH_PACKAGES=<DSH>/packages go test -count=1 -run TestLiveReferenceSessionSurfaceMatchesRecorded ./internal/testkit
```

## 限制

- 场景表达力受格式限制：它描述“一组事件 + 一次 fold”，尚未覆盖增量 append 的中间状态、并发读取与跨会话 fork。
- 逐行比较不是语义等价证明；两侧都未声明的行为差异不会被发现。
- 场景只覆盖 surface 与派生历史。envelope 的字段级拒绝（`ignorable` 的形状、未知字段等）由 Go 单侧测试固定，因为参考的 `foldSurface` 不做 payload 校验——那些规则在它的入日志边界上。这是**已记录的覆盖差异**，不是遗漏。
- 实时对照需要本机存在固定提交的构建产物；缺失时该检查跳过，表示未验证而非通过。
