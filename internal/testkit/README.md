---
description: "用共享场景驱动 Cordis 与固定 TypeScript 参考的差分对照，让行为等价有可复现证据。"
kind: "package-library"
---

# internal/testkit

## 概述

这个包提供确定性的测试基础设施，当前包含 Cordis 差分场景回放器。它把一份场景 JSON 分别喂给 Go 运行时与固定的 TypeScript 参考，各自产出规范化 trace，再逐行比较。测试因此比较的是**同一输入下的两侧行为**，而不是把 Go 当前输出录成期望值——后者只能证明实现没有变化，不能证明它与参考一致。

## 目录

- [职责](#职责)
- [使用](#使用)
- [理解实现](#理解实现)
- [进一步阅读](#进一步阅读)
- [验证](#验证)
- [限制](#限制)

## 职责

- 读取 `testdata/parity/cordis/scenarios` 下的场景，在 Go 运行时中回放并产出 trace。
- 比较 Go trace 与已记录的参考 trace，只接受**显式声明且确实发生**的差异。
- 不负责 Cordis 本身的实现，也不判断行为是否“正确”；它只回答“两侧是否一致”。

## 使用

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

## 理解实现

`Replay` 顺序执行 step，把每个可观察事件写成一行；`observe` 输出各节点状态。trace 刻意不包含 fiber ID、时间戳和独立节点的注册顺序——这些在两侧实现间不同，却不描述行为，归一化掉它们才能让真正的差异暴露。

两侧运行的边界不同：参考实现用微任务推进生命周期，Go 在 `Plugin`/`Dispose` 内同步 reconcile。回放器用 `Wait` 等待结算，而不是 sleep，因此结果不依赖调度时序。

差异机制见 [testdata/parity/cordis/README.md](../../testdata/parity/cordis/README.md)，其中每条差异都带有理由；`CompareTraces` 的实现见 [compare.go](compare.go)。

## 进一步阅读

- [差分场景与归一化规则](../../testdata/parity/cordis/README.md)：场景格式、trace 规则与差异清单。
- [Cordis 源码对应表](../../docs/cordis/source-map.md)：每个行为在两侧的入口。
- [测试与门禁](../../docs/testing.md)：这个包如何进入仓库检查。

## 验证

```sh
go test -race -count=1 ./internal/testkit
```

要对照**实时**参考（而不是已记录的 trace）运行，设置固定提交的构建产物路径：

```sh
CORDIS_REFERENCE_LIB=<DSH>/vendor/cordis/lib/index.js go test -count=1 -run TestLiveReferenceTrace ./internal/testkit
```

该检查在没有 Node 或没有该路径时跳过；跳过表示未验证，不代表通过。

## 限制

- 场景表达力限于回放器实现的 op；尚未覆盖并发取消、竞态与事件过滤组合。
- 实时对照依赖本机存在固定提交的 Cordis 构建产物。默认测试使用已记录 trace，录制来源与核对方法写在 `testdata/parity/cordis/provenance.json`。
- 比较是逐行文本比较，不是语义等价证明：两侧都未声明的行为差异不会被发现。
