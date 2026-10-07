# M2 执行契约：Loader、配置身份与最小 Profile

## 现状

M0 与 M1 的执行契约都已通过，M2 的待实施范围由[阶段索引](../dsh-go-replication/stages/index.md)和 [M2 细化方案](../dsh-go-replication/stages/m02-composition.md)定义：五个切片 M2.1–M2.5，退出门禁 G2。本页是 M2 的入口，事实归属仍在细化方案和共同验收页，本页不复制它们的规则。

## 目标

- **复刻**：[[goal:replication]] 用一份 JSON 配置装配出稳定、可更新的已编译插件树，让 Entry 身份、Fiber 绑定、启停与最小 profile 与固定 DSH 参考行为对应。
- **学习**：[[goal:learning]] 让 Go 读者从显式注册表与结构体配置出发，解释 Entry、Loader、profile 与 bundle 的分工，并区分声明树与运行树。
- **证据**：[[goal:evidence]] 每条行为有固定 TS 来源、Go 入口、行为测试与显式差异；规划、缺环境与未验项不进入通过分子。

## 执行契约

[plan.json](plan.json) 是本阶段的执行契约，六个节点串行：

[[node:m21-entry]] → [[node:m22-tree]] → [[node:m23-update]] → [[node:m24-profile]] → [[node:m25-cli]] → [[node:g2-acceptance]]

每个切片同时交付实现与学习产物。每节点需要语义评审，独立性如实记录。

## 产物所有权与不变量

契约遵守三条不变量，它们不是格式偏好，而是协议的执行约束：

1. **inputs 不得被本节点或链上更晚的节点写入。** `outputs` 和 `inputs` 都参与前驱有效性校验：前者决定产物是否被改，后者决定输入是否被换。任一被后继改写，前驱节点立刻变为 stale。
2. **不使用目录级 writes。** 早期草稿用 `loader/` 覆盖整个目录，会让相邻切片互相改写对方的产物。现在每个 `writes` 逐文件列出，且只包含本节点 `outputs`。
3. **检查只断言本节点有权写入、或由已通过前驱产出的文件。** 这条来自一次真实缺陷：`doc-structure` 曾一次性硬编码三份学习文档，而它同时挂在 M2.1 与 M2.3 上——M2.1 只有 `go-primer.md`，其余两份尚未存在，`read_text()` 抛 `FileNotFoundError`，DAG 会在 M2.1 就地卡死。现在每个节点只校验自己产出的文档。

`scripts/doccheck/main.go`、`scripts/doccheck/main_test.go`、`Makefile`、`docs/testing.md` 被多个切片先后修改，因此它们不出现在任何节点的 `inputs`，也不作为非最终节点的 `outputs`；改动的证据由各节点自己的 `coverage-registration` 检查提供，最终状态由 [[node:g2-acceptance]] 冻结为 outputs。

### 对上游产物的已知影响

M2 必然要修改 M1 已通过节点产出过的门禁文件，这会按协议的"产物变化使证据失效"机制生效：

| 被改文件 | 上游归属 | 后果 |
| --- | --- | --- |
| `scripts/doccheck/main.go`、`main_test.go` | M1 `parity-harness`／`m01-handover` outputs | M1 相关节点在协议中转为 stale，需按 M1 的计划重新验收或接受其历史结论 |
| `Makefile`、`docs/testing.md` | M1 outputs | 同上 |
| `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md` | M0 `cordis-audit` 与 M1 outputs | 同上 |

这是 M2 真实修改门禁与新模块时不可避免的代价，已如实记录，不用"局部改动"淡化。`stages/index.md`、`stages/m02-composition.md` 与 M0 契约的其余 `documents` **不改**：它们参与 M0 的 revision 计算，改动会让 M0 全部节点变为 `plan changed; revise required`，影响面更大。M2 的状态因此记录在本阶段主题页。

## M1 遗留缺口的归属裁决

M1 审计的[仍然开放](../dsh-go-replication/cordis-audit.md#仍然开放)表列出 6 个未决缺口，并声明"在 M2 展开时决定归属"。归属如下。

| 缺口 | 固定 TS 依据 | M2 归属 | 本阶段验收方式 |
| --- | --- | --- | --- |
| `internal/plugin` 事件 | `vendor/loader/src/index.ts:129` 设置 `fiber.entry`、处理自卸载，8 个生产文件 | [[node:m22-tree]] | Entry 与 Fiber 的绑定在 Go 中显式表达；绑定关系可查询且与所有者一致 |
| `internal/config` waterfall | `vendor/loader/src/index.ts:104` 对每份配置插值 `!!js` | [[node:m21-entry]] | 配置 codec 在解析阶段处理表达式字段；解析失败不进入应用阶段 |
| `internal/update` waterfall | `vendor/loader/src/index.ts:115`、`index.ts:123` 用 `global`／`prepend` 挂全局更新钩子 | [[node:m23-update]] | 更新钩子的顺序与全局／前置差异有测试；更新不无故改变条目身份 |
| `Context.intercept` 与分层配置合并 | `vendor/loader/src/config/isolate.ts:126`、`vendor/cordis/src/service.ts:87` `resolveConfig` | [[node:m24-profile]] | overlay 顺序与 required 规则有 fixture；来源优先级逐项说明 |
| per-entry isolate realm | `vendor/loader/src/config/isolate.ts:92–152` 直接读写 `ctx[Context.isolate]` 与 `reflect.store` | [[node:m22-tree]] | 条目级服务可见性在 Go 中显式表达；跨条目隔离有测试 |
| `Context.extend` 属性合并 | `vendor/loader/src/config/entry.ts:57`、`config/tree.ts:16` 携带 `Entry.key`／`baseUrl` | [[node:m21-entry]] | 决定 Go 等价物：结构体嵌入、显式字段或不做；无论哪种都记为差异或适配并说明理由 |

### 复核为表达差异的项

| 缺口 | 裁决 |
| --- | --- |
| 生成器／异步生成器 Effect | 维持 M1 的重新判定：`OnDispose` 已提供等价的所有权语义，记为**表达差异**而非缺口。[[node:m22-tree]] 在实现 Entry→Fiber 绑定时复核该结论仍成立 |
| `internal/service` 通知事件 | 两个生产消费者不足以在 M2 提前补。若 [[node:m23-update]] 的更新语义确实需要通知，则在该切片内实现；否则保留为 M12 收口项并写明理由 |

## 待复核的边界

- **原 profile 兼容**：本阶段只做 JSON 输入形式，patch 替换／合并、默认值、惰性注入与 disabled 的语义逐项说明。YAML include、惰性表达式、配置 HMR 与原 profile 兼容属 M12.2。
- **静态注册与 npm**：本阶段是显式编译期注册，不提供 npm 动态安装、模块 HMR 或原 profile 兼容；该声明必须落在 `loader/README.md` 与 `docs/loader/tutorial.md` 并有检查覆盖，不得表述为当前能力。

## 裁决落点的明确处置

裁决写入**本页**（见上表），不写入 `cordis-audit.md` 与 `stages/m02-composition.md`。这是明确的处置结论，不是悬空：

- `docs/10-plans/dsh-go-replication/cordis-audit.md` 是 M0 的 `cordis-audit`、`baseline-acceptance` 与 M1 的 `m01-handover` 三个**已通过**节点的 outputs。追加内容会改变它的指纹并使其 stale；要恢复就需要重新验收 M0 与 M1，超出 M2 授权。
- `docs/10-plans/dsh-go-replication/stages/m02-composition.md` 列在 M0 契约的 `documents` 中。改动它会改变 M0 的 revision，使 M0 **全部**节点变为 `plan changed; revise required`。
- M1 审计"仍然开放"表里"在 M2 展开时决定归属"的承诺由本页兑现。审计页描述的是 M1 结束时的状态，保持原样并不失真——它不是一份需要追随 M2 进展的活动文档。

若后续需要把回链固化进审计页，正确做法是先对 M0／M1 执行 `revise` 并按协议重新验收，再改写；不得静默覆盖它们已通过时的产物。这条界限记在此处，避免以后有人以"文档过时"为由绕过协议。

本节点的 `gap-adjudication` 检查断言六项缺口确实出现在本页，防止裁决只写在任务记录里而没有落盘。

## G2 收口的范围决定

G2 复核了受门禁包清单、跑通完整 `make check` 并导出验证证据。两处范围决定需要说明：

1. **格式门禁从固定目录清单改为遍历仓库 Go 源码。** 原 `gofmt -l cordis examples scripts` 只覆盖三个目录，`loader/`、`app/`、`cmd/`、`internal/` 都不在其中——实测把一个未格式化文件放进 `loader/` 后 `make check` 仍然通过。现在由 [scripts/doccheck/fmt.go](../../../scripts/doccheck/fmt.go) 的 `checkFormatting` 遍历仓库（跳过 `.git`、`bin`、`validation`），Makefile、CI 与 [测试说明](../../testing.md) 同步改用 `go run ./scripts/doccheck -fmt`。有效与无效样例在 [fmt_test.go](../../../scripts/doccheck/fmt_test.go)。
2. **不改 `stages/index.md`。** 它列在 M0 契约的 `documents` 中，改动会使 M0 全部节点变为 `plan changed; revise required`。M2 的状态因此记录在本页与[自动生成的 status.md](status.md)，上游文档保持 M0 时点内容。这条界限与 [M0.3 的处置](#裁决落点的明确处置)一致。

## 阶段结论与未验项

六个节点全部通过各自契约的检查与语义评审（评审独立性如实记录为 `independent: false`：作者完成评审）。以下边界必须随阶段一起交付，不得读成已完成：

- **M12.2 归属**：YAML include、`!!js` 惰性表达式、配置热重载、原 DSH profile 兼容、npm 动态安装与模块 HMR。M2 只交付 JSON 输入形式与显式编译期注册。
- **未做互操作验证**：没有证明 Go 实现能读取原 DSH 的 profile 文件或加载 npm 插件——本阶段明确不声称。
- **上游 receipt stale**：m21 的 `loader/entry.go` 与 `loader/README.md`、m22 的 `loader/group.go`/`loader/tree.go`/`loader/tree_test.go`、m02-contract 的 `plan.json` 均被下游节点改写。按协议这些 receipt 已 stale，须由评审/控制器在当前产物上重新验收；下游节点不得自证前驱，也不得回退真实缺陷修复。
- **G2 的覆盖率语义**：`app` 与 `loader` 都受 ≥90% 门禁，实测分别为 90.7% 与 92.5%。这不是 DSH 的每文件 100% 规则。
