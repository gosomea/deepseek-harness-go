---
description: "从 Go 视角理解持久化的 seam、句柄与单写者：为什么接口要窄、句柄要单所有权、第二条写者必须被拒绝。"
kind: "concept-note"
---

# C10 持久化入门：seam、句柄与所有权

## 读完要解决的问题

M5 结束时，一次完整轮次已经能跑通，但那份日志只在内存里。进程一退出，历史、排队的输入、还没结算的工具调用全部消失。要把它落盘，第一反应是「找个文件写进去就行」，但真正要先回答的是三个问题：

1. **谁来写？** 如果两个进程同时往同一个会话追加，日志还能是一份有序历史吗？
2. **写到哪里算数？** 「已经写进缓冲区」「已经可见」「已经落盘」是三件不同的事，调用方需要知道 `Append` 返回意味着哪一种。
3. **重开之后，哪些工作该继续？** 崩溃可能发生在工具体之前、之中或之后，「结果没结算」并不等于「副作用没发生」。

读完后你能：解释 `Service`/`Handle` 为什么是两层；说明单写者为什么必须在打开时拒绝而不是在写入时报错；在 `persistence/` 与 `plugins/sessionjsonl/` 里找到每条规则的实现与对应测试。

需要的 Go 知识：接口与方法集、`context` 取消、`sync.Mutex`、`errors.Is`/`errors.Join`、`os.File` 与 `bufio.Writer` 的区别。本文讨论的是**已实现**的代码，涉及参考实现的对照会明确标注。

## 从熟悉的 Go 写法出发

普通 Go 服务里，持久化通常长这样：一个 `Store` 结构体持有 `*sql.DB` 或文件路径，方法直接读写，调用方拿到的就是一个可以随便用的对象。

```go fragment
type Store struct{ db *sql.DB }

func (s *Store) Append(ctx context.Context, event Event) error {
	_, err := s.db.ExecContext(ctx, "insert ...", event)
	return err
}
```

这在单写者、单进程、一次打开的场景下没问题。它的边界有三处：

- **权限过宽**：拿到 `Store` 的人也能删除、迁移、改写历史，哪怕他只需要追加。
- **所有权不清**：两个 goroutine 同时持有同一个对象「打开」了会话，谁负责关？
- **承诺含糊**：`Append` 返回 nil 之后，数据到底在哪里？

DSH 把这三个问题拆成三层来解决，Go 侧对应如下（术语第一次出现时给含义）：

- **seam（接缝）**：可替换的边界接口。`persistence.Service` 是服务级 seam——创建、打开、统计、列举；`persistence.Handle` 是会话级 seam——读取、追加、落盘、关闭。
- **句柄（handle）**：**单所有权**的一次打开。它不是共享服务，而是一份有生命周期的资源。
- **单写者（single writer）**：同一个会话在任一时刻只有一个 `write` 句柄，其余写请求被拒绝。

类比「`*sql.DB` 换成接口」并不等价：接口只是换了调用形状，而这里的重点是**权限与所有权本身被写进了契约**——`read` 句柄在类型上就没有取得写权限的路径。

## 最小实验

准备：仓库根目录、无需密钥、无需网络。观察重点是「第二个写者在哪里被拒绝」。

```sh
go test -race -count=1 ./persistence/... ./plugins/sessionjsonl/...
```

预期：两个包都 `ok`，退出码 0。

最小的成功场景是一个会话被写入、落盘、由新进程重开：

```sh
go test -count=1 -run TestAppendFlushAndReopen -v ./plugins/sessionjsonl/
```

预期看到 `--- PASS: TestAppendFlushAndReopen`。这个测试做三件事：写入两条记录、`Flush`、关掉句柄；用**新的 Provider 实例**重开并读回两条记录。用新实例是刻意的——同一个实例缓存着自己的视图，证明不了字节真的落盘了。

再增加一个因素：第二个写者。

```sh
go test -count=1 -run TestSecondWriterIsRefused -v ./plugins/sessionjsonl/
```

预期 `--- PASS`：第二次 `Open(AccessWrite)` 返回 `persistence.ErrWriterExists`；同时**只读**句柄可以正常工作。最后关掉第一个写者，句柄被释放，第三次写打开成功。

跨进程的那一半单独测：`TestWriterLeaseSurvivesAcrossProviders` 建两个 Provider 指向同一目录，第二个 Provider 的写打开同样被拒绝——因为拒绝不只靠进程内的 map，还靠磁盘上独占创建的 lease 文件。

## 先预测，再观察

**先写下你的预测**：如果把 `plugins/sessionjsonl` 里写打开路径的进程内登记去掉，只保留 lease，`TestSecondWriterIsRefused` 会怎样失败？

观察方法：

```sh
go test -count=1 -run 'SecondWriter|WriterLease' -v ./plugins/sessionjsonl/
```

结果与解释：同**一个** Provider 内的第二次写打开将不再被拒绝，因为两次打开拿到的是两个句柄，而 lease 文件是同一条路径、第二次创建的 `O_EXCL` 也会失败——所以跨进程仍然挡住，同进程却被漏掉；反之只留登记，第二个**进程**就会漏掉。两个拒绝点缺一不可，顺序也有讲究：先登记、后 lease，任一步失败都要回滚前一步，否则一次失败的打开会把会话永久锁死。这一点由 `TestFailedOpenReleasesTheWriterClaim` 与 `TestExistingLeaseRefusesWriterWithoutSideEffects` 断言，也能从 `plugins/sessionjsonl/storage.go` 的 `Open` 里逐行对照。

第二个预测：`Append` 之后立刻用**另一个**读句柄读，能看到刚写的记录吗？

答案是不能——`Append` 只承诺「进入本句柄的缓冲并对自己可见」。要跨句柄看到，必须 `Flush`。恢复方式很简单：跑 `TestAppendFlushAndReopen`，它先断言写句柄自己读得到，再断言新实例读得到。

第三个预测：把工具体的**副作用**当成「没结算就等于没发生」，恢复时自动重跑一遍，会有什么问题？这一问的答案在[恢复与 checkpoint](recovery.md) 里展开，这里只需记住结论：崩溃点可能在副作用之后，重跑会重复执行。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 会话存储 seam：`SessionPersistence` 服务 + 逐会话 `SessionHandle`；`write` 是唯一变更者，`read` 只观察且不取得所有权；关闭后每个操作都失败 |
| TypeScript 入口 | 固定提交 `00102833` 的 `packages/session/session-persistence/src/handle.ts`（`SessionAccess`、`SessionHandle`）与 `index.ts`；参考测试位于同包 |
| Go 入口 | [persistence.Service](../../persistence/README.md)、[persistence.Handle](../../persistence/README.md)、[pkg.go.dev 之外的源码](../../persistence/handle.go)；测试见 [handle_test.go](../../persistence/handle_test.go) 的 `TestHandleBaseRules`、`TestBaseServiceSingleWriter` |
| 保持的行为 | 打开授予 `read`/`write`；关闭幂等；关闭后操作报错；同一会话只有一个写者；`read` 可在 `write` 持有期间工作 |
| Go 表达 | 窄接口 + `HandleBase` 内嵌类型承载共享规则；`BaseService` 持 `writers` map；`errors.Join` 汇总关闭期错误；`atomic.Bool` 表示关闭标记 |
| 不等价边界 | 参考用 `AbortSignal` 与 `Symbol.asyncDispose`，Go 用 `context.Context` 与显式 `Close`；参考的 `eventState`（深冻结/独占）在 Go 里没有对应物；**本包不读 DSH 会话文件**，格式互读属后续里程碑 |

参考源码中有一条容易忽略的注释：读取「从不回溯到本句柄已观察位置之下」。本包把它写进 `Handle` 的接口注释并在 `TestHandleCachesItsOwnView` 中断言，因为只在注释里成立的一致性规则无法防止回归。

## 一次操作的时序与所有权

一次写打开与追加的参与者有四个：调用方、`Provider`、`BaseService`、以及磁盘上的 artifact 与 lease。

```text
调用方            Provider                BaseService          disk
  |                  |                        |                 |
  |-- Open(write) -->|                        |                 |
  |                  |-- 读 header 校验格式 -->|                 |-- 读 header
  |                  |-- ClaimWriter(id) ----->| 登记 writers[id] |
  |                  |<-- ok ------------------|                 |
  |                  |-- claimLease -----------|---------------->| O_EXCL 建 lease
  |                  |<-- lease 路径 ----------------------------| ok
  |                  |-- open(O_APPEND) ------>|                 |-- 打开追加句柄
  |<-- Handle --------|                        |                 |
  |                  |                        |                 |
  |-- Append(...) --->| 缓冲记录（bufio）      |                 |
  |<-- Event(seq) ----|                        |                 |
  |-- Flush() ------->| bufio.Flush + Sync ----|---------------->| 落盘
  |<-- nil -----------|                        |                 |
  |-- Close() ------->| 释放 lease + ReleaseWriter ------------->| 删除 lease
```

所有权要点：

- **谁持有状态**：`Handle` 持有缓冲与自己的读取视图；`BaseService` 持有 `writers`；磁盘持有 lease 文件与字节。
- **何时发布结果**：`Append` 返回后记录对**本**句柄可见；`Flush` 返回后才对**其他**读者可见并承诺落盘。
- **取消传播到哪里**：`context` 在真正动手之前被检查，因此取消不会留下半个记录；已进入缓冲的记录由 `Flush`/`Close` 决定命运。
- **谁释放什么**：`Handle.Close` 释放缓冲、追加句柄与 writer 声明；`Provider.Close` 在有写者时**拒绝**关闭，避免声明被丢掉而另一个进程趁虚而入。

## 失败与定位

| 现象 | 原因 | 定位方式 |
| --- | --- | --- |
| 第二次写打开成功 | 只留了 lease 或只留了登记 | 对照 `Provider.Open` 中登记与 `claimLease` 的顺序；跑 `TestSecondWriterIsRefused` 与 `TestWriterLeaseSurvivesAcrossProviders` |
| 一次失败的打开锁死了会话 | 失败后没有回滚前面的声明/lease | 看 `Open` 中每个错误分支是否调用了 `ReleaseWriter` 或 `releaseLease`；`TestFailedOpenReleasesTheWriterClaim` 断言声明未被卡住 |
| `Append` 报成功但数据丢失 | 把 `Append` 的承诺当成落盘 | `Append` 走 `bufio.Writer`；落盘的唯一承诺点是 `Flush`，见 `TestFlushReportsUnwritableDescriptor` |
| 关闭后仍能操作 | 句柄没走 `CheckOpen` | 每个操作都应先 `CheckOpen`；`TestReadOnlyAndClosedRules` 覆盖只读与关闭两种拒绝 |
| 重开后历史变短 | 读到了损坏的日志却没报错 | `Stat` 的 `RecoverableTail` 与 `Inspect` 的 `Reason`；见[格式与损坏](format.md) |

## 回到源码与下一步

- 契约与句柄：[persistence/handle.go](../../persistence/handle.go)、[persistence/service.go](../../persistence/service.go)
- 第一个后端：[plugins/sessionjsonl/README.md](../../plugins/sessionjsonl/README.md)
- 规范化的日志本身：[session/README.md](../../session/README.md)
- 下一步读：[格式与损坏](format.md)（记录边界与恢复），再读[恢复与 checkpoint](recovery.md)（重开后哪些工作会继续）
