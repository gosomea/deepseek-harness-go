---
description: "用共享场景驱动 Cordis 与固定 TypeScript 参考的差分对照，让行为等价有可复现证据。"
kind: "package-library"
---

# internal/testkit

## 概述

这个包提供确定性的测试基础设施，当前包含两族差分场景回放器：Cordis 运行时（`cordis/`）与 Session surface（`session/`）。它把一份场景 JSON 分别喂给 Go 实现与固定的 TypeScript 参考，各自产出规范化 trace，再逐行比较。测试因此比较的是**同一输入下的两侧行为**，而不是把 Go 当前输出录成期望值——后者只能证明实现没有变化，不能证明它与参考一致。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- 读取 `testdata/parity/cordis/scenarios` 下的场景，在 Go 运行时中回放并产出 trace；再读取 `testdata/parity/session/scenarios` 下的场景，在 Go `session` 包上折叠 surface 并产出 trace。
- 比较 Go trace 与已记录的参考 trace，只接受**显式声明且确实发生**的差异。
- 不负责两种实现的本身，也不判断行为是否“正确”；它只回答“两侧是否一致”。

## 使用

两族场景共用同一个比较器（`CompareTraces`），所以差异规则只有一套：声明必须实际发生，未声明的差异必须失败。

### Cordis 场景

场景是一份 JSON：`id`、`title`、`steps`。每个 step 是一种可观察动作，例如 `register`、`settle`、`observe`、`setReady`、`notify`、`emit`、`update`。

```go fragment
scenario, err := testkit.LoadScenario("testdata/parity/cordis/scenarios/dependency-pending-recovery.json")
if err != nil {
	return err
}
got, err := testkit.Replay(scenario)          // Go 侧 trace
reference, err := os.ReadFile("testdata/parity/cordis/expected/dependency-pending-recovery.ts.txt")
if err := testkit.CompareTraces(scenario, string(reference), got); err != nil {
	return err                                  // 未声明差异或声明失效
}
```

记录参考 trace 需要固定的 Cordis 构建产物。参考树本身只读，构建产物来自固定提交：

```sh
node testdata/parity/cordis/runner-ts.mjs \
  <DSH>/vendor/cordis/lib/index.js \
  testdata/parity/cordis/scenarios/<scenario>.json > testdata/parity/cordis/expected/<scenario>.ts.txt
```

失败有两种，且都应当失败：出现**未声明**的差异，或声明的差异**没有实际发生**。后一种保证差异清单不会退化成永久的白名单。

### Session surface 场景

场景是一份 JSON：`id`、`title`、`events`，可选 `expectFoldError`。每个事件是 `type`、`seq`、`time`、`data` 与可选的 `surfaceOp`／`sourceEventSeqs`。

```go fragment
scenario, err := testkit.LoadSessionScenario("testdata/parity/session/scenarios/surface-replacement.json")
if err != nil {
	return err
}
got, err := testkit.ReplaySessionScenario(scenario)     // Go 侧 trace
reference, err := os.ReadFile("testdata/parity/session/expected/surface-replacement.ts.txt")
if err := testkit.CompareSessionTraces(scenario, string(reference), got); err != nil {
	return err
}
```

录制参考 trace：

```sh
node testdata/parity/session/runner-ts.mjs <DSH>/packages \
  testdata/parity/session/scenarios/<scenario>.json > testdata/parity/session/expected/<scenario>.ts.txt
```

含 `image/offload` 的场景在两侧都会装上等价的占位解释器，因为该类型在没有解释器时会被拒绝。trace **不记录消息 id**：参考自己生成它们，本包保留 payload 里带的那个，比较它们等于比较生成器。

## 理解实现

`Replay` 顺序执行 step，把每个可观察事件写成一行；`observe` 输出各节点状态。trace 刻意不包含 fiber ID、时间戳和独立节点的注册顺序——这些在两侧实现间不同，却不描述行为，归一化掉它们才能让真正的差异暴露。

两侧运行的边界不同：参考实现用微任务推进生命周期，Go 在 `Plugin`/`Dispose` 内同步 reconcile。回放器用 `Wait` 等待结算，而不是 sleep，因此结果不依赖调度时序。

`ReplaySessionScenario` 把场景事件折成一条 **面**（记住：Go 的 `session` 里叫 surface）：它记录 fold 是否被拒绝、节点序列、替换报告、两个 generation 计数，以及派生历史的 role、可见文本与内容块种类。后两项都要记录——只看文本会漏掉“文本相同但块种类变了”这类真实差异。

差异机制见两个目录各自的 README，其中每条差异都带有理由；`CompareTraces` 的实现见 [compare.go](compare.go)，场景解码见 [session_scenario.go](session_scenario.go)。

## 进一步阅读

- [差分场景与归一化规则](../../testdata/parity/cordis/README.md)：场景格式、trace 规则与差异清单。
- [Cordis 源码对应表](../../docs/cordis/source-map.md)：每个行为在两侧的入口。
- [测试与门禁](../../docs/testing.md)：这个包如何进入仓库检查。

## 验证

```sh
go test -race -count=1 ./internal/testkit
```

要对照**实时**参考（而不是已记录的 trace）运行，分别设置固定构建产物的路径：

```sh
CORDIS_REFERENCE_LIB=<DSH>/vendor/cordis/lib/index.js go test -count=1 -run TestLiveReferenceTrace ./internal/testkit
DSH_PACKAGES=<DSH>/packages go test -count=1 -run TestLiveReferenceSessionSurfaceMatchesRecorded ./internal/testkit
```

两个检查都在缺少 Node 或缺少对应路径时跳过；跳过表示未验证，不代表通过。

## 限制

- Cordis 场景表达力限于回放器实现的 op；尚未覆盖并发取消、竞态与事件过滤组合。
- Session 场景描述“一组事件 + 一次 fold”，尚未覆盖增量 append 的中间状态、并发读取与跨会话 fork。
- **覆盖范围不对称**：Session 场景只对照 surface 与派生历史。envelope 的字段级拒绝（`ignorable` 的形状、未知 envelope 字段、必填字段）由 Go 单侧测试固定，因为参考的 `foldSurface` 不做 payload 校验——那些规则在它的入日志边界上。这是已记录的差异，不是遗漏。
- 实时对照依赖本机存在固定提交的构建产物。默认测试使用已记录 trace，录制来源与核对方法写在两个目录各自的 `provenance.json`。
- 比较是逐行文本比较，不是语义等价证明：两侧都未声明的行为差异不会被发现。
