---
description: "为什么派生缓存不能是权威：删除后重建必须一致，缺少投影必须报错而不是给出一份短历史。"
kind: "concept-note"
---

# 派生缓存：可丢弃的视图，不是第二个真相

## 读完要解决的问题

日志存的是**事件**，而模型需要的是**消息历史**。两者之间隔着一层「投影」：哪些事件会产生模型消息、怎么折叠成一份 `messages`。这层计算每次读都重做很浪费，于是很自然会想缓存起来。

缓存一旦存在，就立刻带来一个问题：**如果缓存和日志不一致，该信谁？**

如果答案是「不确定」，那这个缓存比没有更糟——读者无法判断自己看到的是不是真实历史。所以派生的正确形态是：

1. **可丢弃**：任何时候删掉它，下一次读都能从日志重建出同样的结果。
2. **不自作主张变短**：日志变长了，缓存必须重建，绝不能返回一份比日志更短的历史。
3. **缺投影要报错**：如果某个事件需要解释器而调用方没提供，必须明确失败，而不是静默跳过它。

读完后你能：解释为什么 `Cache` 不是权威；说明「缺少必需投影」为什么必须是错误；对照 `session` 的 surface 规则找到投影的边界。本文讨论的是**已实现**的代码。

需要的 Go 知识：方法集、`sync.Mutex`、切片复制、`errors.Is`。

## 从熟悉的 Go 写法出发

普通的 Go 缓存长这样：一个 map 加一个失效标记，命中就返回，未命中就计算。

```go fragment
type cache struct {
	mu     sync.Mutex
	value  []Message
	valid  bool
}

func (c *cache) Get(events []Event) []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid {
		return c.value
	}
	c.value = fold(events)
	c.valid = true
	return c.value
}
```

这段代码有三个现实问题：

- **只按 valid 判断**会返回**陈旧**结果：日志追加了事件，缓存还标着 valid。
- **返回值是内部切片**：调用方改一下就把缓存污染了。
- **计算失败时没有一致的处置**：`fold` 出错后 `valid` 该是 true 还是 false？

DSH 侧的概念对应是「权威日志 + 派生读模型」：日志是唯一真相，投影是可以随时丢弃重算的读模型。Go 侧落在 `persistence.Cache`：

- **权威（authoritative）**：日志。它只能追加，且序号连续。
- **派生（derived）**：`Cache` 持有的消息历史。
- **投影（projection）**：把 surface 事件折叠成消息的规则，由 `session` 定义。
- **重建（rebuild）**：从日志重新计算派生值。

## 最小实验

准备：仓库根、无需密钥、无需文件系统。

```sh
go test -count=1 -run 'Cache|CheckProjections' -v ./persistence/
```

预期全部 PASS。关键断言：

- `TestCacheRebuildsIdenticallyAfterDiscard`：建缓存 → `Discard()` → 重建，三条路径得到相同内容；`Valid()` 在丢弃后为 false。
- `TestCacheRebuildsWhenTheLogGrows`：日志从 1 条增到 2 条 → 缓存重建为 2 条消息，而不是继续返回 1 条。
- `TestCacheFailureLeavesNoValue`：读不了的事件让构建失败，之后 `Valid()==false`；换成好日志后重建成功。
- `TestCheckProjectionsReportsUnknownSurfaceTypes`：注册表缺 `assistant/message` → 返回 `ErrMissingProjection`，错误里**含具体类型名**；补齐注册表后通过；非 surface 事件不需要解释器。

再跑一次全量确认没有互相污染：

```sh
go test -race -count=1 ./persistence/...
```

## 先预测，再观察

**预测一**：如果把缓存的命中条件从「日志长度相同」改成「只要 valid」，`TestCacheRebuildsWhenTheLogGrows` 会怎样？

观察：结果会失败——返回 1 条而日志有 2 条。原因在 `Cache.Messages` 的命中判断：它同时检查 `valid` 与 `builtFrom == len(events)`。**只看 valid 就返回**是这类缓存最典型的 bug，测试专门为它存在。恢复方式：把条件改回比较日志长度。

**预测二**：`Messages` 返回的是内部切片本身，调用方 `append` 一下会发生什么？

结果：可能污染缓存或读到意外内容。实现里两次返回都做了 `append([]llm.Message(nil), ...)` 副本，所以调用方拿到的是自己的切片。这一条没有单独的测试名，由 `TestCacheRebuildsIdenticallyAfterDiscard` 在多次读取之间比较内容间接覆盖——变更这里时应同时确认该测试仍通过。

**预测三**：日志里有一个 surface 事件，但调用方没给对应的解释器。应该静默跳过它，还是报错？

结果：报错，`TestCheckProjectionsReportsUnknownSurfaceTypes` 断言 `ErrMissingProjection`，并要求错误里含事件类型名。理由与[格式与损坏](format.md)里的版本拒绝同源：**静默地少一条消息，比明确失败更危险**，因为调用方无法区分「历史就是这样」和「我漏了一个解释器」。`CheckProjections` 因此被单独暴露出来，让调用方在折叠**之前**就能报告是哪个类型缺失。

**预测四**：`Restore` 为什么把「恢复状态」和「派生历史」放在同一次调用里返回？

结果：为了避免「拿到空视图继续跑」。如果拆成两步，调用方完全可能只用第一步的返回值。`TestRestoreReturnsStateAndHistory` 断言两者同时返回，且成功后缓存 `Valid()` 为 true。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 会话投影／读模型：权威事件日志与派生的客户端读模型分离；读模型可丢弃并从日志重建；缺少必需投影时明确失败 |
| TypeScript 入口 | 固定提交 `00102833` 的 `packages/session/session-format/src/chain.ts` 与 `types.ts`（格式链与事件形状）；投影与缓存边界见 `packages/session/session-persistence/src/index.ts` 与 `session-checkpoint-policy` |
| Go 入口 | [persistence.Cache](../../persistence/cache.go)（`Messages`/`Discard`/`Valid`）、[persistence.CheckProjections](../../persistence/cache.go)、[persistence.Restore](../../persistence/cache.go)、[persistence.Store](../../persistence/cache.go)；测试见 [cache_test.go](../../persistence/cache_test.go) |
| 保持的行为 | 丢掉缓存后重建结果一致；日志变长则重建；失败不留下可用值；缺投影报错并指出类型；非 surface 事件不需要解释器 |
| Go 表达 | `Cache` 持 `builtFrom` 长度而非时间戳；`sync.Mutex` 串行化读写；返回值复制切片；`ErrMissingProjection` 直接复用 `session.ErrInvalidEvent`，让错误分类与日志侧一致 |
| 不等价边界 | 参考的投影是**插件式**且与实时推送（`agent/assistant-stream` 等）耦合，本阶段只实现「从日志折叠出历史」这一部分，**没有实时投影**；参考的 `eventState`（独占/深冻结）在 Go 里由复制代替，语义更弱但无 GC 压力；本缓存**不跨进程共享** |

## 一次操作的时序与所有权

参与者：调用方、`Cache`、日志。

```text
调用方            Cache                       log
  |----------------|---------------------------|
  |-- Messages(events) ---------------------->|
  |                | valid && builtFrom==len?  |
  |                |   是 → 返回副本            |
  |                |   否 → session.DeriveMessages
  |                |        成功 → 存副本、builtFrom=len、valid=true
  |                |        失败 → valid=false、messages=nil
  |<-- 副本或错误 -|---------------------------|
  |                                           |
  |-- Discard() --->| valid=false、messages=nil |
  |-- Messages(...) -------------------------->| 重新折叠
```

所有权要点：

- **谁持有状态**：`Cache` 持有派生副本；日志在任何时候都是权威。
- **何时发布结果**：只在折叠**成功**后把 `valid` 置真；失败必须置假，否则下一次会返回部分历史。
- **锁的范围**：从判断命中到写回结果全程持锁，因此并发读不会拿到「正在被替换」的切片。
- **谁释放什么**：没有资源需要释放；`Discard` 只丢引用，交由 GC 回收。

## 失败与定位

| 现象 | 原因 | 定位方式 |
| --- | --- | --- |
| 日志变长后历史没变长 | 命中判断只看 valid | `Cache.Messages` 的 `builtFrom == len(events)`；`TestCacheRebuildsWhenTheLogGrows` |
| 折叠失败后仍返回旧历史 | 失败路径没有清空 | `TestCacheFailureLeavesNoValue` 断言 `Valid()==false` |
| 历史莫名少一条消息 | 缺投影却静默跳过 | `CheckProjections` 返回 `ErrMissingProjection` 并含类型名 |
| 并发读拿到半成品 | 返回内部切片或锁范围不足 | `Messages` 全程持锁且返回副本；`go test -race` 覆盖 |
| `Restore` 返回了空历史却没报错 | 存储或折叠错误被吞掉 | `TestRestorePropagatesStorageFailure` 断言错误冒泡 |

## 回到源码与下一步

- 缓存与投影检查：[persistence/cache.go](../../persistence/cache.go)
- 恢复状态：[persistence/resume.go](../../persistence/resume.go)
- surface 与投影规则：[session/README.md](../../session/README.md)
- 契约与单写者：[docs/persistence/go-primer.md](go-primer.md)
