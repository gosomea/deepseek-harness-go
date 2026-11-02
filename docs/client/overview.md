# 客户端与投影

## 读完要解决的问题

断线后的 UI 从哪里恢复，实时 delta 是真源吗？读完后你能：区分 Host 权威状态、客户端投影与实时 delta；用快照+增量重建客户端视图；解释断线恢复为何按序列续接。需要 Go 的切片与 `context` 基础。当前实现是 `plugins/clientview`、`plugins/hostapi`（controller）、`plugins/desktophost`。

## 从熟悉的 Go 写法出发

普通 Go 客户端常把「收到的消息」直接追加进自己的列表。这在单连接、无掉线时可行，但一旦掉线重连，客户端无法回答「我缺了哪几条、有没重复」。DSH 把客户端视图定义为 host 日志的**投影**：以序列对齐，快照是前缀、delta 是后缀，重连按序列续接。类比在「追加消息」上成立，在「无缺失无重复」上不等价。

## 最小实验

在仓库根目录：

```sh
go run ./examples/client
```

输出逐字保存在 [examples/client/expected.txt](../../examples/client/expected.txt)：

```text output-file=examples/client/expected.txt
resumed through: 3
resynced: false
composed equals full: true
resync fallback: true through 5
```

观察重点：快照+delta 得到的视图与直接全快照相同；客户端落在 hub 保留范围之外时回退快照（resync=true）。

## 先预测，再观察

先预测：客户端 last-seen 为 1，而 hub 只剩从 5 开始的条目，`Resume` 会怎样？写出预期再对照实现——`Since(1)` 返回 `ErrGap`，于是 `Resume` 回退到 `Latest()` 全快照并报 `resynced=true`，客户端不会看到不完整视图。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | Host 权威状态、客户端投影、快照/增量、断线恢复 |
| TypeScript 入口 | 固定提交 `packages/client`、`packages/host` |
| Go 入口 | [plugins/clientview](../../plugins/clientview/README.md)、[plugins/hostapi](../../plugins/hostapi/README.md)、[plugins/desktophost](../../plugins/desktophost/README.md) |
| 保持的行为 | UI 不产生第二份权威日志；快照+delta 衔接无缺失/重复；慢消费者丢事件 |
| Go 表达 | 纯投影 + 序列对齐 + Hub 重连回退 |
| 不等价边界 | Go Host 侧已验证；真实 React UI/浏览器 E2E/Desktop 打包属缺口 |

## 一次操作的时序与所有权

host 持有权威日志 → `clientview` 生成 Snapshot/Delta → 客户端 `ApplySnapshot`+`ApplyDelta` 得视图；掉线重连 `Hub.Resume` 按序列续接或回退快照。host 拥有状态，客户端只持投影。

## 失败与定位

- **`ErrGap`**：客户端落后到 hub 保留范围之外，需全快照。
- **delta 起点不符**：拒绝应用，避免重复/跳条。
- **会话越权**：`session.read` 由 controller 按 principal 拒绝。

## 回到源码与下一步

先读 [plugins/clientview](../../plugins/clientview/README.md)，再看 [前端缺口](frontend-gap.md) 了解未完成项。
