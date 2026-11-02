# 范围说明与对照审阅

本文件给出全域对照审阅的结论，并以明确措辞限定发布范围。

## 结论

- 固定基线的 54 个包组 + 9 个 vendor 包，账本（`dispositions.json`）**全部有处置**，由 `cmd/conformance` 机器校验：`verified=30 adapted=25 gap=8`。
- **不声称完整复刻**：8 个 gap 组与跨平台发行门禁未完成；本版本只发布范围明确的阶段版本。
- 账本以包组为分母，组内逐包行为等价仍待 M12 之后的持续审计；数字不代表每个包都逐项对照过。

## 可导航入口

- 全域账本：[ledger.md](ledger.md)
- 兼容缺口：[../compatibility/gaps.md](../compatibility/gaps.md)
- 发行缺口：[../release/gaps.md](../release/gaps.md)
- 学习地图：[../learning/index.md](../learning/index.md)

## 发布纪律

任一 gap 未关闭、任一发行门禁未执行时，发布说明必须写明「范围明确的阶段版本」，不得改为「完整复刻」。
