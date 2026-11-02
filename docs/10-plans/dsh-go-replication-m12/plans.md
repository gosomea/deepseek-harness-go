# M12 全域覆盖与发行收口

本页是 `plan.json` 的主题页，与它共同决定 revision。

## 现状

M0–M11 已通过各自执行契约。M12 对固定基线的每个包组给出可追溯结论，并把本环境无法完成的发行/签名/真实外部能力如实记为缺口。

## 目标与诚实边界

对固定基线的每个包组给出可追溯结论；把可交付的兼容与 Provider 补齐；发布**范围准确、可复现**的版本。本环境（单机 macOS、无签名权限、无真实外部服务、无完整前端/native 链）无法完成的项，作为**明确缺口**记录，不伪造成通过。

三个必须成立的性质：

1. **账本机器可查**：54 包组 + 9 vendor 每组必须有处置，未归属由 `cmd/conformance` 拒绝。
2. **缺口如实记录**：兼容/配置/格式/PTC 与发行/平台/性能缺口逐项书面声明。
3. **不声称完整复刻**：仍有缺口时只发布范围明确的阶段版本。

## 进入条件

必需前驱：[M9 的 G9](../dsh-go-replication-m09/plans.md) 与 [M11 的 G11](../dsh-go-replication-m11/plans.md)，均已通过。

## 执行契约

见 [plan.json](plan.json)。**九个切片逐一成节点**（M12.1–M12.9），可交付者真做，缺口者逐切片记录理由：

| 节点 | 切片 | 交付 | 处置 |
| --- | --- | --- | --- |
| `m121-ledger` | M12.1 | `internal/conformance` + `cmd/conformance` + `dispositions.json`（63 项）+ `ledger.md` | 已实现（机器校验） |
| `m122-config-compat` | M12.2 | `docs/configuration/compat.md` | 缺口记录（YAML/include/惰性/volatile/HMR/profile） |
| `m123-session-format` | M12.3 | `docs/session-format/compat.md` | 缺口记录（原 DSH 格式/迁移/query） |
| `m124-ptc-workflow` | M12.4 | `docs/ptc/gaps.md` | 缺口记录（PTC 生产 Provider） |
| `m125-experimental` | M12.5 | `docs/experimental/gaps.md` | 缺口记录（浏览器/计算机/Office/语音等） |
| `m126-credentials-telemetry` | M12.6 | `plugins/telemetry`（隐私白名单，94.7%） | 已实现（keyless seam） |
| `m127-native-bench` | M12.7 | `benchmarks/`（可跑基准）+ `docs/release/platform.md` | 基准已实现；跨平台未验证 |
| `m128-release` | M12.8 | `docs/release/distribution.md` + `gaps.md` + `version.md` | 缺口记录（签名/三平台/Python SDK） |
| `m129-docs-website` | M12.9 | `docs/website/index.md` + `docs/conformance/scope.md` | 骨架已实现；静态发布缺口 |
| `g12-acceptance` | G12 | 账本校验、基准、make check、文档门禁 | 验收 |

> 说明：每切片状态在上表逐项列出。可交付的（行为账本、遥测 seam、基准脚本、文档骨架）真做并测试；确需外部环境/签名/多平台/Python 链的（配置 HMR、原 Session 迁移、PTC 生产引擎、实验能力、发行签名与 Python SDK、跨平台）逐切片记录缺口理由，不伪装通过。这与 c-m12「无法完成的发行门禁如实记为缺口」一致，且不再折叠为聚合文档。

## 产物所有权与不变量

- 共享门禁文件 `scripts/doccheck/*` 由 `m121` 登记（新运行时包 `internal/conformance`），`g12` 冻结。
- `docs/README.md`、`docs/roadmap.md`、`docs/learning/index.md`、`stages/index.md` 只在 `g12` 写一次。

## 学习交付

| 笔记 | 主题 |
| --- | --- |
| [conformance/ledger.md](../../conformance/ledger.md) | 能力账本、处置语义与机器校验 |
| [conformance/scope.md](../../conformance/scope.md) | 范围说明与发布纪律 |

## 账本统计（当前）

`verified=30 adapted=25 gap=8`（63 项）。8 个 gap 组：`browser-use`、`computer-use`、`document`、`experimental`、`ptc-runtime`、`session-query`、`typert`、`webhook`。

## 本阶段实际运行的命令与结果

| 阶段 | 命令 | 退出码 | 结果 |
| --- | --- | --- | --- |
| 契约 | `plan.py validate docs/10-plans/dsh-go-replication-m12/plan.json` | 0 | 5 节点，结构合法 |
| M12.1 | `go test -race -count=1 ./internal/conformance/...` | 0 | 覆盖率 93.2% |
| M12.1 | `go run ./cmd/conformance -inventory ... -dispositions ...` | 0 | `conformance ledger: OK (63 groups, verified=30 adapted=25 gap=8)` |
| M12.6 | `go test -race -count=1 ./plugins/telemetry/...` | 0 | 覆盖率 94.7% |
| M12.7 | `go test -run '^$' -bench . -benchmem ./benchmarks/...` | 0 | 基准可跑（TaskstateRebuild/ClientDelta） |
| g12 | `make check`（从仓库根） | 0 | fmt/vet/test/coverage/docs/doc-examples/build 全部通过 |

**这些都是本机结果。** 本地通过不代表 CI 已通过。

### 尚未验证（明确缺口）

- 三平台打包/安装/签名、Python SDK 分发、native 平台矩阵与真实负载基准、真实 Web UI/浏览器 E2E/Desktop 打包：未执行/未实现（详见 [release/gaps.md](../../release/gaps.md)）。
- 原 DSH 配置（YAML/include/惰性）、原 Session 格式迁移、PTC 生产 Provider、typert、webhook 等兼容能力：未实现（详见 [compatibility/gaps.md](../../compatibility/gaps.md)）。
- 本次为发布前快照：只发布范围明确的阶段版本，**不声称完整复刻**。

## 阶段内的真实失败与处置

1. `cmd/conformance` 首版无法解析 inventory：`vendor_packages` 是字符串数组而非映射 → 调整 `Inventory` 结构并归一 vendor 名（`vendor/x` → `x`）。
2. 账本初版把已实现的 `preset`/`skill`/`host`/`acp`/`guard` 误标 gap → 按真实实现纠正为 verified/adapted，避免虚报缺口。
3. `internal/conformance`、`cmd/conformance` 缺 README → doccheck 拒绝后补写；`cmd` 的 `kind` 需为 `command-tool` 后通过。
