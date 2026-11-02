# M3 执行契约：对话词汇、Agent Scope 与内存 Session

## 现状

M0、M1 与 M2 的执行契约都已通过，M3 的待实施范围由[阶段索引](../dsh-go-replication/stages/index.md)和 [M3 细化方案](../dsh-go-replication/stages/m03-session.md)定义：五个切片 M3.1–M3.5、退出门禁 G3。本页是 M3 的入口，事实归属仍在细化方案和共同验收页，本页不复制它们的规则。

M3 之前，仓库没有任何对话词汇、事件日志或投影：`cordis` 是通用运行时，`loader`／`app` 是装配层，两者都不产生模型可见数据。M3 首次引入**权威事实**与**从事实派生的视图**这对关系，后续 M4–M6 的流、工具、轮次和持久化都建立在这份数据契约上。

## 目标

- **复刻**：[[goal:replication]] 从一份权威事件日志重建出同一份模型历史，并让消息词汇、Agent 可见层与事件 envelope 与固定 DSH 参考行为对应。
- **学习**：[[goal:learning]] 让 Go 读者从结构体与切片出发，解释“为什么 Session 不直接保存一份 messages 数组”，区分声明数据与派生视图、审计事件与模型可见事件。
- **证据**：[[goal:evidence]] 每条行为有固定 TS 来源、Go 入口、行为测试与显式差异；规划、缺环境与未验项不进入通过分子。

## 执行契约

[plan.json](plan.json) 是本阶段的执行契约，六个节点按下面的依赖推进：

[plan.json](plan.json) 是本阶段的执行契约，六个节点串行：

[[node:m31-llm]] → [[node:m32-scope]] → [[node:m33-session]] → [[node:m34-projection]] → [[node:m35-example]] → [[node:g3-acceptance]]

M3 细化方案把 M3.2 与 M3.3 写成同一前驱下的两条分支。本契约改为**串行**：两个切片都要把新包登记进共享的覆盖率门禁（`scripts/doccheck/main.go`、`docs/testing.md`），而登记必须发生在包已存在之后——否则门禁会因为“没有覆盖数据”而拒绝一个尚未创建的包。串行是这条登记规则的结果，不是排版偏好。

每个切片同时交付实现与学习产物。每节点需要语义评审，独立性如实记录。

### 为什么 M3 采用“两层”而不是“一层”模型历史

M3 细化方案原文有一处必须明确的歧义，它决定整个阶段的结构，因此在本页裁决：

**`session-projection` 注册表与 session 的 message projection 是两套机制，只有后者产生模型历史。** 固定参考里 `packages/session/session-projection` 的 `ProjectionDefinition.apply` 提供的是**宿主与客户端读模型**（它的 README 明确写着不产生模型可见内容），而 `packages/core/session/src/surface.ts` 的 `foldSurface` + `SessionMessageProjection` 才是 `deriveMessages()` 的唯一来源。

M3 细化方案的 M3.4 写“实现消息投影和注册的纯 projection；区分模型 surface 与审计事件”，其验收（重复 fold 一致、缺失必要 projection 显式失败、审计事件不凭空进入模型）三项**都**指向 surface 层。因此本阶段实现的是 surface 语义；`session-projection` 客户端读模型属 M11 的客户端口投影，不在 M3 范围。这条裁决写在本页，避免读者把两个同名机制混为一谈。

### 固定参考的差分实测

M3 的核心语义（surface fold）可以在不启动 Cordis 的情况下直接调用参考实现，因此本阶段可以做**真实的双实现差分**，而不是只靠 Go 单边断言。已实测：

```sh
node -e "import('<DSH>/packages/core/session/lib/index.js').then(m=>console.log(m.SESSION_FORMAT_VERSION))"
```

参考提交的 `lib/` 构建产物存在，`foldSurface`、`deriveEventMessage`、`isSurfaceEligibleType` 均可直接调用。差分场景由 `testdata/parity/session/` 承载（该路径由 [[node:m35-example]] 创建）：同一份 JSON 分别喂给 Go 实现与本仓库的 Node runner，两侧产出同一种逐行 trace 后比较。参考 `lib/` 是 `.gitignore` 的构建产物，检查在缺失时**跳过**并单独记为未验证，不记为通过。

## 产物所有权与不变量

契约遵守 M2 已确立的三条不变量：

1. **inputs 不得被本节点或链上更晚的节点写入。**
2. **不使用目录级 writes**：每个 `writes` 逐文件列出。
3. **检查只断言本节点有权写入、或由已通过前驱产出的文件。**

共享门禁文件 `scripts/doccheck/main.go`、`scripts/doccheck/main_test.go`、`docs/testing.md` 被 m31、m32、m33 三个切片先后修改（每个新运行时包在同一片内登记进 `coverageRequirement`）。它们因此不出现在任何节点的 `inputs`，也不作为非最终节点的 `outputs`，最终状态由 [[node:g3-acceptance]] 冻结。

### 对上游产物的已知影响

| 被改文件 | 上游归属 | 后果 |
| --- | --- | --- |
| `scripts/doccheck/main.go`、`main_test.go` | M2 `g2-acceptance` outputs | M2 收口节点在协议中转为 stale，需按其计划重新验收或接受其历史结论 |
| `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md` | M0 `cordis-audit`、M2 `m25-cli` outputs | 同上 |
| `docs/testing.md` | M2 `g2-acceptance` outputs | 同上 |
| `docs/10-plans/dsh-go-replication/stages/index.md` | M0 `documents` + M2 `g2-acceptance` outputs | M0 全部节点变为 `plan changed; revise required`（M2 已使 M0 处于该状态）；该文件由 [[node:g3-acceptance]] 更新当前起点 |

`stages/m03-session.md` 与 `stages/m02-composition.md` 列在 M0 契约的 `documents` 中，**不改**：它们是 M0 时点的方案文本，改动会使 M0 revision 变化的范围进一步扩大。M3 的实际交付范围与实测差异记录在**本页**。`docs/10-plans/dsh-go-replication/cordis-audit.md` 是 M0／M1 已通过节点的 outputs，同样不改。

这是 M3 真实修改门禁与新增模块时不可避免的代价，已如实记录，不用“局部改动”淡化。

### M3 细化方案中未在本阶段实现的部分

M3 细化方案的“修改范围”列出 `docs/llm/`、`docs/session/`。本阶段按新模块登记规则增加了 `docs/scope/`：AGENTS.md 要求每个新模块进入[文档索引](../../README.md)与[路线图](../../roadmap.md)，`scope/` 是三个新包之一，只给它包 README 会让概念说明没有归属页。这是范围扩张，不是遗漏。

M3 细化方案的“计划中的检查”留了三个命令。`go test ./llm/... ./scope/... ./session/...` 与 `go run ./examples/session` 进入本契约；`make check` 由 [[node:g3-acceptance]] 执行。

## 阶段内要冻结的选择

| 选择 | 决定 | 理由 |
| --- | --- | --- |
| 内容块表达 | 判别字段结构体 + 原始 JSON 兜底（`ContentBlock` 为接口，`UnknownBlock` 保留原文） | 参考允许未知块在持久层存活、由 Provider 拒绝；Go 的接口值不直接序列化，必须用明确 codec |
| 未知内容块政策 | 分层：组装报错、存储保留、Provider 拒绝 | 参考的三层行为不同，全局单一政策在每个方向都是错的 |
| 消息身份 | `type MessageID string`，构造函数不校验格式 | 参考的 brand 是编译期身份转换，不做格式校验；Go 加校验会比参考更严并破坏旧 ID 回放 |
| 序列号 | `type Seq uint64`，`seq == len(log)` | 参考里 `seq` 不是独立分配的计数器，而是追加时的日志长度；间隙与重复一律拒绝，绝不重编号 |
| envelope 形状 | 恰好 7 个字段的带标签结构体，未知字段拒绝 | 参考的 `assertSessionEventEnvelope` 拒绝任何额外自有键 |
| 未知事件政策 | 未知且无 `ignorable:true` ⇒ 整份日志拒绝重建；未知且有 ⇒ 保留原文且永不影响模型 surface | `ignorable` 是持久层兼容机制，不是注册表 |
| 两层注册表 | `ProjectionRegistry`（按字符串键，读模型）与 `MessageProjectionRegistry`（按事件类型，内容改写）分开 | 两者签名、失败模式、缓存都不同，见上文裁决 |

Go 差异记录在 `llm/README.md`、`scope/README.md`、`session/README.md` 的限制章节；差分场景中的显式差异记录在 `testdata/parity/session/DIVERGENCES.md`。两个路径都由本阶段的节点创建（见[执行契约](#执行契约)），所以此处按仓库的文档规范写成代码文字而不是链接。

## 学习交付

下表是[[node:m35-example]]与[[node:g3-acceptance]]要交付的主题。路径在创建前保持代码文字，创建后由 [[node:g3-acceptance]] 在[学习地图](../../learning/index.md)中改为实际链接并纳入“当前可学”表。

| 主题 | 笔记（由 m31–m35 创建） | 前置 |
| --- | --- | --- |
| C06 对话与流 | `docs/llm/go-primer.md`、`docs/llm/content-blocks.md` | Go 结构体、接口、JSON |
| C07 事件溯源 | `docs/session/go-primer.md`、`docs/session/event-log.md`、`docs/session/projection.md` | C06、C04 的事件概念 |
| Agent 可见层 | `docs/scope/go-primer.md` | C02 的服务可见性 |

三个主题都按[学习笔记模板](../../learning/note-template.md)组织。M3 通过后 C06、C07 与 Agent 可见层从[学习地图](../../learning/index.md)的规划行转为可学行，其“后续学习地图”表同步更新。

## 阶段结论与未验项

本阶段的边界必须在交付时一起写明，不得读成已完成：

- **没有调用任何真实模型**。`llm/` 只有词汇与 codec；流、assembler、Provider 与 tRPC 适配器属 M4。
- **没有持久化**。`session/` 是内存日志；JSONL、句柄、单写者、恢复与格式互读属 M6。本阶段不冒用 DSH 的 `session.v4.jsonl` 格式名，也不写入用户原有 DSH 数据。
- **没有读取原 DSH Session 文件的能力**。差分测试对照的是 surface 算法的行为，不是文件格式互操作；后者属 M6／M12。
- **`session-projection` 客户端读模型未实现**，属 M11。
- **M3 的差分检查依赖参考构建产物**。`lib/` 缺失时该检查跳过，报告记为未验证而不是通过。

## 阶段结论与未验项

六个节点全部通过各自契约的检查与语义评审（评审独立性如实记录为 `independent: false`：作者完成评审）。状态由 CLI 生成，见 [status.md](status.md) 与 [dag.mmd](dag.mmd)。契约在执行中修正过几次，每次修正后的通过状态各有一份 `validation/<run-id>/` 导出，互不覆盖；本页不链接某一个 run，因为 run 目录由 export 生成，而本页是其 inputs 的一部分——引用它会让本页的每一次改写都使刚导出的证据失效。查验证据时按 run-id 查看 `validation/` 下的目录。

复刻与学习两项都留有证据：

- **复刻**：`llm`、`scope`、`session` 三个包交付各自的行为与失败用例；`session` 的 surface 语义另有**双实现差分**——六个共享场景由 Go 与固定参考分别执行，trace 逐行比较，当前**没有任何已声明差异**（[场景说明](../../../testdata/parity/session/README.md)、[差异清单](../../../testdata/parity/session/DIVERGENCES.md)、[录制来源](../../../testdata/parity/session/provenance.json)）。
- **学习**：五个学习文档按模板组织并进入[文档索引](../../README.md)、[路线图](../../roadmap.md)与[学习地图](../../learning/index.md)；`examples/session` 是可运行入口，其输出由文档程序门禁逐字比对。

以下边界必须随阶段一起交付，不得读成已完成：

- **没有调用任何真实模型**。`llm` 只有词汇与 codec；流、assembler、Provider 与 tRPC 适配器属 M4。
- **没有持久化**。`session` 是内存日志；JSONL、句柄、单写者、恢复与格式互读属 M6。本阶段不冒用 DSH 的 `session.v4.jsonl` 格式名，也不写入用户原有 DSH 数据。
- **没有读取原 DSH Session 文件的能力**。差分对照的是 surface 算法的行为，不是文件格式互操作。
- **payload 级校验未实现**。参考在入日志边界还检查 `request/header` 的空字段、`developer/message` 的 `headerSeq` 与 `tool/result` 的 `isError` 一致性；本阶段只实现 envelope 级规则。差异逐条记在 [envelope 参考](../../session/event-log.md#本切片未实现的部分)。
- **`session-projection` 客户端读模型未实现**，属 M11。
- **压缩、图片附件与真实 offload 投影未实现**。`replace` 的语义与全部约束已实现并有测试，但产生压缩区间的组件属 M8；测试里的 offload 解释器是等价替身。
- **实时差分需要参考构建产物**。`DSH_PACKAGES` 指向的 `packages/core/session/lib` 缺失时，`TestLiveReferenceSessionSurfaceMatchesRecorded` 跳过并记为未验证；已记录的参考 trace 仍参与比较。

### 契约在执行中修正的三处

如实记录，因为“通过”的前提是契约本身可信：

1. **m31 的产物清单漏登 `llm/coverage_test.go`**，首次 verify 报“changes outside declared writes”。该文件是真实交付物，已补入 outputs 与 writes。
2. **m32 的产物清单漏登 `docs/scope/go-primer.md`**，同类遗漏，已补齐。
3. **m34 必须扩展 m33 的产物**：实时 surface 要与事件写入在同一个临界区里更新，而 Go 无法从另一个文件给结构体加字段，因此 `session/log.go`、`types.go`、`codec.go`、`registry.go` 列入 m34 的 writes。这使 m33 的 receipt 转为 stale，按协议在集成产物上重新验收，而不是让下游自证前驱。

此外 g3 的 `learning-index` 检查初稿钉住了一个固定标题计数，在加入 scope 主题后失败；检查已改为断言**内容**而不是计数。
